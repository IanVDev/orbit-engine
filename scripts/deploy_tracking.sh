#!/usr/bin/env bash
# deploy_tracking.sh — Deploy soberano do tracking-server (padrão AURYA).
#
# Princípios AURYA:
#   Atomic   — binário substituído atomicamente (temp → rename), sem janela parcial
#   Unambiguous — commit SHA obrigatório; deploy sem identidade conhecida é rejeitado
#   Repeatable  — mesmo artefato + mesmo commit = mesmo resultado em qualquer ambiente
#   Yield      — observability obrigatória pós-deploy; sem dados, sem sucesso
#   Auditable  — cada deploy registra hash, commit e timestamp rastreáveis
#
# Fluxo:
#   1. Pré-voo (dependências + variáveis obrigatórias)
#   2. Download do artefato versionado (ORBIT_ARTIFACT_URL ou local em dev)
#   3. Validação de checksum SHA-256
#   4. Substituição atômica do binário
#   5. Restart do serviço (systemd → launchd → processo direto)
#   6. Validação /health
#   7. Validação /v1/runtime (commit esperado)
#   8. Observability integrity check (obrigatório)
#
# Variáveis de ambiente:
#   ORBIT_ARTIFACT_URL     URL do artefato (s3:// ou https://) — obrigatório em produção
#   ORBIT_ARTIFACT_SHA256  SHA-256 esperado do artefato — obrigatório em produção
#   ORBIT_EXPECTED_COMMIT  Commit SHA esperado no binário — obrigatório em produção
#   ORBIT_BINARY_PATH      Destino do binário (padrão: /usr/local/bin/tracking-server)
#   ORBIT_ENV              Ambiente: production | development (padrão: development)
#   TRACKING_HOST          host:porta para validação (padrão: 127.0.0.1:9100)
#   STARTUP_TIMEOUT        Segundos aguardando /health (padrão: 15)
#   LAUNCHD_LABEL          Label launchd macOS (padrão: com.orbit-engine.tracking-server)
#   SYSTEMD_UNIT           Nome da unit systemd Linux (padrão: tracking-server)
#
# Modo desenvolvimento (sem S3):
#   Se ORBIT_ARTIFACT_URL não estiver definido, usa ORBIT_LOCAL_BINARY (caminho local).
#   Exige ORBIT_ENV != production. Checksum e commit ainda são validados.
#
# Uso:
#   # Produção (requer bucket configurado):
#   ORBIT_ENV=production \
#   ORBIT_ARTIFACT_URL=s3://meu-bucket/tracking-server-abc1234-linux-amd64 \
#   ORBIT_ARTIFACT_SHA256=<sha256> \
#   ORBIT_EXPECTED_COMMIT=abc1234 \
#   ./scripts/deploy_tracking.sh
#
#   # Desenvolvimento (binário local, sem S3):
#   ORBIT_LOCAL_BINARY=./tracking/tracking-server-abc1234-darwin-arm64 \
#   ORBIT_EXPECTED_COMMIT=abc1234 \
#   ./scripts/deploy_tracking.sh

set -uo pipefail

# ── Config ────────────────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

ORBIT_ENV="${ORBIT_ENV:-development}"
TRACKING_HOST="${TRACKING_HOST:-127.0.0.1:9100}"
TRACKING_URL="http://${TRACKING_HOST}"
ORBIT_BINARY_PATH="${ORBIT_BINARY_PATH:-/usr/local/bin/tracking-server}"
STARTUP_TIMEOUT="${STARTUP_TIMEOUT:-15}"
LAUNCHD_LABEL="${LAUNCHD_LABEL:-com.orbit-engine.tracking-server}"
SYSTEMD_UNIT="${SYSTEMD_UNIT:-tracking-server}"
ORBIT_ARTIFACT_URL="${ORBIT_ARTIFACT_URL:-}"
ORBIT_ARTIFACT_SHA256="${ORBIT_ARTIFACT_SHA256:-}"
ORBIT_EXPECTED_COMMIT="${ORBIT_EXPECTED_COMMIT:-}"
ORBIT_LOCAL_BINARY="${ORBIT_LOCAL_BINARY:-}"

IS_PRODUCTION=0
[[ "${ORBIT_ENV}" == "production" ]] && IS_PRODUCTION=1

# ── Cores ─────────────────────────────────────────────────────────────────────

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

pass()   { echo -e "  ${GREEN}[✓]${NC} $*"; }
fail()   { echo -e "  ${RED}[✗]${NC} $*"; exit 1; }
info()   { echo -e "  ${CYAN}[→]${NC} $*"; }
warn()   { echo -e "  ${YELLOW}[~]${NC} $*"; }
header() { echo -e "\n${BOLD}$*${NC}"; }

# ── Utilitários ───────────────────────────────────────────────────────────────

sha256_of() {
    local file="$1"
    if command -v sha256sum &>/dev/null; then
        sha256sum "$file" | awk '{print $1}'
    elif command -v shasum &>/dev/null; then
        shasum -a 256 "$file" | awk '{print $1}'
    else
        fail "sha256sum e shasum ausentes — checksum impossível"
    fi
}

# Detecta e abstrai o sistema de init disponível.
# Retorna: "systemd" | "launchd" | "process"
detect_init() {
    if command -v systemctl &>/dev/null && systemctl --version &>/dev/null 2>&1; then
        echo "systemd"
    elif command -v launchctl &>/dev/null; then
        echo "launchd"
    else
        echo "process"
    fi
}

restart_service() {
    local init_system="$1"
    case "${init_system}" in
        systemd)
            info "systemctl restart ${SYSTEMD_UNIT}..."
            systemctl restart "${SYSTEMD_UNIT}" || fail "systemctl restart falhou"
            pass "serviço reiniciado via systemd"
            ;;
        launchd)
            info "launchctl kickstart -k ${LAUNCHD_LABEL}..."
            launchctl kickstart -k "gui/$(id -u)/${LAUNCHD_LABEL}" 2>/dev/null || \
            launchctl kickstart -k "system/${LAUNCHD_LABEL}" 2>/dev/null || {
                # Fallback: stop + start
                launchctl stop "${LAUNCHD_LABEL}" 2>/dev/null || true
                sleep 1
                launchctl start "${LAUNCHD_LABEL}" 2>/dev/null || true
                warn "launchctl restart via stop+start (kickstart nao disponível)"
            }
            pass "serviço reiniciado via launchd"
            ;;
        process)
            info "gerenciamento direto de processo (sem init system)..."
            OLD_PID=$(pgrep -f "tracking-server" 2>/dev/null || true)
            if [[ -n "${OLD_PID}" ]]; then
                kill "${OLD_PID}" 2>/dev/null || true
                local waited=0
                while kill -0 "${OLD_PID}" 2>/dev/null; do
                    [[ ${waited} -ge 5 ]] && { kill -9 "${OLD_PID}" 2>/dev/null || true; break; }
                    sleep 1; (( waited++ ))
                done
                pass "processo antigo encerrado (PID ${OLD_PID})"
            else
                info "nenhum processo anterior encontrado"
            fi
            LOG_FILE="/tmp/tracking-server.log"
            local binary_dir
            binary_dir="$(dirname "${ORBIT_BINARY_PATH}")"
            (cd "${binary_dir}" && "${ORBIT_BINARY_PATH}" >> "${LOG_FILE}" 2>&1) &
            disown $! 2>/dev/null || true
            pass "processo iniciado — log: ${LOG_FILE}"
            ;;
    esac
}

# ── Banner ────────────────────────────────────────────────────────────────────

header "deploy_tracking.sh — padrão AURYA"
echo "  ambiente:  ${ORBIT_ENV}"
echo "  destino:   ${ORBIT_BINARY_PATH}"
echo "  backend:   ${TRACKING_URL}"
echo "  iniciado:  $(date -u +"%Y-%m-%dT%H:%M:%SZ")"

# ── Etapa 1: Pré-voo ──────────────────────────────────────────────────────────

header "Etapa 1/7 — Pré-voo"

for cmd in curl; do
    command -v "$cmd" &>/dev/null || fail "dependência ausente: $cmd"
done

# Em produção, variáveis de identidade são obrigatórias (fail-closed).
if [[ ${IS_PRODUCTION} -eq 1 ]]; then
    [[ -z "${ORBIT_ARTIFACT_URL}" ]]    && fail "ORBIT_ARTIFACT_URL é obrigatório em production"
    [[ -z "${ORBIT_ARTIFACT_SHA256}" ]] && fail "ORBIT_ARTIFACT_SHA256 é obrigatório em production"
    [[ -z "${ORBIT_EXPECTED_COMMIT}" ]] && fail "ORBIT_EXPECTED_COMMIT é obrigatório em production"
    pass "variáveis de produção presentes"
else
    warn "modo development — S3 pode ser substituído por ORBIT_LOCAL_BINARY"
    [[ -z "${ORBIT_EXPECTED_COMMIT}" ]] && warn "ORBIT_EXPECTED_COMMIT não definido — validação de commit será pulada"
fi

INIT_SYSTEM=$(detect_init)
info "init system detectado: ${INIT_SYSTEM}"
pass "pré-voo concluído"

# ── Etapa 2: Aquisição do artefato ────────────────────────────────────────────

header "Etapa 2/7 — Aquisição do artefato"

TEMP_BINARY="$(mktemp /tmp/tracking-server-deploy-XXXXXX)"
trap 'rm -f "${TEMP_BINARY}" "${TEMP_BINARY}.sha256"' EXIT

if [[ -n "${ORBIT_ARTIFACT_URL}" ]]; then
    info "baixando de: ${ORBIT_ARTIFACT_URL}"

    if [[ "${ORBIT_ARTIFACT_URL}" == s3://* ]]; then
        command -v aws &>/dev/null || fail "aws CLI ausente — necessário para s3:// URLs"
        aws s3 cp "${ORBIT_ARTIFACT_URL}" "${TEMP_BINARY}" || fail "download S3 falhou"
        # Baixar checksum do mesmo bucket (convenção: <artifact>.sha256)
        if [[ -z "${ORBIT_ARTIFACT_SHA256}" ]]; then
            aws s3 cp "${ORBIT_ARTIFACT_URL}.sha256" "${TEMP_BINARY}.sha256" 2>/dev/null && \
                ORBIT_ARTIFACT_SHA256=$(awk '{print $1}' "${TEMP_BINARY}.sha256") || \
                fail "ORBIT_ARTIFACT_SHA256 não definido e .sha256 não encontrado no bucket"
        fi
    else
        # https:// ou http://
        curl -fsSL --max-time 60 --output "${TEMP_BINARY}" "${ORBIT_ARTIFACT_URL}" \
            || fail "download HTTP falhou: ${ORBIT_ARTIFACT_URL}"
        if [[ -z "${ORBIT_ARTIFACT_SHA256}" ]]; then
            curl -fsSL --max-time 10 --output "${TEMP_BINARY}.sha256" "${ORBIT_ARTIFACT_URL}.sha256" 2>/dev/null && \
                ORBIT_ARTIFACT_SHA256=$(awk '{print $1}' "${TEMP_BINARY}.sha256") || \
                fail "ORBIT_ARTIFACT_SHA256 não definido e .sha256 não encontrado em ${ORBIT_ARTIFACT_URL}.sha256"
        fi
    fi
    pass "artefato baixado"

elif [[ -n "${ORBIT_LOCAL_BINARY}" ]]; then
    [[ ${IS_PRODUCTION} -eq 1 ]] && fail "ORBIT_LOCAL_BINARY não permitido em production"
    [[ -f "${ORBIT_LOCAL_BINARY}" ]] || fail "ORBIT_LOCAL_BINARY não encontrado: ${ORBIT_LOCAL_BINARY}"
    cp "${ORBIT_LOCAL_BINARY}" "${TEMP_BINARY}"
    pass "artefato copiado de ${ORBIT_LOCAL_BINARY} (modo dev)"

else
    fail "nenhuma fonte de artefato definida. Defina ORBIT_ARTIFACT_URL (produção) ou ORBIT_LOCAL_BINARY (dev)"
fi

chmod +x "${TEMP_BINARY}"

# ── Etapa 3: Validação de checksum ────────────────────────────────────────────

header "Etapa 3/7 — Validação de checksum"

ACTUAL_SHA=$(sha256_of "${TEMP_BINARY}")
info "sha256 calculado: ${ACTUAL_SHA}"

if [[ -n "${ORBIT_ARTIFACT_SHA256}" ]]; then
    # Normaliza: remove possível nome de arquivo após o hash (formato sha256sum)
    EXPECTED_SHA=$(echo "${ORBIT_ARTIFACT_SHA256}" | awk '{print $1}')
    if [[ "${ACTUAL_SHA}" == "${EXPECTED_SHA}" ]]; then
        pass "checksum válido"
    else
        fail "checksum inválido\n  esperado: ${EXPECTED_SHA}\n  calculado: ${ACTUAL_SHA}"
    fi
else
    warn "ORBIT_ARTIFACT_SHA256 não definido — checksum não validado (apenas em dev)"
fi

# ── Etapa 4: Substituição atômica ─────────────────────────────────────────────

header "Etapa 4/7 — Substituição atômica do binário"

BINARY_DIR="$(dirname "${ORBIT_BINARY_PATH}")"
if [[ ! -d "${BINARY_DIR}" ]]; then
    mkdir -p "${BINARY_DIR}" || fail "não foi possível criar ${BINARY_DIR}"
fi
if [[ ! -w "${BINARY_DIR}" ]]; then
    fail "sem permissão de escrita em ${BINARY_DIR} — execute com sudo ou ajuste permissões"
fi

# Backup do binário atual (se existir)
if [[ -f "${ORBIT_BINARY_PATH}" ]]; then
    BACKUP="${ORBIT_BINARY_PATH}.bak"
    cp "${ORBIT_BINARY_PATH}" "${BACKUP}"
    info "backup: ${BACKUP}"
fi

# rename(2) é atômico no mesmo filesystem — sem janela parcial
mv "${TEMP_BINARY}" "${ORBIT_BINARY_PATH}"
trap - EXIT  # arquivo movido, cancelar remoção do temp
chmod +x "${ORBIT_BINARY_PATH}"

INSTALLED_SHA=$(sha256_of "${ORBIT_BINARY_PATH}")
pass "binário instalado atomicamente"
info "sha256 instalado: ${INSTALLED_SHA}"

# ── Etapa 5: Restart do serviço ───────────────────────────────────────────────

header "Etapa 5/7 — Restart do serviço (${INIT_SYSTEM})"
restart_service "${INIT_SYSTEM}"

# ── Etapa 6: Validação /health ────────────────────────────────────────────────

header "Etapa 6/7 — Validação /health"
info "aguardando backend responder (timeout: ${STARTUP_TIMEOUT}s)..."

elapsed=0
while true; do
    STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "${TRACKING_URL}/health" 2>/dev/null || echo "000")

    if [[ "${STATUS}" == "200" ]]; then
        BODY=$(curl -s --max-time 2 "${TRACKING_URL}/health")
        pass "/health OK: ${BODY}"
        break
    fi

    if [[ ${elapsed} -ge ${STARTUP_TIMEOUT} ]]; then
        echo ""
        fail "/health não respondeu em ${STARTUP_TIMEOUT}s (último status: ${STATUS})"
    fi

    sleep 1; (( elapsed++ )); printf "."
done
echo ""

# ── Etapa 7: Validação /v1/runtime ────────────────────────────────────────────

header "Etapa 7/7 — Validação /v1/runtime"

RUNTIME_BODY=$(curl -s --max-time 5 "${TRACKING_URL}/v1/runtime" 2>/dev/null || echo "")
RUNTIME_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "${TRACKING_URL}/v1/runtime" 2>/dev/null || echo "000")

if [[ "${RUNTIME_STATUS}" != "200" ]]; then
    fail "/v1/runtime retornou HTTP ${RUNTIME_STATUS} — binário pode ser versão anterior sem este endpoint"
fi

ACTUAL_COMMIT=$(echo "${RUNTIME_BODY}" | grep -o '"commit":"[^"]*"' | cut -d'"' -f4 || echo "")
ACTUAL_VERSION=$(echo "${RUNTIME_BODY}" | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || echo "")
ACTUAL_MODEL=$(echo "${RUNTIME_BODY}" | grep -o '"model_control":"[^"]*"' | cut -d'"' -f4 || echo "")

info "commit: ${ACTUAL_COMMIT}"
info "version: ${ACTUAL_VERSION}"
info "model_control: ${ACTUAL_MODEL}"

if [[ -n "${ORBIT_EXPECTED_COMMIT}" ]]; then
    if [[ "${ACTUAL_COMMIT}" == "${ORBIT_EXPECTED_COMMIT}" ]]; then
        pass "commit validado: ${ACTUAL_COMMIT} == ${ORBIT_EXPECTED_COMMIT}"
    else
        fail "commit mismatch\n  esperado: ${ORBIT_EXPECTED_COMMIT}\n  binário:  ${ACTUAL_COMMIT}"
    fi
else
    warn "ORBIT_EXPECTED_COMMIT não definido — commit não validado"
fi

# ── Observability (obrigatório) ───────────────────────────────────────────────

header "Observability — obrigatório pós-deploy"

OBS_SCRIPT="${SCRIPT_DIR}/observability_integrity_check.sh"
if [[ ! -x "${OBS_SCRIPT}" ]]; then
    fail "observability_integrity_check.sh não encontrado ou sem permissão execute — ${OBS_SCRIPT}"
fi

info "executando ${OBS_SCRIPT}..."
"${OBS_SCRIPT}" || fail "observability integrity check falhou — deploy rejeitado"
pass "observability OK"

# ── Registro de auditoria ─────────────────────────────────────────────────────

AUDIT_LOG="/tmp/orbit-deploy-audit.log"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
echo "${TIMESTAMP} commit=${ACTUAL_COMMIT} version=${ACTUAL_VERSION} sha256=${INSTALLED_SHA} env=${ORBIT_ENV}" \
    >> "${AUDIT_LOG}"

# ── Sumário ────────────────────────────────────────────────────────────────────

echo ""
echo -e "${BOLD}${GREEN}Deploy AURYA concluído.${NC}"
echo "  Commit:    ${ACTUAL_COMMIT}"
echo "  Version:   ${ACTUAL_VERSION}"
echo "  Binary:    ${ORBIT_BINARY_PATH}"
echo "  SHA-256:   ${INSTALLED_SHA}"
echo "  Backend:   ${TRACKING_URL}"
echo "  Audit log: ${AUDIT_LOG}"
echo "  Timestamp: ${TIMESTAMP}"
