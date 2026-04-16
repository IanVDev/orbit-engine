#!/usr/bin/env bash
# simulate_usage.sh — Simulador de produto real para orbit-engine.
#
# Modela sessões reais (start → interações → decisão → end) e calcula:
#   activation_rate  = activations / sessions
#   value_rate       = value_sessions / sessions
#   cost_per_value   = total_tokens / value_sessions
#
# Uso:
#   ./scripts/simulate_usage.sh [--count N] [--sessions S] [--prob P]
#
#   --count    N  eventos por sessão (default: 3)
#   --sessions S  número de sessões  (default: 5)
#   --prob     P  probabilidade de activação 0.0–1.0 (default: 0.6)
#
# Fail-closed:
#   - Aborta se sessions == 0
#   - Aborta se activation_rate == 0 após todas as sessões
#
# Requer: curl, python3
# ---------------------------------------------------------------------------
set -uo pipefail

TRACKING="http://127.0.0.1:9100"
GW="http://127.0.0.1:9091"

COUNT=3
SESSIONS=5
PROB=0.6

while [[ $# -gt 0 ]]; do
  case "$1" in
    --count)    COUNT="$2";    shift 2 ;;
    --sessions) SESSIONS="$2"; shift 2 ;;
    --prob)     PROB="$2";     shift 2 ;;
    *) echo "uso: $0 [--count N] [--sessions S] [--prob P]"; exit 1 ;;
  esac
done

if [[ "${SESSIONS}" -le 0 ]]; then
  echo "❌ ABORT: --sessions deve ser > 0 (got ${SESSIONS})" >&2
  exit 1
fi

FAILURES=0
GLOBAL_SESSION="sim-$(date +%s)"

fail()  { echo "  ❌ $1"; FAILURES=$((FAILURES+1)); }
pass()  { echo "  ✅ $1"; }
warn()  { echo "  ⚠️  $1"; }

now_iso() {
  python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'))"
}

sim_log() {
  local event="$1" session="$2" detail="${3:-}"
  python3 -c "
import json, sys
print(json.dumps({'event': sys.argv[1], 'session_id': sys.argv[2], 'detail': sys.argv[3]}), file=sys.stderr)
" "${event}" "${session}" "${detail}" || true
}

random_float() {
  python3 -c "import random; print(f'{random.random():.4f}')"
}

float_lt() {
  python3 -c "import sys; sys.exit(0 if float(sys.argv[1]) < float(sys.argv[2]) else 1)" "$1" "$2"
}

echo ""
echo "══════════════════════════════════════════════════════════"
echo "  orbit-engine — simulador de produto real"
echo "  sessões: ${SESSIONS}  interações/sessão: ${COUNT}  prob: ${PROB}"
echo "══════════════════════════════════════════════════════════"
echo ""

if ! curl -sf "${TRACKING}/health" >/dev/null 2>&1; then
  echo "  ❌ tracking-server não responde em ${TRACKING}" >&2
  exit 1
fi
pass "tracking-server OK"
echo ""

BEFORE_TOKENS=$(curl -s "${TRACKING}/metrics" | awk '/^orbit_skill_tokens_saved_total/{print $2}')
echo "  baseline tokens_saved = ${BEFORE_TOKENS:-0}"
echo ""

echo "── Iniciando ${SESSIONS} sessão(ões)..."
echo ""

TOTAL_ACTIVATIONS=0
TOTAL_TOKENS=0
VALUE_SESSIONS=0

for s in $(seq 1 "${SESSIONS}"); do
  SESSION_ID="${GLOBAL_SESSION}-s${s}"
  SESSION_ACTIVATIONS=0
  SESSION_TOKENS=0

  sim_log "session_start" "${SESSION_ID}" "interactions=${COUNT}"
  echo "  ┌ Sessão ${s}/${SESSIONS}  [${SESSION_ID}]"

  for i in $(seq 1 "${COUNT}"); do
    TOKENS=$(( 500 + (s * 37 + i * 13) % 300 ))
    SESSION_TOKENS=$(( SESSION_TOKENS + TOKENS ))

    R=$(random_float)
    if float_lt "${R}" "${PROB}"; then
      SESSION_ACTIVATIONS=$(( SESSION_ACTIVATIONS + 1 ))
      ACTIONS_APPLIED=2
      sim_log "interaction_activated" "${SESSION_ID}" "tokens=${TOKENS}"
    else
      ACTIONS_APPLIED=0
      sim_log "interaction_ignored" "${SESSION_ID}" "tokens=${TOKENS}"
    fi

    TS=$(now_iso)
    PAYLOAD=$(printf \
      '{"event_type":"activation","timestamp":"%s","session_id":"%s","mode":"auto","trigger":"simulate_product","estimated_waste":0.3,"actions_suggested":3,"actions_applied":%d,"impact_estimated_tokens":%d}' \
      "${TS}" "${SESSION_ID}" "${ACTIONS_APPLIED}" "${TOKENS}")

    RESP=$(curl -s -X POST "${TRACKING}/track" \
      -H "Content-Type: application/json" \
      -d "${PAYLOAD}" 2>/dev/null || echo '{"status":"curl_error"}')

    STATUS=$(echo "${RESP}" | python3 -c \
      "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null || echo "parse_error")

    if [[ "${STATUS}" == "ok" ]]; then
      echo "  │  evento ${i}: tokens=${TOKENS} applied=${ACTIONS_APPLIED} ✓"
    else
      echo "  │  evento ${i}: tokens=${TOKENS} applied=${ACTIONS_APPLIED} ✗ [${RESP}]"
      FAILURES=$(( FAILURES + 1 ))
    fi

    sleep 0.05
  done

  if [[ "${SESSION_ACTIVATIONS}" -ge "${COUNT}" ]]; then
    VALUE_LEVEL="high"
    VALUE_SESSIONS=$(( VALUE_SESSIONS + 1 ))
  elif [[ "${SESSION_ACTIVATIONS}" -gt 0 ]]; then
    VALUE_LEVEL="medium"
    VALUE_SESSIONS=$(( VALUE_SESSIONS + 1 ))
  else
    VALUE_LEVEL="none"
  fi

  TOTAL_ACTIVATIONS=$(( TOTAL_ACTIVATIONS + SESSION_ACTIVATIONS ))
  TOTAL_TOKENS=$(( TOTAL_TOKENS + SESSION_TOKENS ))

  sim_log "session_end" "${SESSION_ID}" \
    "activations=${SESSION_ACTIVATIONS}/${COUNT} value=${VALUE_LEVEL} tokens=${SESSION_TOKENS}"

  echo "  └ fim: activações=${SESSION_ACTIVATIONS}/${COUNT}  valor=${VALUE_LEVEL}  tokens=${SESSION_TOKENS}"
  echo ""
done

ACTIVATION_RATE=$(python3 -c "print(f'{${TOTAL_ACTIVATIONS} / (${SESSIONS} * ${COUNT}):.3f}')")
VALUE_RATE=$(python3 -c "print(f'{${VALUE_SESSIONS} / ${SESSIONS}:.3f}')")

if [[ "${VALUE_SESSIONS}" -gt 0 ]]; then
  COST_PER_VALUE=$(python3 -c "print(f'{${TOTAL_TOKENS} / ${VALUE_SESSIONS}:.1f}')")
else
  COST_PER_VALUE="N/A"
fi

echo "── KPIs da simulação ──────────────────────────────────────"
echo "  activation_rate  = ${ACTIVATION_RATE}"
echo "  value_rate       = ${VALUE_RATE}"
echo "  cost_per_value   = ${COST_PER_VALUE} tokens"
echo "  total_tokens     = ${TOTAL_TOKENS}"
echo ""

if [[ "${TOTAL_ACTIVATIONS}" -eq 0 ]]; then
  echo "❌ ABORT: activation_rate == 0 após ${SESSIONS} sessões." >&2
  echo "   Aumente --prob (atual: ${PROB}) ou verifique o servidor." >&2
  exit 1
fi

echo "── Simulando rejeições..."

RESP=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
  -H "Content-Type: application/json" -d "NOT-JSON" 2>/dev/null || echo "000")
if [[ "${RESP}" != "200" ]]; then
  pass "rejeição body inválido (HTTP ${RESP})"
else
  fail "body inválido deveria ser rejeitado (got 200)"
fi

RESP=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
  -H "Content-Type: application/json" -d '{"event_type":""}' 2>/dev/null || echo "000")
if [[ "${RESP}" != "200" ]]; then
  pass "rejeição campos vazios (HTTP ${RESP})"
else
  fail "campos vazios deveria ser rejeitado (got 200)"
fi

echo ""
echo "── Aguardando scrape (10s)..."
sleep 10

echo ""
echo "── Validando métricas críticas..."
METRICS=$(curl -s "${TRACKING}/metrics" 2>/dev/null || true)

CRITICAL_METRICS=(
  "orbit_skill_sessions_total"
  "orbit_skill_activations_total"
  "orbit_skill_tokens_saved_total"
  "orbit_skill_waste_estimated"
  "orbit_tracking_up"
  "orbit_last_event_timestamp"
  "orbit_heartbeat_total"
  "orbit_tracking_rejected_total"
)

for m in "${CRITICAL_METRICS[@]}"; do
  if echo "${METRICS}" | grep -q "^${m}"; then
    pass "${m}"
  else
    fail "${m} NÃO ENCONTRADA"
  fi
done

echo ""
REJECTED_VAL=$(echo "${METRICS}" | awk '/^orbit_tracking_rejected_total/{print $2}' | head -1)
if [[ -n "${REJECTED_VAL}" ]] && python3 -c "import sys; sys.exit(0 if float('${REJECTED_VAL}') > 0 else 1)" 2>/dev/null; then
  pass "orbit_tracking_rejected_total = ${REJECTED_VAL}"
else
  warn "orbit_tracking_rejected_total = ${REJECTED_VAL:-vazio}"
fi

echo ""
echo "── Recording rules via gateway..."
for q in "orbit:activation_rate" "orbit:tokens_per_session" "orbit:sessions_total:prod"; do
  ENC=$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))" "${q}")
  RESULT=$(curl -s "${GW}/api/v1/query?query=${ENC}" 2>/dev/null | \
    python3 -c "
import json,sys
d=json.load(sys.stdin)
r=d.get('data',{}).get('result',[])
print(r[0]['value'][1] if r else 'sem dados')
" 2>/dev/null || echo "erro")
  echo "  ${q} = ${RESULT}"
done

echo ""
echo "══════════════════════════════════════════════════════════"
echo "  Sessões simuladas : ${SESSIONS}"
echo "  Total tokens      : ${TOTAL_TOKENS}"
echo "  activation_rate   : ${ACTIVATION_RATE}"
echo "  value_rate        : ${VALUE_RATE}"
echo "  cost_per_value    : ${COST_PER_VALUE}"

if [[ "${FAILURES}" -gt 0 ]]; then
  echo "  ❌ ${FAILURES} FALHA(S) DETECTADA(S)"
  echo "══════════════════════════════════════════════════════════"
  exit 1
else
  echo "  ✅ Simulação concluída com sucesso"
  echo "══════════════════════════════════════════════════════════"
  exit 0
fi
