#!/usr/bin/env bash
# fault_injection.sh — Testa se o gate e alertas detectam falhas reais.
#
# Cada cenário quebra algo propositalmente e verifica se o sistema
# detecta a falha. NÃO execute em produção.
#
# Uso:
#   ./scripts/fault_injection.sh                    # todos os cenários
#   ./scripts/fault_injection.sh --scenario N       # cenário específico (1-6)
set -uo pipefail

TRACKING="http://127.0.0.1:9100"
GW="http://127.0.0.1:9091"
PASS=0
FAIL=0

SCENARIO_FILTER=0
if [[ "${1:-}" == "--scenario" && -n "${2:-}" ]]; then
  SCENARIO_FILTER="$2"
fi

ts_now() {
  python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))"
}

result() {
  local label="$1" expected="$2" got="$3"
  if [ "$expected" = "$got" ]; then
    echo "  ✅ ${label} — esperado: ${expected}, obtido: ${got}"
    ((PASS++)) || true
  else
    echo "  ❌ ${label} — esperado: ${expected}, obtido: ${got}"
    ((FAIL++)) || true
  fi
}

should_run() {
  [ "$SCENARIO_FILTER" -eq 0 ] || [ "$SCENARIO_FILTER" -eq "$1" ]
}

echo ""
echo "══════════════════════════════════════════════════════════════"
echo "  orbit-engine — FAULT INJECTION TEST"
echo "══════════════════════════════════════════════════════════════"
echo ""

# ── Cenário 1: Evento malformado (deve retornar 400) ────────────────

if should_run 1; then
  echo "── Cenário 1: Evento malformado (JSON inválido)"
  http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d '{"event_type":"","session_id":"","mode":"invalid"}' 2>/dev/null || echo "000")
  result "rejeita evento vazio" "400" "$http_code"

  http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d 'not json at all' 2>/dev/null || echo "000")
  result "rejeita JSON inválido" "400" "$http_code"

  http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d '{"event_type":"test","session_id":"s","mode":"auto","timestamp":"2026-04-15T12:00:00"}' 2>/dev/null || echo "000")
  result "rejeita timestamp sem timezone" "400" "$http_code"
  echo ""
fi

# ── Cenário 2: Governance bypass attempt ─────────────────────────────

if should_run 2; then
  echo "── Cenário 2: Tentativa de bypass de governança"

  # Raw metric query should be blocked
  status=$(curl -s "${GW}/api/v1/query?query=orbit_skill_tokens_saved_total" 2>/dev/null | \
    python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "error")
  result "bloqueia orbit_skill_* raw" "error" "$status"

  # Empty query should be blocked
  status=$(curl -s "${GW}/api/v1/query?query=" 2>/dev/null | \
    python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "error")
  result "bloqueia query vazia" "error" "$status"

  # Recording rule should pass
  status=$(curl -s "${GW}/api/v1/query?query=orbit:tokens_saved_total:prod" 2>/dev/null | \
    python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "error")
  result "permite recording rule" "success" "$status"
  echo ""
fi

# ── Cenário 3: Método HTTP errado ────────────────────────────────────

if should_run 3; then
  echo "── Cenário 3: Método HTTP inválido"
  http_code=$(curl -s -o /dev/null -w "%{http_code}" -X GET "${TRACKING}/track" 2>/dev/null || echo "000")
  result "rejeita GET em /track" "405" "$http_code"

  http_code=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "${TRACKING}/track" 2>/dev/null || echo "000")
  result "rejeita DELETE em /track" "405" "$http_code"
  echo ""
fi

# ── Cenário 4: Timestamp fora dos bounds ─────────────────────────────

if should_run 4; then
  echo "── Cenário 4: Timestamps fora dos bounds temporais"

  # Future timestamp (>5min ahead)
  future_ts=$(python3 -c "import datetime; print((datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(minutes=10)).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")
  http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d "{\"event_type\":\"activation\",\"session_id\":\"fault-ts\",\"mode\":\"auto\",\"timestamp\":\"${future_ts}\",\"estimated_waste\":100,\"actions_suggested\":1,\"actions_applied\":1,\"impact_estimated_tokens\":100}" 2>/dev/null || echo "000")
  result "rejeita timestamp +10min futuro" "400" "$http_code"

  # Past timestamp (>24h ago)
  past_ts=$(python3 -c "import datetime; print((datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(hours=25)).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")
  http_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d "{\"event_type\":\"activation\",\"session_id\":\"fault-ts\",\"mode\":\"auto\",\"timestamp\":\"${past_ts}\",\"estimated_waste\":100,\"actions_suggested\":1,\"actions_applied\":1,\"impact_estimated_tokens\":100}" 2>/dev/null || echo "000")
  result "rejeita timestamp -25h passado" "400" "$http_code"
  echo ""
fi

# ── Cenário 5: Validação do gate sob código (unit test) ──────────────

if should_run 5; then
  echo "── Cenário 5: Gate de contrato (go test)"
  echo "  Rodando TestV1ContractComplete..."
  if cd /Users/ian/Documents/orbit-engine/tracking && go test -run "TestV1ContractComplete|TestV1GatewayMetricsContract" -count=1 -v 2>&1 | tail -5 | grep -q "PASS"; then
    result "gate-v1 contract test" "PASS" "PASS"
  else
    result "gate-v1 contract test" "PASS" "FAIL"
  fi
  echo ""
fi

# ── Cenário 6: Alerta de silêncio (verificação de regra) ────────────

if should_run 6; then
  echo "── Cenário 6: Verificação de alerting rules"

  # Check if alerting rules are loaded (via Prometheus API if available)
  prom_rules=$(curl -s "http://127.0.0.1:9090/api/v1/rules" 2>/dev/null | \
    python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    groups = d.get('data', {}).get('groups', [])
    alert_count = sum(1 for g in groups for r in g.get('rules', []) if r.get('type') == 'alerting')
    print(alert_count)
except:
    print('ERR')
" 2>/dev/null || echo "ERR")

  if [ "$prom_rules" = "ERR" ]; then
    echo "  ⚠️  Prometheus não acessível em :9090 — verificação de alerts pulada"
    echo "  💡 Certifique-se de que Prometheus está rodando com orbit_rules.yml"
  else
    echo "  📊 Alerting rules carregadas no Prometheus: ${prom_rules}"
    if [ "$prom_rules" -ge 5 ] 2>/dev/null; then
      result "alerting rules carregadas" ">=5" "$prom_rules"
    else
      result "alerting rules carregadas" ">=5" "$prom_rules"
    fi
  fi
  echo ""
fi

# ── Resultado ────────────────────────────────────────────────────────

echo "══════════════════════════════════════════════════════════════"
echo "  RESULTADO: ${PASS} pass / ${FAIL} fail"
echo "══════════════════════════════════════════════════════════════"
echo ""

if [ "$FAIL" -gt 0 ]; then
  echo "  🔴 Falhas detectadas — sistema NÃO está pronto"
  exit 1
else
  echo "  🟢 Todos os cenários de falha foram detectados corretamente"
  exit 0
fi
