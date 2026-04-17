#!/usr/bin/env bash
# provision_orbit_node.sh — Provisionamento de nó orbit-engine em EC2 Ubuntu 22.04.
#
# Execução:
#   Via SSM Run Command:
#     aws ssm send-command \
#       --document-name "AWS-RunShellScript" \
#       --instance-ids "i-xxxx" \
#       --parameters 'commands=["bash /opt/orbit/provision_orbit_node.sh"]' \
#       --region us-east-1
#
#   Via EC2 user data (base64 encode + --user-data):
#     aws ec2 run-instances --user-data file://scripts/provision_orbit_node.sh ...
#
# Variáveis de ambiente (ou SSM Parameter Store em /orbit/*):
#   ORBIT_ARTIFACT_URL      URL do binário tracking-server (s3:// ou https://) [obrigatório]
#   ORBIT_ARTIFACT_SHA256   SHA-256 esperado do artefato [obrigatório]
#   ORBIT_EXPECTED_COMMIT   Commit SHA esperado no /v1/runtime [obrigatório]
#   ORBIT_RECONCILE_SECRET  Chave HMAC para /reconcile [obrigatório em production]
#   ORBIT_HMAC_SECRET       Chave HMAC para /track [opcional]
#   ORBIT_REGION            Região AWS (auto-detectada via IMDS se ausente)
#   ORBIT_ENV               production | development (default: production)
#   ORBIT_TOKEN_BUDGET_SESSION  (default: 100000)
#   ORBIT_TOKEN_BUDGET_CALL     (default: 10000)
#
# Permissões IAM obrigatórias no instance profile:
#   ec2:TerminateInstances  (escopo: própria instância via condição aws:ResourceAccount)
#   s3:GetObject            (escopo: bucket de artefatos)
#   ssm:GetParameter        (escopo: /orbit/*)
#   ssm:GetParameters       (escopo: /orbit/*)
#
# Fail-closed: qualquer erro → log FATAL + ec2:TerminateInstances + exit 1.
# Idempotente: binário com mesmo SHA-256 não é reinstalado.
# Logs estruturados: JSON em /var/log/orbit-provision.log e stdout (journald em user data).

set -uo pipefail

# ── Constantes ────────────────────────────────────────────────────────────────

readonly PROVISION_LOG="/var/log/orbit-provision.log"
readonly ORBIT_USER="orbit"
readonly ORBIT_GROUP="orbit"
readonly ORBIT_BINARY_PATH="/usr/local/bin/tracking-server"
readonly ORBIT_CONFIG_DIR="/etc/orbit-engine"
readonly ORBIT_ENV_FILE="${ORBIT_CONFIG_DIR}/tracking.env"
readonly TRACKING_HOST="127.0.0.1:9100"
readonly TRACKING_URL="http://${TRACKING_HOST}"
readonly SERVICE_NAME="tracking-server"
readonly SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
readonly STARTUP_TIMEOUT=30
readonly IMDS_BASE="http://169.254.169.254"

ORBIT_ENV="${ORBIT_ENV:-production}"
ORBIT_TOKEN_BUDGET_SESSION="${ORBIT_TOKEN_BUDGET_SESSION:-100000}"
ORBIT_TOKEN_BUDGET_CALL="${ORBIT_TOKEN_BUDGET_CALL:-10000}"
ORBIT_REGION="${ORBIT_REGION:-}"

CURRENT_STEP="init"

# ── Logging estruturado ───────────────────────────────────────────────────────

mkdir -p "$(dirname "${PROVISION_LOG}")"
touch "${PROVISION_LOG}"

_log() {
    local level="$1"
    shift
    local msg="$*"
    local ts
    ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    # Escapar aspas para JSON válido
    local safe="${msg//\\/\\\\}"
    safe="${safe//\"/\\\"}"
    local entry
    printf -v entry '{"ts":"%s","level":"%s","step":"%s","msg":"%s"}' \
        "$ts" "$level" "${CURRENT_STEP}" "$safe"
    echo "$entry" | tee -a "${PROVISION_LOG}"
}

_info()  { _log "INFO"  "$*"; }
_pass()  { _log "PASS"  "$*"; }
_warn()  { _log "WARN"  "$*"; }
_step()  { CURRENT_STEP="$1"; _log "STEP"  "iniciando: $1"; }

# ── Fail-closed ───────────────────────────────────────────────────────────────
#
# Qualquer falha executa _die: loga, tenta destruir a instância e sai com 1.
# A instância corrompida nunca fica em estado parcialmente provisionado.

_die() {
    local msg="${1:-erro desconhecido}"
    _log "FATAL" "${msg}"
    _log "FATAL" "iniciando auto-destruição da instância (fail-closed)"

    local instance_id
    instance_id=$(_imds "/latest/meta-data/instance-id" 2>/dev/null || echo "")

    if [[ -n "${instance_id}" && -n "${ORBIT_REGION}" ]]; then
        _log "FATAL" "terminando instância ${instance_id} na região ${ORBIT_REGION}"
        aws ec2 terminate-instances \
            --instance-ids "${instance_id}" \
            --region "${ORBIT_REGION}" \
            --output text >> "${PROVISION_LOG}" 2>&1 || \
            _log "FATAL" "ec2:TerminateInstances falhou — instância pode precisar de limpeza manual"
    else
        _log "FATAL" "instance-id ou região indisponíveis — não foi possível destruir automaticamente"
    fi

    exit 1
}

# Trap: captura qualquer exit não-zero não tratado explicitamente
trap '_die "falha inesperada em linha ${LINENO}: ${BASH_COMMAND}"' ERR

# ── IMDSv2 ────────────────────────────────────────────────────────────────────

_imds_token() {
    curl -sf \
        -X PUT "${IMDS_BASE}/latest/api/token" \
        -H "X-aws-ec2-metadata-token-ttl-seconds: 60" \
        --max-time 5 \
        2>/dev/null || echo ""
}

_imds() {
    local path="$1"
    local token
    token=$(_imds_token)
    [[ -z "$token" ]] && { echo ""; return 1; }
    curl -sf \
        -H "X-aws-ec2-metadata-token: ${token}" \
        "${IMDS_BASE}${path}" \
        --max-time 5 \
        2>/dev/null || echo ""
}

# ── SSM Parameter Store ───────────────────────────────────────────────────────
#
# _ssm_param <nome>: retorna valor ou string vazia em caso de falha.
# _require_param <var> <ssm_path>: popula var de env ou SSM; _die se ambos ausentes.

_ssm_param() {
    local name="$1"
    aws ssm get-parameter \
        --name "${name}" \
        --with-decryption \
        --region "${ORBIT_REGION}" \
        --query "Parameter.Value" \
        --output text \
        2>/dev/null || echo ""
}

_require_param() {
    local var_name="$1"
    local ssm_path="$2"
    local current_val="${!var_name:-}"

    if [[ -n "${current_val}" ]]; then
        _info "${var_name} carregado do environment"
        return 0
    fi

    _info "${var_name} não definido no environment — buscando em SSM: ${ssm_path}"
    local val
    val=$(_ssm_param "${ssm_path}")

    if [[ -z "${val}" ]]; then
        _die "${var_name} é obrigatório mas não encontrado em environment nem em SSM ${ssm_path}"
    fi

    # Exportar para o ambiente corrente
    export "${var_name}=${val}"
    _info "${var_name} carregado do SSM Parameter Store"
}

# ── SHA-256 ───────────────────────────────────────────────────────────────────

_sha256() {
    local file="$1"
    if command -v sha256sum &>/dev/null; then
        sha256sum "$file" | awk '{print $1}'
    else
        _die "sha256sum não encontrado — impossível validar integridade do artefato"
    fi
}

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 0 — Bootstrap: log, IMDS, região
# ════════════════════════════════════════════════════════════════════════════

_step "0-bootstrap"

PROVISION_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
_info "provision_orbit_node.sh iniciado em ${PROVISION_TS}"
_info "orbit-engine environment: ${ORBIT_ENV}"
_info "Ubuntu release: $(lsb_release -rs 2>/dev/null || echo 'desconhecido')"

# Verificar IMDS (confirma que estamos em EC2)
INSTANCE_ID=$(_imds "/latest/meta-data/instance-id" || echo "")
if [[ -z "${INSTANCE_ID}" ]]; then
    _die "IMDS não responde — não é uma instância EC2 ou IMDSv2 bloqueado"
fi
_info "instância EC2 detectada: ${INSTANCE_ID}"

# Detectar região se não fornecida
if [[ -z "${ORBIT_REGION}" ]]; then
    AZ=$(_imds "/latest/meta-data/placement/availability-zone" || echo "")
    if [[ -z "${AZ}" ]]; then
        _die "não foi possível detectar availability zone — defina ORBIT_REGION manualmente"
    fi
    # Remove o sufixo de AZ para obter a região (ex: us-east-1a → us-east-1)
    ORBIT_REGION="${AZ%?}"
    _info "região detectada via IMDS: ${ORBIT_REGION}"
fi
_pass "bootstrap: instância=${INSTANCE_ID} região=${ORBIT_REGION}"

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 1 — Dependências do sistema
# ════════════════════════════════════════════════════════════════════════════

_step "1-dependencias"

# Aguardar dpkg não estar em uso (pode ocorrer logo após o boot com cloud-init)
_wait_apt() {
    local i=0
    while fuser /var/lib/dpkg/lock-frontend &>/dev/null 2>&1; do
        (( i++ )) || true
        [[ $i -ge 60 ]] && _die "dpkg bloqueado por 60s — outro processo de package manager está rodando"
        sleep 1
    done
}

_wait_apt
export DEBIAN_FRONTEND=noninteractive

apt-get update -qq >> "${PROVISION_LOG}" 2>&1 \
    || _die "apt-get update falhou"

apt-get install -y -qq \
    curl \
    ca-certificates \
    unzip \
    jq \
    >> "${PROVISION_LOG}" 2>&1 \
    || _die "instalação de dependências falhou"

# AWS CLI v2 — instalar apenas se ausente ou versão < 2
if ! command -v aws &>/dev/null || ! aws --version 2>&1 | grep -q "aws-cli/2"; then
    _info "instalando AWS CLI v2..."
    AWSCLI_TMP=$(mktemp -d)
    curl -fsSL \
        "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" \
        -o "${AWSCLI_TMP}/awscliv2.zip" \
        >> "${PROVISION_LOG}" 2>&1 \
        || _die "download do AWS CLI falhou"
    unzip -q "${AWSCLI_TMP}/awscliv2.zip" -d "${AWSCLI_TMP}" \
        >> "${PROVISION_LOG}" 2>&1
    "${AWSCLI_TMP}/aws/install" --update \
        >> "${PROVISION_LOG}" 2>&1 \
        || _die "instalação do AWS CLI falhou"
    rm -rf "${AWSCLI_TMP}"
    _pass "AWS CLI v2 instalado"
else
    _pass "AWS CLI já presente: $(aws --version 2>&1 | head -1)"
fi

_pass "dependências instaladas"

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 2 — Parâmetros obrigatórios (environment ou SSM Parameter Store)
# ════════════════════════════════════════════════════════════════════════════

_step "2-parametros"

_require_param "ORBIT_ARTIFACT_URL"     "/orbit/artifact-url"
_require_param "ORBIT_ARTIFACT_SHA256"  "/orbit/artifact-sha256"
_require_param "ORBIT_EXPECTED_COMMIT"  "/orbit/expected-commit"
_require_param "ORBIT_RECONCILE_SECRET" "/orbit/reconcile-secret"

# HMAC para /track — opcional; warn se ausente
ORBIT_HMAC_SECRET="${ORBIT_HMAC_SECRET:-$(_ssm_param "/orbit/hmac-secret" 2>/dev/null || echo "")}"
if [[ -z "${ORBIT_HMAC_SECRET}" ]]; then
    _warn "ORBIT_HMAC_SECRET não definido — /track operará sem autenticação HMAC"
fi

_pass "todos os parâmetros obrigatórios carregados"

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 3 — Usuário e grupo orbit (idempotente)
# ════════════════════════════════════════════════════════════════════════════

_step "3-usuario"

if ! getent group "${ORBIT_GROUP}" &>/dev/null; then
    groupadd --system "${ORBIT_GROUP}"
    _pass "grupo '${ORBIT_GROUP}' criado"
else
    _info "grupo '${ORBIT_GROUP}' já existe — idempotente"
fi

if ! id "${ORBIT_USER}" &>/dev/null; then
    useradd \
        --system \
        --gid "${ORBIT_GROUP}" \
        --no-create-home \
        --shell /usr/sbin/nologin \
        --comment "orbit-engine tracking server" \
        "${ORBIT_USER}"
    _pass "usuário '${ORBIT_USER}' criado"
else
    _info "usuário '${ORBIT_USER}' já existe — idempotente"
fi

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 4 — Diretórios e permissões
# ════════════════════════════════════════════════════════════════════════════

_step "4-diretorios"

mkdir -p "${ORBIT_CONFIG_DIR}"
chmod 750 "${ORBIT_CONFIG_DIR}"
chown root:"${ORBIT_GROUP}" "${ORBIT_CONFIG_DIR}"

# Log de auditoria de deploy
DEPLOY_AUDIT_LOG="/var/log/orbit-deploy-audit.log"
touch "${DEPLOY_AUDIT_LOG}"
chown root:"${ORBIT_GROUP}" "${DEPLOY_AUDIT_LOG}"
chmod 640 "${DEPLOY_AUDIT_LOG}"

_pass "diretórios configurados"

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 5 — Aquisição e validação do artefato (idempotente por SHA-256)
# ════════════════════════════════════════════════════════════════════════════

_step "5-artefato"

# Idempotência: pular download se binário já está instalado com SHA correto
SKIP_INSTALL=0
if [[ -f "${ORBIT_BINARY_PATH}" ]]; then
    EXISTING_SHA=$(_sha256 "${ORBIT_BINARY_PATH}")
    EXPECTED_SHA=$(echo "${ORBIT_ARTIFACT_SHA256}" | awk '{print $1}')
    if [[ "${EXISTING_SHA}" == "${EXPECTED_SHA}" ]]; then
        _info "binário já instalado com SHA correto (${EXISTING_SHA:0:16}…) — pulando download"
        SKIP_INSTALL=1
    else
        _info "SHA diverge — atualizando binário: instalado=${EXISTING_SHA:0:16}… esperado=${EXPECTED_SHA:0:16}…"
    fi
fi

if [[ "${SKIP_INSTALL}" -eq 0 ]]; then
    TEMP_BINARY=$(mktemp /tmp/orbit-tracking-server-XXXXXX)
    # Garantir remoção do arquivo temporário em qualquer saída
    trap 'rm -f "${TEMP_BINARY}" 2>/dev/null || true; _die "falha inesperada em linha ${LINENO}: ${BASH_COMMAND}"' ERR

    _info "baixando artefato de: ${ORBIT_ARTIFACT_URL}"

    if [[ "${ORBIT_ARTIFACT_URL}" == s3://* ]]; then
        aws s3 cp "${ORBIT_ARTIFACT_URL}" "${TEMP_BINARY}" \
            --region "${ORBIT_REGION}" \
            >> "${PROVISION_LOG}" 2>&1 \
            || _die "aws s3 cp falhou — verifique permissão s3:GetObject e o bucket"
    else
        curl -fsSL \
            --max-time 120 \
            --retry 3 \
            --retry-delay 5 \
            --output "${TEMP_BINARY}" \
            "${ORBIT_ARTIFACT_URL}" \
            >> "${PROVISION_LOG}" 2>&1 \
            || _die "download HTTP falhou: ${ORBIT_ARTIFACT_URL}"
    fi

    _info "validando SHA-256..."
    ACTUAL_SHA=$(_sha256 "${TEMP_BINARY}")
    EXPECTED_SHA=$(echo "${ORBIT_ARTIFACT_SHA256}" | awk '{print $1}')

    if [[ "${ACTUAL_SHA}" != "${EXPECTED_SHA}" ]]; then
        rm -f "${TEMP_BINARY}"
        _die "SHA-256 inválido — esperado=${EXPECTED_SHA} calculado=${ACTUAL_SHA} — artefato corrompido ou substituído"
    fi
    _pass "SHA-256 validado: ${ACTUAL_SHA:0:16}…"

    # ── Etapa 6: Instalação atômica ──────────────────────────────────────
    # rename(2) é atômico no mesmo filesystem — sem janela de binário corrompido
    _step "6-instalacao-atomica"

    chmod +x "${TEMP_BINARY}"

    # Backup do binário anterior para rollback manual se necessário
    if [[ -f "${ORBIT_BINARY_PATH}" ]]; then
        cp "${ORBIT_BINARY_PATH}" "${ORBIT_BINARY_PATH}.bak"
        _info "backup: ${ORBIT_BINARY_PATH}.bak"
    fi

    mv "${TEMP_BINARY}" "${ORBIT_BINARY_PATH}"
    # Cancelar trap de remoção do temp (já movido)
    trap '_die "falha inesperada em linha ${LINENO}: ${BASH_COMMAND}"' ERR

    chown root:root "${ORBIT_BINARY_PATH}"
    chmod 755 "${ORBIT_BINARY_PATH}"

    _pass "binário instalado atomicamente em ${ORBIT_BINARY_PATH}"

    # Registrar auditoria
    printf '{"ts":"%s","commit":"%s","sha256":"%s","artifact":"%s","instance":"%s"}\n' \
        "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        "${ORBIT_EXPECTED_COMMIT}" \
        "${ACTUAL_SHA}" \
        "${ORBIT_ARTIFACT_URL}" \
        "${INSTANCE_ID}" \
        >> "${DEPLOY_AUDIT_LOG}"
else
    _step "6-instalacao-atomica"
    _info "binário inalterado (idempotente)"
fi

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 7 — Arquivo de environment (/etc/orbit-engine/tracking.env)
# ════════════════════════════════════════════════════════════════════════════

_step "7-environment"

# Escrever atomicamente via arquivo temporário
ENV_TMP=$(mktemp "${ORBIT_CONFIG_DIR}/.tracking.env.XXXXXX")

cat > "${ENV_TMP}" <<EOF
# orbit-engine tracking-server environment
# Gerado por provision_orbit_node.sh em $(date -u +"%Y-%m-%dT%H:%M:%SZ")
# NÃO editar manualmente — rerun provision_orbit_node.sh para atualizar.

ORBIT_ENV=${ORBIT_ENV}

# Expor em todas interfaces para scrape por Prometheus externo.
# Segurança: security group deve restringir porta 9100 à VPC de monitoramento.
ORBIT_BIND_ALL=1

# Secrets de autenticação
ORBIT_RECONCILE_SECRET=${ORBIT_RECONCILE_SECRET}
EOF

if [[ -n "${ORBIT_HMAC_SECRET}" ]]; then
    echo "ORBIT_HMAC_SECRET=${ORBIT_HMAC_SECRET}" >> "${ENV_TMP}"
fi

# Permissões restritas: apenas root e grupo orbit podem ler (secrets estão aqui)
chown root:"${ORBIT_GROUP}" "${ENV_TMP}"
chmod 640 "${ENV_TMP}"

# Substituição atômica
mv "${ENV_TMP}" "${ORBIT_ENV_FILE}"
_pass "environment escrito em ${ORBIT_ENV_FILE} (mode 640)"

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 8 — Unit systemd (idempotente)
# ════════════════════════════════════════════════════════════════════════════

_step "8-systemd"

cat > "${SERVICE_FILE}" <<EOF
[Unit]
Description=orbit-engine Tracking Server
Documentation=https://github.com/IanVDev/orbit-engine
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${ORBIT_USER}
Group=${ORBIT_GROUP}

# Fail-closed: restart forever — tracking nunca pode ficar sem servidor.
Restart=always
RestartSec=3
StartLimitIntervalSec=0

ExecStart=${ORBIT_BINARY_PATH} \\
    --model-control=locked \\
    --token-budget-session=${ORBIT_TOKEN_BUDGET_SESSION} \\
    --token-budget-call=${ORBIT_TOKEN_BUDGET_CALL}

# Logs estruturados para journald.
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadOnlyPaths=/

# Resource limits
LimitNOFILE=65536
MemoryMax=512M

# Secrets e configuração — nunca commitados.
EnvironmentFile=${ORBIT_ENV_FILE}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload >> "${PROVISION_LOG}" 2>&1 \
    || _die "systemctl daemon-reload falhou"

systemctl enable "${SERVICE_NAME}" >> "${PROVISION_LOG}" 2>&1 \
    || _die "systemctl enable ${SERVICE_NAME} falhou"

_pass "unit ${SERVICE_FILE} instalada e habilitada"

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 9 — Iniciar / reiniciar serviço
# ════════════════════════════════════════════════════════════════════════════

_step "9-iniciar-servico"

if systemctl is-active --quiet "${SERVICE_NAME}"; then
    _info "serviço já ativo — reiniciando para aplicar nova configuração"
    systemctl restart "${SERVICE_NAME}" >> "${PROVISION_LOG}" 2>&1 \
        || _die "systemctl restart falhou — ver journalctl -u ${SERVICE_NAME}"
    _pass "serviço reiniciado"
else
    systemctl start "${SERVICE_NAME}" >> "${PROVISION_LOG}" 2>&1 \
        || _die "systemctl start falhou — ver journalctl -u ${SERVICE_NAME}"
    _pass "serviço iniciado"
fi

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 10 — Validação final (fail-closed)
# ════════════════════════════════════════════════════════════════════════════

_step "10-validacao"

# 10a. systemctl is-active
_info "verificando systemctl is-active ${SERVICE_NAME}..."
ELAPSED=0
until systemctl is-active --quiet "${SERVICE_NAME}"; do
    (( ELAPSED++ )) || true
    [[ $ELAPSED -ge $STARTUP_TIMEOUT ]] && \
        _die "${SERVICE_NAME} não atingiu estado 'active' em ${STARTUP_TIMEOUT}s — $(systemctl status "${SERVICE_NAME}" --no-pager 2>&1 | tail -5)"
    sleep 1
done
_pass "systemctl: ${SERVICE_NAME} está active"

# 10b. /health — polling até o servidor responder
_info "aguardando tracking server em ${TRACKING_URL}/health (timeout: ${STARTUP_TIMEOUT}s)..."
ELAPSED=0
until curl -sf --max-time 2 "${TRACKING_URL}/health" | grep -q "ok"; do
    (( ELAPSED++ )) || true
    [[ $ELAPSED -ge $STARTUP_TIMEOUT ]] && \
        _die "/health não respondeu com 'ok' em ${STARTUP_TIMEOUT}s (último status: $(curl -sI --max-time 2 "${TRACKING_URL}/health" 2>/dev/null | head -1))"
    sleep 1
done
_pass "/health responde: ok"

# 10c. /metrics — verificar HTTP 200
METRICS_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" --max-time 5 "${TRACKING_URL}/metrics" 2>/dev/null || echo "000")
if [[ "${METRICS_STATUS}" != "200" ]]; then
    _die "/metrics retornou HTTP ${METRICS_STATUS} (esperado: 200) — tracking server não está expondo métricas"
fi
_pass "/metrics retorna HTTP 200"

# 10d. métricas obrigatórias presentes
_info "verificando métricas obrigatórias em /metrics..."
METRICS_BODY=$(curl -sf --max-time 5 "${TRACKING_URL}/metrics" 2>/dev/null || echo "")
REQUIRED_METRICS=(
    "orbit_skill_activations_total"
    "orbit_tracking_rejected_total"
    "orbit_behavior_abuse_total"
    "orbit_tracking_up"
)
for metric in "${REQUIRED_METRICS[@]}"; do
    if echo "${METRICS_BODY}" | grep -q "^${metric}"; then
        _pass "métrica presente: ${metric}"
    else
        _die "métrica obrigatória AUSENTE: ${metric} — binário instalado pode ser versão incorreta"
    fi
done

# 10e. /v1/runtime — validar commit deployado
_info "validando commit em /v1/runtime..."
RUNTIME_BODY=$(curl -sf --max-time 5 "${TRACKING_URL}/v1/runtime" 2>/dev/null || echo "{}")
ACTUAL_COMMIT=$(echo "${RUNTIME_BODY}" | python3 -c \
    "import json,sys; d=json.load(sys.stdin); print(d.get('commit',''))" 2>/dev/null || echo "")

if [[ "${ACTUAL_COMMIT}" != "${ORBIT_EXPECTED_COMMIT}" ]]; then
    _die "/v1/runtime commit mismatch — esperado=${ORBIT_EXPECTED_COMMIT} binário=${ACTUAL_COMMIT}"
fi
_pass "/v1/runtime commit validado: ${ACTUAL_COMMIT}"

# ════════════════════════════════════════════════════════════════════════════
# CONCLUSÃO
# ════════════════════════════════════════════════════════════════════════════

_step "concluido"

END_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

printf '{"ts":"%s","level":"PASS","step":"concluido","msg":"provisionamento concluido","instance":"%s","commit":"%s","region":"%s","metrics_url":"http://%s:9100/metrics"}\n' \
    "${END_TS}" \
    "${INSTANCE_ID}" \
    "${ACTUAL_COMMIT}" \
    "${ORBIT_REGION}" \
    "$(curl -sf --max-time 3 "${IMDS_BASE}/latest/meta-data/local-ipv4" 2>/dev/null || echo "127.0.0.1")" \
    | tee -a "${PROVISION_LOG}"

_info "log completo: ${PROVISION_LOG}"
_info "auditoria de deploy: ${DEPLOY_AUDIT_LOG}"
_info "logs do serviço: journalctl -u ${SERVICE_NAME} -f"
