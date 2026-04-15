#!/usr/bin/env bash
# simulate_usage.sh — Simula eventos de skill via POST /track (tracking-server :9100).
# Uso: ./scripts/simulate_usage.sh [--count N]
set -uo pipefail

TRACKING="http://127.0.0.1:9100"
GW="http://127.0.0.1:9091"
COUNT=3
if [[ "${1:-}" == "--count" && -n "${2:-}" ]]; then COUNT="$2"; fi

SESSION="sim-$(date +%s)"

echo ""
echo "══════════════════════════════════════════════"
echo "  orbit-engine — simulação de uso real"
echo "  session: ${SESSION}  eventos: ${COUNT}"
echo "══════════════════════════════════════════════"
echo ""

if ! curl -sf "${TRACKING}/health" >/dev/null 2>&1; then
  echo "  ❌ tracking-server não responde"; exit 1
fi
echo "  ✅ tracking-server OK"
BEFORE=$(curl -s "${TRACKING}/metrics" | awk '/^orbit_skill_tokens_saved_total/{print $2}')
echo "  baseline tokens_saved = ${BEFORE:-0}"; echo ""

echo "── Injetando ${COUNT} evento(s)..."
TOTAL=0
for i in $(seq 1 "${COUNT}"); do
  TOKENS=$((500 + (i-1)*100))
  WASTE=$((200 + (i-1)*50))
  TOTAL=$((TOTAL+TOKENS))
  TS=$(python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")

  PAYLOAD=$(printf '{"event_type":"activation","timestamp":"%s","session_id":"%s","mode":"auto","trigger":"simulate_usage_script","estimated_waste":%d,"actions_suggested":3,"actions_applied":2,"impact_estimated_tokens":%d}' \
    "${TS}" "${SESSION}" "${WASTE}" "${TOKENS}")

  resp=$(curl -s -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d "${PAYLOAD}")

  status=$(echo "${resp}" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null || echo "erro")
  if [ "${status}" = "ok" ]; then
    echo "  ✅ evento ${i} — waste=${WASTE} tokens=${TOKENS}"
  else
    echo "  ❌ evento ${i} — resp=[${resp}]  payload=[${PAYLOAD}]"
  fi
  sleep 0.1
done

echo ""; echo "── Aguardando scrape (10s)..."; sleep 10

echo ""; echo "── Métricas após injeção ──"
curl -s "${TRACKING}/metrics" | grep -E "^orbit_skill_(activations|tokens_saved|waste_estimated|sessions)" | sed 's/^/  /'

echo ""; echo "── Recording rules via gateway ──"
for q in "orbit:tokens_saved_total:prod" "orbit:activations_total:prod" "orbit:sessions_total:prod"; do
  enc=$(python3 -c "import urllib.parse, sys; print(urllib.parse.quote(sys.argv[1]))" "${q}")
  result=$(curl -s "${GW}/api/v1/query?query=${enc}" | \
    python3 -c "import json,sys; d=json.load(sys.stdin); r=d['data']['result']; print(r[0]['value'][1] if r else 'sem dados')" 2>/dev/null || echo "erro")
  echo "  ${q} = ${result}"
done

echo ""
echo "══════════════════════════════════════════════"
echo "  Total tokens simulados: ${TOTAL}"
echo "  Grafana: recarregue os painéis."
echo "══════════════════════════════════════════════"
echo ""
