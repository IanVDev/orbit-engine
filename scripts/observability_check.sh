#!/usr/bin/env bash
# observability_check.sh — Diagnóstico automático de observabilidade do orbit-engine.
#
# Testa cada camada do pipeline: /metrics → scrape Prometheus → ingestão → PromQL.
# Retorna GO (exit 0) ou NO-GO (exit 1).
#
# Uso:
#   ./scripts/observability_check.sh
#
# Variáveis de ambiente (opcionais):
#   TRACKING_HOST   host:porta do tracking server  (padrão: 127.0.0.1:9100)
#   PROM_HOST       host:porta do Prometheus        (padrão: 127.0.0.1:9090)
#   GATEWAY_HOST    host:porta do gateway PromQL    (padrão: 127.0.0.1:9091)
#   SCRAPE_WAIT     segundos para aguardar scrape   (padrão: 12)

set -uo pipefail

# ── Configuração ────────────────────────────────────────────────────────────

TRACKING_HOST="${TRACKING_HOST:-127.0.0.1:9100}"
PROM_HOST="${PROM_HOST:-127.0.0.1:9090}"
GATEWAY_HOST="${GATEWAY_HOST:-127.0.0.1:9091}"
SCRAPE_WAIT="${SCRAPE_WAIT:-12}"

TRACKING_URL="http://${TRACKING_HOST}"
PROM_URL="http://${PROM_HOST}"
GATEWAY_URL="http://${GATEWAY_HOST}"

# Métricas obrigatórias que devem aparecer em /metrics
REQUIRED_METRICS=(
    "orbit_skill_activations_total"
    "orbit_skill_tokens_saved_total"
    "orbit_heartbeat_total"
    "orbit_tracking_up"
    "orbit_seed_mode"
    "orbit_last_event_timestamp"
)

# ── Cores ────────────────────────────────────────────────────────────────────

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ── Estado global ────────────────────────────────────────────────────────────

PASS=0
FAIL=0
WARN=0

# ── Helpers ──────────────────────────────────────────────────────────────────

_header() {
    echo ""
    echo -e "${CYAN}${BOLD}══════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}${BOLD}  $1${NC}"
    echo -e "${CYAN}${BOLD}══════════════════════════════════════════════════════════${NC}"
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
    echo -e "  ${RED}[FAIL]${NC} $1"
    ((FAIL++)) || true
}

_warn() {
    echo -e "  ${YELLOW}[WARN]${NC} $1"
    ((WARN++)) || true
}

_info() {
    echo -e "        $1"
}

# Extrai o valor numérico de uma métrica do texto /metrics
# Uso: _metric_value "orbit_tracking_up" <texto_metrics>
_metric_value() {
    local name="$1"
    local text="$2"
    echo "$text" | awk "/^${name}[{ ]/{print \$2; exit}"
}

# Verifica se curl está disponível
_require_curl() {
    if ! command -v curl >/dev/null 2>&1; then
        echo -e "${RED}ERRO FATAL: curl não encontrado. Instale curl e tente novamente.${NC}"
        exit 2
    fi
}

# Verifica se python3 está disponível (usado para encode de URL e parse JSON)
_require_python() {
    if ! command -v python3 >/dev/null 2>&1; then
        echo -e "${RED}ERRO FATAL: python3 não encontrado. Instale python3 e tente novamente.${NC}"
        exit 2
    fi
}

# ── Início ────────────────────────────────────────────────────────────────────

_require_curl
_require_python

START_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

_header "orbit-engine — OBSERVABILITY CHECK"
echo ""
echo -e "  Início:          ${START_TS}"
echo -e "  tracking server: ${TRACKING_URL}"
echo -e "  prometheus:      ${PROM_URL}"
echo -e "  gateway PromQL:  ${GATEWAY_URL}"
echo -e "  scrape_wait:     ${SCRAPE_WAIT}s"

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 1 — /metrics endpoint
# ════════════════════════════════════════════════════════════════════════════

_header "ETAPA 1/6 — Validação do endpoint /metrics"

_step "Checando acessibilidade: GET ${TRACKING_URL}/metrics"
METRICS_RAW=$(curl -sf --max-time 5 "${TRACKING_URL}/metrics" 2>/dev/null) || true

if [[ -z "$METRICS_RAW" ]]; then
    _fail "Endpoint ${TRACKING_URL}/metrics não respondeu"
    _info "Verifique se o tracking-server está rodando: cd tracking && go run ./cmd/main.go"
    METRICS_ACCESSIBLE=0
else
    _pass "Endpoint /metrics acessível (${#METRICS_RAW} bytes)"
    METRICS_ACCESSIBLE=1
fi

if [[ "$METRICS_ACCESSIBLE" -eq 1 ]]; then
    _step "Verificando métricas obrigatórias em /metrics"
    for metric in "${REQUIRED_METRICS[@]}"; do
        if echo "$METRICS_RAW" | grep -q "^${metric}"; then
            val=$(_metric_value "$metric" "$METRICS_RAW")
            _pass "${metric} = ${val:-<presente, sem valor numérico simples>}"
        else
            _fail "${metric} AUSENTE em /metrics"
        fi
    done

    _step "Verificando orbit_tracking_up == 1"
    UP_VAL=$(_metric_value "orbit_tracking_up" "$METRICS_RAW")
    if [[ "$UP_VAL" == "1" ]]; then
        _pass "orbit_tracking_up = 1 (processo vivo)"
    else
        _fail "orbit_tracking_up = ${UP_VAL:-AUSENTE} (esperado: 1)"
    fi

    _step "Verificando orbit_seed_mode == 0 (não é seed/dev)"
    SEED_VAL=$(_metric_value "orbit_seed_mode" "$METRICS_RAW")
    if [[ "$SEED_VAL" == "0" ]]; then
        _pass "orbit_seed_mode = 0 (modo produção)"
    elif [[ "$SEED_VAL" == "1" ]]; then
        _warn "orbit_seed_mode = 1 — este é um processo seed/dev, não produção"
    else
        _fail "orbit_seed_mode = ${SEED_VAL:-AUSENTE}"
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 2 — Prometheus scrape targets
# ════════════════════════════════════════════════════════════════════════════

_header "ETAPA 2/6 — Validação do scrape Prometheus"

_step "Verificando saúde do Prometheus: GET ${PROM_URL}/-/healthy"
if curl -sf --max-time 5 "${PROM_URL}/-/healthy" >/dev/null 2>&1; then
    _pass "Prometheus respondeu em /-/healthy"
    PROM_UP=1
else
    _warn "Prometheus não acessível em ${PROM_URL}/-/healthy — etapa 2 e 4 serão parciais"
    PROM_UP=0
fi

if [[ "$PROM_UP" -eq 1 ]]; then
    _step "Consultando targets via API: GET ${PROM_URL}/api/v1/targets"
    TARGETS_JSON=$(curl -sf --max-time 5 "${PROM_URL}/api/v1/targets" 2>/dev/null) || true

    if [[ -z "$TARGETS_JSON" ]]; then
        _fail "Não foi possível obter targets do Prometheus"
    else
        # Verificar cada job obrigatório
        for job in "orbit-engine-tracking" "orbit-engine-gateway"; do
            JOB_STATE=$(echo "$TARGETS_JSON" | python3 -c "
import json, sys
d = json.load(sys.stdin)
targets = d.get('data', {}).get('activeTargets', [])
for t in targets:
    if t.get('labels', {}).get('job') == '${job}':
        print(t.get('health', 'unknown'))
        break
else:
    print('not_found')
" 2>/dev/null || echo "parse_error")

            if [[ "$JOB_STATE" == "up" ]]; then
                _pass "job=${job} → health=up (scrape OK)"
            elif [[ "$JOB_STATE" == "not_found" ]]; then
                _fail "job=${job} → NÃO ENCONTRADO nos targets do Prometheus"
                _info "Verifique prometheus.yml: job_name deve ser '${job}'"
            else
                _fail "job=${job} → health=${JOB_STATE} (scrape falhando)"
                _info "Veja detalhes em: ${PROM_URL}/targets"
            fi
        done

        # Mostrar erros de scrape, se houver
        SCRAPE_ERRORS=$(echo "$TARGETS_JSON" | python3 -c "
import json, sys
d = json.load(sys.stdin)
errs = []
for t in d.get('data', {}).get('activeTargets', []):
    if t.get('lastError'):
        errs.append('{}: {}'.format(t.get('labels',{}).get('job','?'), t.get('lastError','')))
print('\n'.join(errs) if errs else '')
" 2>/dev/null || echo "")

        if [[ -n "$SCRAPE_ERRORS" ]]; then
            _warn "Erros de scrape detectados:"
            echo "$SCRAPE_ERRORS" | while IFS= read -r line; do
                _info "  $line"
            done
        fi
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 3 — Geração de carga (POST /track)
# ════════════════════════════════════════════════════════════════════════════

_header "ETAPA 3/6 — Geração de carga via POST /track"

SESSION_ID="obs-check-$(date +%s)"
LOAD_OK=0

if [[ "$METRICS_ACCESSIBLE" -eq 0 ]]; then
    _warn "tracking-server inacessível — pulando geração de carga"
else
    _step "Capturando baseline de orbit_skill_tokens_saved_total"
    BASELINE_TOKENS=$(_metric_value "orbit_skill_tokens_saved_total" "$METRICS_RAW")
    _info "baseline tokens_saved = ${BASELINE_TOKENS:-0}"

    _step "Injetando 3 eventos de ativação (session=${SESSION_ID})"
    INJECT_FAIL=0
    for i in 1 2 3; do
        TOKENS=$((300 + i * 100))
        WASTE=$((100 + i * 50))
        TS=$(python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")
        PAYLOAD=$(printf '{"event_type":"activation","timestamp":"%s","session_id":"%s","mode":"auto","trigger":"observability_check","estimated_waste":%d,"actions_suggested":2,"actions_applied":1,"impact_estimated_tokens":%d}' \
            "$TS" "$SESSION_ID" "$WASTE" "$TOKENS")

        RESP=$(curl -sf --max-time 5 -X POST "${TRACKING_URL}/track" \
            -H "Content-Type: application/json" \
            -d "$PAYLOAD" 2>/dev/null) || RESP=""

        STATUS=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null || echo "erro")
        if [[ "$STATUS" == "ok" ]]; then
            _pass "evento ${i} aceito (tokens=${TOKENS}, waste=${WASTE})"
        else
            _fail "evento ${i} rejeitado — resp=[${RESP}]"
            ((INJECT_FAIL++)) || true
        fi
        sleep 0.1
    done

    if [[ "$INJECT_FAIL" -eq 0 ]]; then
        LOAD_OK=1
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 4 — Validação de ingestão (métricas incrementaram?)
# ════════════════════════════════════════════════════════════════════════════

_header "ETAPA 4/6 — Validação de ingestão"

if [[ "$LOAD_OK" -eq 0 ]]; then
    _warn "Carga não gerada — pulando validação de ingestão"
else
    _step "Aguardando ${SCRAPE_WAIT}s para scrape do Prometheus..."
    sleep "$SCRAPE_WAIT"

    _step "Verificando incremento em /metrics (tracking-server direto)"
    METRICS_AFTER=$(curl -sf --max-time 5 "${TRACKING_URL}/metrics" 2>/dev/null) || METRICS_AFTER=""

    if [[ -n "$METRICS_AFTER" ]]; then
        AFTER_TOKENS=$(_metric_value "orbit_skill_tokens_saved_total" "$METRICS_AFTER")
        AFTER_ACTIVATIONS=$(echo "$METRICS_AFTER" | grep '^orbit_skill_activations_total{' | awk '{sum+=$2} END{print sum}')

        if [[ -n "$AFTER_TOKENS" && "${AFTER_TOKENS%.*}" -gt "${BASELINE_TOKENS%.*}" ]]; then
            _pass "orbit_skill_tokens_saved_total incrementou: ${BASELINE_TOKENS:-0} → ${AFTER_TOKENS}"
        else
            _fail "orbit_skill_tokens_saved_total NÃO incrementou (antes=${BASELINE_TOKENS:-0}, depois=${AFTER_TOKENS:-?})"
        fi

        if [[ -n "$AFTER_ACTIVATIONS" && "${AFTER_ACTIVATIONS%.*}" -gt "0" ]]; then
            _pass "orbit_skill_activations_total > 0 (total=${AFTER_ACTIVATIONS})"
        else
            _fail "orbit_skill_activations_total = ${AFTER_ACTIVATIONS:-0} (esperado > 0)"
        fi

        HB_VAL=$(_metric_value "orbit_heartbeat_total" "$METRICS_AFTER")
        if [[ -n "$HB_VAL" && "${HB_VAL%.*}" -ge "0" ]]; then
            _pass "orbit_heartbeat_total presente = ${HB_VAL}"
        else
            _fail "orbit_heartbeat_total AUSENTE ou inválido em /metrics"
        fi
    else
        _fail "Não foi possível ler /metrics após a carga"
    fi

    # Validar via Prometheus (se disponível)
    if [[ "$PROM_UP" -eq 1 ]]; then
        _step "Validando ingestão via Prometheus API"
        for query in "orbit_heartbeat_total" "orbit_tracking_up" "orbit_skill_activations_total"; do
            ENC=$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))" "$query")
            RESULT=$(curl -sf --max-time 5 "${PROM_URL}/api/v1/query?query=${ENC}" 2>/dev/null | \
                python3 -c "
import json,sys
try:
    d = json.load(sys.stdin)
    r = d.get('data',{}).get('result',[])
    if r:
        print('found:{}'.format(r[0]['value'][1]))
    else:
        print('no_data')
except:
    print('parse_error')
" 2>/dev/null || echo "curl_error")

            if [[ "$RESULT" == found:* ]]; then
                _pass "Prometheus: ${query} = ${RESULT#found:}"
            elif [[ "$RESULT" == "no_data" ]]; then
                _fail "Prometheus: ${query} → no_data (scrape não chegou ou métrica ausente)"
                _info "Verifique em ${PROM_URL}/graph?g0.expr=${query}"
            else
                _fail "Prometheus: ${query} → ${RESULT}"
            fi
        done
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 5 — Alertas e recording rules
# ════════════════════════════════════════════════════════════════════════════

_header "ETAPA 5/6 — Alertas e recording rules"

_step "Contando alertas em orbit_rules.yml"
RULES_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/orbit_rules.yml"
if [[ -f "$RULES_FILE" ]]; then
    ALERT_COUNT=$(grep -c "^      - alert:" "$RULES_FILE" || true)
    if [[ "$ALERT_COUNT" -ge 8 ]]; then
        _pass "${ALERT_COUNT} alertas configurados (mínimo obrigatório: 8)"
    else
        _fail "Apenas ${ALERT_COUNT} alertas — mínimo obrigatório é 8 (falta OrbitNoHeartbeat?)"
    fi

    _step "Verificando presença do alerta OrbitNoHeartbeat"
    if grep -q "alert: OrbitNoHeartbeat" "$RULES_FILE"; then
        _pass "Alerta OrbitNoHeartbeat presente"
    else
        _fail "Alerta OrbitNoHeartbeat AUSENTE em orbit_rules.yml"
    fi

    _step "Verificando presença da recording rule orbit:heartbeat_rate:prod"
    if grep -q "orbit:heartbeat_rate:prod" "$RULES_FILE"; then
        _pass "Recording rule orbit:heartbeat_rate:prod presente"
    else
        _fail "Recording rule orbit:heartbeat_rate:prod AUSENTE em orbit_rules.yml"
    fi
else
    _fail "orbit_rules.yml não encontrado em ${RULES_FILE}"
fi

_step "Validando orbit_rules.yml com promtool (se disponível)"
if command -v promtool >/dev/null 2>&1; then
    if promtool check rules "$RULES_FILE" >/dev/null 2>&1; then
        _pass "promtool check rules: OK"
    else
        _fail "promtool check rules: ERRO"
        promtool check rules "$RULES_FILE" 2>&1 | head -10 | while IFS= read -r line; do
            _info "$line"
        done
    fi
else
    _warn "promtool não encontrado — validação de sintaxe do YAML pulada"
    _info "Instale: https://prometheus.io/download/"
fi

if [[ "$PROM_UP" -eq 1 ]]; then
    _step "Verificando alerts FIRING no Prometheus"
    FIRING=$(curl -sf --max-time 5 "${PROM_URL}/api/v1/alerts" 2>/dev/null | python3 -c "
import json,sys
try:
    d = json.load(sys.stdin)
    firing = [a['labels']['alertname'] for a in d.get('data',{}).get('alerts',[]) if a.get('state')=='firing']
    print('\n'.join(firing) if firing else 'none')
except:
    print('parse_error')
" 2>/dev/null || echo "curl_error")

    if [[ "$FIRING" == "none" ]]; then
        _pass "Nenhum alerta FIRING no momento"
    elif [[ "$FIRING" == "curl_error" || "$FIRING" == "parse_error" ]]; then
        _warn "Não foi possível verificar alertas ativos"
    else
        while IFS= read -r alert; do
            [[ -z "$alert" ]] && continue
            _fail "Alerta FIRING: ${alert}"
        done <<< "$FIRING"
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# ETAPA 6/6 — Validação de segurança (HMAC, replay, burst)
# ════════════════════════════════════════════════════════════════════════════

_header "ETAPA 6/6 — Validação de segurança do /track"

if [[ "$METRICS_ACCESSIBLE" -eq 0 ]]; then
    _warn "tracking-server inacessível — pulando validação de segurança"
else
    # ── 6a. Replay detection (enviar mesmo payload 2x) ──
    _step "Testando proteção anti-replay (dedup)"
    REPLAY_TS=$(python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")
    REPLAY_PAYLOAD=$(printf '{"event_type":"activation","timestamp":"%s","session_id":"sec-replay-%s","mode":"auto","trigger":"security_check","estimated_waste":50,"actions_suggested":1,"actions_applied":1,"impact_estimated_tokens":100}' \
        "$REPLAY_TS" "$(date +%s)")

    REPLAY_RESP1=$(curl -sf -o /dev/null -w "%{http_code}" --max-time 5 -X POST "${TRACKING_URL}/track" \
        -H "Content-Type: application/json" -d "$REPLAY_PAYLOAD" 2>/dev/null) || REPLAY_RESP1="000"

    REPLAY_RESP2=$(curl -sf -o /dev/null -w "%{http_code}" --max-time 5 -X POST "${TRACKING_URL}/track" \
        -H "Content-Type: application/json" -d "$REPLAY_PAYLOAD" 2>/dev/null) || REPLAY_RESP2="000"

    if [[ "$REPLAY_RESP1" == "200" && "$REPLAY_RESP2" == "409" ]]; then
        _pass "Anti-replay: primeiro aceito (200), replay bloqueado (409)"
    elif [[ "$REPLAY_RESP1" == "200" && "$REPLAY_RESP2" == "200" ]]; then
        _fail "Anti-replay: replay NÃO foi bloqueado (ambos 200)"
    else
        _warn "Anti-replay: respostas inesperadas (1st=${REPLAY_RESP1}, 2nd=${REPLAY_RESP2})"
    fi

    # ── 6b. HMAC inválido (se HMAC estiver habilitado) ──
    _step "Testando rejeição de HMAC inválido"
    HMAC_TS=$(python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")
    HMAC_PAYLOAD=$(printf '{"event_type":"activation","timestamp":"%s","session_id":"sec-hmac-%s","mode":"auto","trigger":"security_check","estimated_waste":50,"actions_suggested":1,"actions_applied":1,"impact_estimated_tokens":100}' \
        "$HMAC_TS" "$(date +%s)")

    HMAC_RESP=$(curl -sf -o /dev/null -w "%{http_code}" --max-time 5 -X POST "${TRACKING_URL}/track" \
        -H "Content-Type: application/json" \
        -H "X-Orbit-Signature: deadbeefdeadbeefdeadbeefdeadbeef" \
        -d "$HMAC_PAYLOAD" 2>/dev/null) || HMAC_RESP="000"

    # Check if HMAC is enabled (check /metrics for hmac_failures)
    HMAC_ENABLED=$(echo "$METRICS_RAW" | grep -c "orbit_tracking_hmac_failures_total" || true)

    if [[ "$HMAC_ENABLED" -gt 0 ]]; then
        if [[ "$HMAC_RESP" == "401" ]]; then
            _pass "HMAC inválido rejeitado (401) — autenticação ativa"
        elif [[ "$HMAC_RESP" == "200" ]]; then
            _warn "HMAC inválido aceito (200) — HMAC pode não estar habilitado"
        else
            _info "HMAC: resposta inesperada ${HMAC_RESP}"
        fi
    else
        _warn "HMAC não está habilitado — defina ORBIT_HMAC_SECRET para habilitar"
    fi

    # ── 6c. Burst (enviar muitos requests rápidos) ──
    _step "Testando proteção de rate limit (token bucket burst)"
    BURST_OK=0
    BURST_LIMITED=0
    BURST_CLIENT="sec-burst-$(date +%s)"
    for i in $(seq 1 10); do
        B_TS=$(python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")
        B_PAYLOAD=$(printf '{"event_type":"activation","timestamp":"%s","session_id":"burst-sess-%d","mode":"auto","trigger":"security_check","estimated_waste":50,"actions_suggested":1,"actions_applied":1,"impact_estimated_tokens":100}' \
            "$B_TS" "$i")
        B_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" --max-time 5 -X POST "${TRACKING_URL}/track" \
            -H "Content-Type: application/json" \
            -H "X-Orbit-Client-Id: ${BURST_CLIENT}" \
            -d "$B_PAYLOAD" 2>/dev/null) || B_STATUS="000"
        if [[ "$B_STATUS" == "200" ]]; then
            ((BURST_OK++)) || true
        elif [[ "$B_STATUS" == "429" ]]; then
            ((BURST_LIMITED++)) || true
        fi
    done

    if [[ "$BURST_LIMITED" -gt 0 ]]; then
        _pass "Rate limit ativo: ${BURST_OK} aceitos, ${BURST_LIMITED} limitados (429)"
    elif [[ "$BURST_OK" -eq 10 ]]; then
        _warn "Rate limit pode não ter disparado (todos 10 aceitos) — bucket pode estar grande"
    else
        _warn "Burst: resultados mistos (ok=${BURST_OK}, limited=${BURST_LIMITED})"
    fi

    # ── 6d. Verificar métrica unificada de rejeição ──
    _step "Verificando métrica orbit_tracking_rejected_total"
    METRICS_SEC=$(curl -sf --max-time 5 "${TRACKING_URL}/metrics" 2>/dev/null) || METRICS_SEC=""
    if echo "$METRICS_SEC" | grep -q "orbit_tracking_rejected_total"; then
        _pass "orbit_tracking_rejected_total presente em /metrics"
        # Mostrar valores por reason
        echo "$METRICS_SEC" | grep "orbit_tracking_rejected_total{" | while IFS= read -r line; do
            _info "  $line"
        done
    else
        _warn "orbit_tracking_rejected_total ausente — pode não ter havido rejeições"
    fi
fi

# ════════════════════════════════════════════════════════════════════════════
# RESULTADO FINAL — GO / NO-GO
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}══════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  PASS:  ${PASS}"
echo -e "  FAIL:  ${FAIL}"
echo -e "  WARN:  ${WARN}"
echo ""

if [[ "$FAIL" -eq 0 ]]; then
    echo -e "${GREEN}${BOLD}  VEREDITO: GO${NC}"
    echo ""
    echo -e "  ${GREEN}Pipeline de observabilidade validado. Métricas expostas,${NC}"
    echo -e "  ${GREEN}coletadas e consultáveis.${NC}"
    echo ""
    exit 0
else
    echo -e "${RED}${BOLD}  VEREDITO: NO-GO${NC}"
    echo ""
    echo -e "  ${RED}${FAIL} verificação(ões) falharam.${NC}"
    echo ""
    echo -e "  Diagnóstico rápido:"
    echo -e "    1. Tracking server rodando?  curl ${TRACKING_URL}/health"
    echo -e "    2. Métricas expostas?         curl ${TRACKING_URL}/metrics | grep orbit_"
    echo -e "    3. Prometheus scraping?       curl ${PROM_URL}/api/v1/targets"
    echo -e "    4. Heartbeat presente?        curl ${TRACKING_URL}/metrics | grep orbit_heartbeat"
    echo -e "    5. Alertas disparando?        curl ${PROM_URL}/api/v1/alerts"
    echo ""
    exit 1
fi
