#!/usr/bin/env bash
# simulate_usage.sh — Simula eventos de skill via POST /track (tracking-server :9100).
# Valida que TODAS métricas críticas aparecem no endpoint /metrics.
# Simula ataque para garantir orbit_tracking_rejected_total.
#
# Uso: ./scripts/simulate_usage.sh [--count N]
set -uo pipefail

TRACKING="http://127.0.0.1:9100"
GW="http://127.0.0.1:9091"
COUNT=3
if [[ "${1:-}" == "--count" && -n "${2:-}" ]]; then COUNT="$2"; fi

SESSION="sim-$(date +%s)"
FAILURES=0

fail() { echo "  ❌ $1"; FAILURES=$((FAILURES+1)); }
pass() { echo "  ✅ $1"; }

echo ""
echo "══════════════════════════════════════════════"
echo "  orbit-engine — simulação de uso real"
echo "  session: ${SESSION}  eventos: ${COUNT}"
echo "══════════════════════════════════════════════"
echo ""

# ── Pré-condição: servidor vivo ──────────────────────────────────────
if ! curl -sf "${TRACKING}/health" >/dev/null 2>&1; then
  echo "  ❌ tracking-server não responde"; exit 1
fi
pass "tracking-server OK"
BEFORE=$(curl -s "${TRACKING}/metrics" | awk '/^orbit_skill_tokens_saved_total/{print $2}')
echo "  baseline tokens_saved = ${BEFORE:-0}"; echo ""

# ── Injetar eventos normais ─────────────────────────────────────────
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
    pass "evento ${i} — waste=${WASTE} tokens=${TOKENS}"
  else
    fail "evento ${i} — resp=[${resp}]"
  fi
  sleep 0.1
done

# ── Simular ataque: payloads inválidos para gerar rejected_total ─────
echo ""; echo "── Simulando rejeições (ataque)..."

# 1) Body inválido (não-JSON)
resp=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
  -H "Content-Type: application/json" \
  -d "NOT-JSON")
if [ "${resp}" != "200" ]; then
  pass "rejeição body inválido (HTTP ${resp})"
else
  fail "body inválido deveria ser rejeitado, got 200"
fi

# 2) Payload sem campos obrigatórios
resp=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
  -H "Content-Type: application/json" \
  -d '{"event_type":""}')
if [ "${resp}" != "200" ]; then
  pass "rejeição campos vazios (HTTP ${resp})"
else
  fail "campos vazios deveria ser rejeitado, got 200"
fi

# 3) Rate limit burst (10 requests rápidos)
echo "  → burst de 10 requests para trigger rate limit..."
REJECTED=0
for i in $(seq 1 10); do
  TS=$(python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")
  PAYLOAD=$(printf '{"event_type":"activation","timestamp":"%s","session_id":"burst-%s-%d","mode":"auto","trigger":"burst","estimated_waste":1,"actions_suggested":1,"actions_applied":1,"impact_estimated_tokens":1}' \
    "${TS}" "${SESSION}" "${i}")
  code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d "${PAYLOAD}")
  if [ "${code}" = "429" ]; then
    REJECTED=$((REJECTED+1))
  fi
done
if [ "${REJECTED}" -gt 0 ]; then
  pass "rate limit ativo: ${REJECTED}/10 rejeitados"
else
  echo "  ⚠️  nenhum rate limit disparado (pode ser normal se bucket grande)"
fi

# ── Aguardar scrape ─────────────────────────────────────────────────
echo ""; echo "── Aguardando scrape (10s)..."; sleep 10

# ── Validar métricas críticas ────────────────────────────────────────
echo ""; echo "── Validando métricas críticas no /metrics ──"
METRICS=$(curl -s "${TRACKING}/metrics")

CRITICAL_METRICS=(
  "orbit_skill_sessions_total"
  "orbit_skill_sessions_with_activation_total"
  "orbit_skill_activations_total"
  "orbit_skill_tokens_saved_total"
  "orbit_skill_waste_estimated"
  "orbit_skill_tracking_failures_total"
  "orbit_skill_sessions_without_activation_total"
  "orbit_tracking_up"
  "orbit_seed_mode"
  "orbit_instance_id"
  "orbit_last_event_timestamp"
  "orbit_heartbeat_total"
  "orbit_real_usage_total"
  "orbit_real_usage_alive"
  "orbit_tracking_rejected_total"
  "orbit_tracking_token_bucket_rejected_total"
)

for m in "${CRITICAL_METRICS[@]}"; do
  if echo "${METRICS}" | grep -q "^${m}"; then
    pass "${m}"
  else
    fail "${m} NÃO ENCONTRADA"
  fi
done

# ── Validar rejected_total tem dados ────────────────────────────────
echo ""; echo "── Validando orbit_tracking_rejected_total > 0 ──"
REJECTED_VAL=$(echo "${METRICS}" | grep "^orbit_tracking_rejected_total" | head -1 | awk '{print $2}')
if [ -n "${REJECTED_VAL}" ] && [ "$(echo "${REJECTED_VAL} > 0" | bc 2>/dev/null || echo 0)" = "1" ]; then
  pass "orbit_tracking_rejected_total = ${REJECTED_VAL}"
else
  fail "orbit_tracking_rejected_total deveria ser > 0 após ataque simulado (got: ${REJECTED_VAL:-vazio})"
fi

# ── Métricas resumo ─────────────────────────────────────────────────
echo ""; echo "── Métricas após injeção ──"
echo "${METRICS}" | grep -E "^orbit_skill_(activations|tokens_saved|waste_estimated|sessions)" | sed 's/^/  /'

# ── Recording rules via gateway ──────────────────────────────────────
echo ""; echo "── Recording rules via gateway ──"
for q in "orbit:tokens_saved_total:prod" "orbit:activations_total:prod" "orbit:sessions_total:prod" "orbit:activation_rate" "orbit:tokens_per_session"; do
  enc=$(python3 -c "import urllib.parse, sys; print(urllib.parse.quote(sys.argv[1]))" "${q}")
  result=$(curl -s "${GW}/api/v1/query?query=${enc}" | \
    python3 -c "import json,sys; d=json.load(sys.stdin); r=d['data']['result']; print(r[0]['value'][1] if r else 'sem dados')" 2>/dev/null || echo "erro")
  echo "  ${q} = ${result}"
done

# ── Resultado final ──────────────────────────────────────────────────
echo ""
echo "══════════════════════════════════════════════"
echo "  Total tokens simulados: ${TOTAL}"
if [ "${FAILURES}" -gt 0 ]; then
  echo "  ❌ ${FAILURES} FALHA(S) DETECTADA(S)"
  echo "══════════════════════════════════════════════"
  exit 1
else
  echo "  ✅ TODAS métricas críticas validadas"
  echo "  Grafana: recarregue os painéis."
  echo "══════════════════════════════════════════════"
  exit 0
fi
