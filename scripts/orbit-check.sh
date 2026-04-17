#!/usr/bin/env bash
# orbit-check.sh — Verificação de integridade de produção para o orbit-engine.
#
# Executa validações em camadas com classificação determinística de falhas:
#
#   service_down       → serviço não responde (curl falhou ou timeout)
#   health_invalid     → /health respondeu, mas JSON não tem status=="ok"
#   metrics_missing    → /metrics acessível, mas métrica crítica ausente
#   integrity_mismatch → SHA-256 do gateway não bate com ORBIT_GATEWAY_SHA256
#
# Em ENV=production:
#   - ORBIT_GATEWAY_SHA256 é OBRIGATÓRIO (fail-closed)
#   - Falha em qualquer camada encerra com exit 1
#
# Uso:
#   ./scripts/orbit-check.sh
#   ENV=production ORBIT_GATEWAY_SHA256=<sha> ./scripts/orbit-check.sh
#
# Variáveis de ambiente:
#   ENV                    ambiente ("production" ativa regras estritas)
#   ORBIT_GATEWAY_SHA256   SHA-256 esperado do binário do gateway
#   TRACKING_HOST          host:porta do tracking server  (padrão: 127.0.0.1:9100)
#   GATEWAY_HOST           host:porta do gateway          (padrão: 127.0.0.1:9091)
#   GATEWAY_BIN            caminho do binário do gateway  (padrão: ./tracking/orbit-gateway)
#
# Retornos:
#   0 → todas as verificações passaram (GO)
#   1 → uma ou mais verificações falharam (NO-GO)
#   2 → erro fatal de configuração (ex: SHA ausente em produção)

set -uo pipefail

# ── Configuração ─────────────────────────────────────────────────────────────

TRACKING_HOST="${TRACKING_HOST:-127.0.0.1:9100}"
GATEWAY_HOST="${GATEWAY_HOST:-127.0.0.1:9091}"
GATEWAY_BIN="${GATEWAY_BIN:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/tracking/orbit-gateway}"
ENV="${ENV:-}"
ORBIT_GATEWAY_SHA256="${ORBIT_GATEWAY_SHA256:-}"

TRACKING_URL="http://${TRACKING_HOST}"
GATEWAY_URL="http://${GATEWAY_HOST}"

# Métricas críticas obrigatórias em /metrics
CRITICAL_METRICS=(
    "orbit_skill_activations_total"
    "orbit_tracking_rejected_total"
    "orbit_behavior_abuse_total"
)

# ── Cores ─────────────────────────────────────────────────────────────────────

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ── Estado global ─────────────────────────────────────────────────────────────

PASS=0
FAIL=0

# Array de falhas classificadas: "<código>:<mensagem>"
# Inicializado explicitamente (compatível com bash 3.2)
FAILURES=()

# ── Helpers ───────────────────────────────────────────────────────────────────

_header() {
    echo ""
    echo -e "${CYAN}${BOLD}════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}${BOLD}  $1${NC}"
    echo -e "${CYAN}${BOLD}════════════════════════════════════════════════════════${NC}"
}

_step() {
    echo ""
    echo -e "${BOLD}── $1${NC}"
}

_pass() {
    echo -e "  ${GREEN}[PASS]${NC} $1"
    ((PASS++)) || true
}

_fail() {
    local code="$1"
    local msg="$2"
    echo -e "  ${RED}[FAIL]${NC} [${code}] ${msg}"
    ((FAIL++)) || true
    FAILURES+=("${code}:${msg}")
}

_warn() {
    echo -e "  ${YELLOW}[WARN]${NC} $1"
}

_info() {
    echo -e "        $1"
}

# _curl_health <url>
# Retorna o corpo HTTP (stdout) e o código de status (último campo).
# Timeout fixo: 2s (fail-fast, sem dependências lentas).
_curl_get() {
    local url="$1"
    curl -sf --max-time 2 "$url" 2>/dev/null
}

_curl_status() {
    local url="$1"
    curl -sf -o /dev/null -w "%{http_code}" --max-time 2 "$url" 2>/dev/null || echo "000"
}

# _require <cmd>
_require() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo -e "${RED}ERRO FATAL: '$1' não encontrado. É necessário para o orbit-check.${NC}"
        exit 2
    fi
}

# Extrai valor de uma métrica Prometheus do texto de /metrics
_metric_present() {
    local name="$1"
    local text="$2"
    echo "$text" | grep -q "^${name}"
}

# ── Pré-requisitos ────────────────────────────────────────────────────────────

_require curl

# python3 é usado apenas para validação de JSON — sem bibliotecas externas
_require python3

# ── Cabeçalho ─────────────────────────────────────────────────────────────────

START_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

_header "orbit-engine — ORBIT-CHECK"
echo ""
echo -e "  Início:       ${START_TS}"
echo -e "  Ambiente:     ${ENV:-dev}"
echo -e "  Tracking:     ${TRACKING_URL}"
echo -e "  Gateway:      ${GATEWAY_URL}"
echo ""

# ════════════════════════════════════════════════════════════════════════════
# VERIFICAÇÃO 0 — SHA obrigatório em produção (fail-closed imediato)
# ════════════════════════════════════════════════════════════════════════════

_header "0/4 — Verificação de integridade de configuração"

if [[ "$ENV" == "production" ]]; then
    _step "ENV=production detectado — aplicando regras estritas"

    if [[ -z "$ORBIT_GATEWAY_SHA256" ]]; then
        echo ""
        echo -e "${RED}${BOLD}  ERRO FATAL (integrity_mismatch)${NC}"
        echo -e "  ${RED}ENV=production mas ORBIT_GATEWAY_SHA256 não está definido.${NC}"
        echo -e "  ${RED}Defina a variável antes de executar o orbit-check em produção:${NC}"
        echo ""
        echo -e "    export ORBIT_GATEWAY_SHA256=\$(sha256sum ./tracking/orbit-gateway | awk '{print \$1}')"
        echo ""
        echo -e "  ${RED}Princípio fail-closed: execução abortada.${NC}"
        echo ""
        exit 2
    fi

    _pass "ORBIT_GATEWAY_SHA256 definido (${ORBIT_GATEWAY_SHA256:0:16}…)"
else
    if [[ -z "$ORBIT_GATEWAY_SHA256" ]]; then
        _warn "ORBIT_GATEWAY_SHA256 não definido — verificação de integridade será pulada (apenas em dev)"
    else
        _pass "ORBIT_GATEWAY_SHA256 definido (${ORBIT_GATEWAY_SHA256:0:16}…)"
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# VERIFICAÇÃO 1 — Health check com validação de conteúdo JSON
# ════════════════════════════════════════════════════════════════════════════

_header "1/4 — Health check (status JSON)"

# ── 1a. Tracking server ──────────────────────────────────────────────────────

_step "GET ${TRACKING_URL}/health (--max-time 2)"
TRACKING_HEALTH=$(curl -sf --max-time 2 "${TRACKING_URL}/health" 2>/dev/null) || TRACKING_HEALTH=""

if [[ -z "$TRACKING_HEALTH" ]]; then
    _fail "service_down" "tracking server não responde em ${TRACKING_URL}/health"
else
    # Validar conteúdo JSON: {"status":"ok"} ou texto simples "ok"
    TRACKING_STATUS=$(echo "$TRACKING_HEALTH" | python3 -c "
import json, sys
raw = sys.stdin.read().strip()
try:
    d = json.loads(raw)
    print(d.get('status', '__no_status__'))
except json.JSONDecodeError:
    # Aceitar resposta de texto simples 'ok' (compatibilidade)
    print(raw if raw == 'ok' else '__invalid__')
" 2>/dev/null || echo "__parse_error__")

    if [[ "$TRACKING_STATUS" == "ok" ]]; then
        _pass "tracking server saudável (status=ok)"
    else
        _fail "health_invalid" "tracking /health retornou status='${TRACKING_STATUS}' (esperado: 'ok') — corpo: [${TRACKING_HEALTH}]"
    fi
fi

# ── 1b. Gateway ──────────────────────────────────────────────────────────────

_step "GET ${GATEWAY_URL}/health (--max-time 2)"
GATEWAY_HEALTH=$(curl -sf --max-time 2 "${GATEWAY_URL}/health" 2>/dev/null) || GATEWAY_HEALTH=""

if [[ -z "$GATEWAY_HEALTH" ]]; then
    _fail "service_down" "gateway não responde em ${GATEWAY_URL}/health"
else
    GATEWAY_STATUS=$(echo "$GATEWAY_HEALTH" | python3 -c "
import json, sys
raw = sys.stdin.read().strip()
try:
    d = json.loads(raw)
    print(d.get('status', '__no_status__'))
except json.JSONDecodeError:
    print(raw if raw == 'ok' else '__invalid__')
" 2>/dev/null || echo "__parse_error__")

    if [[ "$GATEWAY_STATUS" == "ok" ]]; then
        _pass "gateway saudável (status=ok)"
    else
        _fail "health_invalid" "gateway /health retornou status='${GATEWAY_STATUS}' (esperado: 'ok') — corpo: [${GATEWAY_HEALTH}]"
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# VERIFICAÇÃO 2 — Métricas críticas obrigatórias
# ════════════════════════════════════════════════════════════════════════════

_header "2/4 — Métricas críticas em /metrics"

_step "GET ${TRACKING_URL}/metrics (--max-time 2)"
METRICS_BODY=$(curl -sf --max-time 2 "${TRACKING_URL}/metrics" 2>/dev/null) || METRICS_BODY=""

if [[ -z "$METRICS_BODY" ]]; then
    _fail "service_down" "tracking /metrics não responde (serviço fora do ar ou timeout)"
else
    _pass "/metrics acessível (${#METRICS_BODY} bytes)"

    for metric in "${CRITICAL_METRICS[@]}"; do
        if _metric_present "$metric" "$METRICS_BODY"; then
            # Extrair primeiro valor numérico da métrica
            val=$(echo "$METRICS_BODY" | awk "/^${metric}[{ ]/{print \$2; exit}")
            _pass "${metric} presente${val:+ (valor=${val})}"
        else
            _fail "metrics_missing" "${metric} AUSENTE em /metrics — métrica crítica não encontrada"
        fi
    done
fi

# ════════════════════════════════════════════════════════════════════════════
# VERIFICAÇÃO 3 — Integridade do binário (SHA-256)
# ════════════════════════════════════════════════════════════════════════════

_header "3/4 — Integridade do binário (SHA-256)"

if [[ -z "$ORBIT_GATEWAY_SHA256" ]]; then
    _warn "ORBIT_GATEWAY_SHA256 não definido — verificação de integridade pulada"
    _info "Em produção este check é obrigatório (ENV=production aborta sem SHA)"
else
    _step "Calculando SHA-256 de: ${GATEWAY_BIN}"

    if [[ ! -f "$GATEWAY_BIN" ]]; then
        _fail "integrity_mismatch" "binário não encontrado: ${GATEWAY_BIN}"
    else
        # sha256sum (Linux) ou shasum -a 256 (macOS)
        if command -v sha256sum >/dev/null 2>&1; then
            ACTUAL_SHA=$(sha256sum "$GATEWAY_BIN" | awk '{print $1}')
        elif command -v shasum >/dev/null 2>&1; then
            ACTUAL_SHA=$(shasum -a 256 "$GATEWAY_BIN" | awk '{print $1}')
        else
            _warn "sha256sum e shasum não encontrados — verificação de integridade pulada"
            ACTUAL_SHA=""
        fi

        if [[ -n "$ACTUAL_SHA" ]]; then
            if [[ "$ACTUAL_SHA" == "$ORBIT_GATEWAY_SHA256" ]]; then
                _pass "SHA-256 confere: ${ACTUAL_SHA:0:16}…"
            else
                _fail "integrity_mismatch" "SHA-256 diverge — esperado: ${ORBIT_GATEWAY_SHA256:0:16}…, obtido: ${ACTUAL_SHA:0:16}…"
                _info "Binário pode ter sido modificado ou substituído"
            fi
        fi
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# RESULTADO FINAL — GO / NO-GO com classificação de falhas
# ════════════════════════════════════════════════════════════════════════════

END_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  Fim:    ${END_TS}"
echo -e "  PASS:   ${PASS}"
echo -e "  FAIL:   ${FAIL}"
echo ""

if [[ "${#FAILURES[@]}" -gt 0 ]]; then
    echo -e "  ${RED}${BOLD}Falhas classificadas:${NC}"
    # Exibir agrupado por código (bash 3.2 compatible — sem declare -A)
    for code in service_down health_invalid metrics_missing integrity_mismatch; do
        _printed=0
        for entry in "${FAILURES[@]}"; do
            entry_code="${entry%%:*}"
            entry_msg="${entry#*:}"
            if [[ "$entry_code" == "$code" ]]; then
                if [[ "$_printed" -eq 0 ]]; then
                    echo -e "  ${RED}[${code}]${NC}"
                    _printed=1
                fi
                echo -e "    • ${entry_msg}"
            fi
        done
    done
    echo ""
fi

if [[ "$FAIL" -eq 0 ]]; then
    echo -e "${GREEN}${BOLD}  VEREDITO: GO ✅${NC}"
    echo ""
    echo -e "  ${GREEN}Todas as verificações passaram. Sistema operacional.${NC}"
    echo ""
    exit 0
else
    echo -e "${RED}${BOLD}  VEREDITO: NO-GO ❌${NC}"
    echo ""
    echo -e "  ${RED}${FAIL} verificação(ões) falharam. Sistema NÃO está pronto.${NC}"
    echo ""
    exit 1
fi
