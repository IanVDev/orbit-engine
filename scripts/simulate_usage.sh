#!/usr/bin/env bash
# simulate_usage.sh — Injeta eventos reais no /track usando decisões do SkillRouter.
# Para cada turno do fixture, roda SkillRouter.evaluate(); só POSTa turnos ativados.
# Valida métricas críticas em /metrics e simula ataques para gerar rejected_total.
#
# Uso: ./scripts/simulate_usage.sh [--fixtures PATH]
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

TRACKING="http://127.0.0.1:9100"
GW="http://127.0.0.1:9091"
FIXTURES="${SCRIPT_DIR}/fixtures/activation_turns.jsonl"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --fixtures) FIXTURES="$2"; shift 2 ;;
    *) echo "uso: $0 [--fixtures PATH]" >&2; exit 2 ;;
  esac
done

RUN_SESSION="sim-$(date +%s)"
FAILURES=0

fail() { echo "  ❌ $1"; FAILURES=$((FAILURES+1)); }
pass() { echo "  ✅ $1"; }

echo ""
echo "══════════════════════════════════════════════"
echo "  orbit-engine — simulação via SkillRouter real"
echo "  run:      ${RUN_SESSION}"
echo "  fixtures: ${FIXTURES}"
echo "══════════════════════════════════════════════"
echo ""

if [[ ! -f "${FIXTURES}" ]]; then
  echo "  ❌ fixture não encontrado: ${FIXTURES}"; exit 1
fi

# ── Pré-condição: servidor vivo ──────────────────────────────────────
if ! curl -sf "${TRACKING}/health" >/dev/null 2>&1; then
  echo "  ❌ tracking-server não responde"; exit 1
fi
pass "tracking-server OK"
BEFORE=$(curl -s "${TRACKING}/metrics" | awk '/^orbit_skill_tokens_saved_total/{print $2}')
echo "  baseline tokens_saved = ${BEFORE:-0}"; echo ""

# ── Avaliar fixture via SkillRouter real (Python) ────────────────────
echo "── Avaliando fixture via SkillRouter.evaluate()..."
DECISIONS_FILE="$(mktemp)"
trap 'rm -f "${DECISIONS_FILE}"' EXIT

if ! python3 "${SCRIPT_DIR}/evaluate_turn.py" "${FIXTURES}" > "${DECISIONS_FILE}"; then
  echo "  ❌ SkillRouter falhou ao avaliar fixture"; exit 1
fi

TOTAL_TURNS=$(wc -l < "${DECISIONS_FILE}" | tr -d ' ')
ACTIVATED_TURNS=$(grep -c '"activated": true' "${DECISIONS_FILE}" || true)
SUPPRESSED_TURNS=$(grep -c '"suppressed": true' "${DECISIONS_FILE}" || true)
echo "  turnos avaliados:  ${TOTAL_TURNS}"
echo "  ativados:          ${ACTIVATED_TURNS}"
echo "  suprimidos:        ${SUPPRESSED_TURNS}"

if [[ "${TOTAL_TURNS}" -eq 0 ]]; then
  echo "  ❌ fail-closed: nenhum turno no fixture"; exit 1
fi
if [[ "${ACTIVATED_TURNS}" -eq 0 ]]; then
  echo "  ❌ fail-closed: nenhuma ativação real (router não discrimina — ainda laboratório)"; exit 1
fi

# ── Injetar SOMENTE decisões ativadas no /track ──────────────────────
echo ""; echo "── POSTando turnos ativados em /track..."
POSTED_OK=0
POSTED_FAIL=0
TOTAL_TOKENS=0
i=0
while IFS= read -r decision; do
  i=$((i+1))
  activated=$(printf '%s' "${decision}" | python3 -c "import json,sys; print(json.load(sys.stdin).get('activated', False))")
  if [[ "${activated}" != "True" ]]; then
    continue
  fi

  # Extrair campos da decisão. Fail-closed: qualquer parse error aborta este turno.
  fields=$(printf '%s' "${decision}" | python3 -c '
import json, sys
d = json.load(sys.stdin)
signals = d.get("signals") or []
reason = signals[0] if signals else "none"
# Texto cru nunca sai do helper; tokens estimados via text_len (heurística /4, min 1).
tokens = max(d.get("text_len", 0) // 4, 1)
print(d["session_id"])
print(d.get("phase", "exploration"))
print(reason)
print(tokens)
') || { fail "turno ${i}: parse falhou"; POSTED_FAIL=$((POSTED_FAIL+1)); continue; }

  SESSION_ID=$(echo "${fields}" | sed -n '1p')
  PHASE=$(echo    "${fields}" | sed -n '2p')
  REASON=$(echo   "${fields}" | sed -n '3p')
  TOKENS=$(echo   "${fields}" | sed -n '4p')
  WASTE=$((TOKENS * 2))
  TOTAL_TOKENS=$((TOTAL_TOKENS + TOKENS))
  TS=$(python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))")

  PAYLOAD=$(python3 -c '
import json, sys
print(json.dumps({
  "event_type": "activation",
  "timestamp": sys.argv[1],
  "session_id": sys.argv[2],
  "mode": "auto",
  "trigger": "simulate_usage_fixture",
  "estimated_waste": float(sys.argv[3]),
  "actions_suggested": 1,
  "actions_applied": 1,
  "impact_estimated_tokens": int(sys.argv[4]),
  "activation_reason": sys.argv[5],
  "activation_phase": sys.argv[6],
}))' "${TS}" "${SESSION_ID}" "${WASTE}" "${TOKENS}" "${REASON}" "${PHASE}")

  resp=$(curl -s -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d "${PAYLOAD}")
  status=$(echo "${resp}" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null || echo "erro")
  if [ "${status}" = "ok" ]; then
    pass "turno ${i} [${SESSION_ID}] reason=${REASON} phase=${PHASE} tokens=${TOKENS}"
    POSTED_OK=$((POSTED_OK+1))
  else
    fail "turno ${i} [${SESSION_ID}] resp=[${resp}]"
    POSTED_FAIL=$((POSTED_FAIL+1))
  fi
  sleep 0.1
done < "${DECISIONS_FILE}"

if [[ "${POSTED_OK}" -eq 0 ]]; then
  echo "  ❌ fail-closed: nenhum evento ativado chegou ao tracking pipeline"; exit 1
fi

# real_activation_rate: qualidade de discriminação do router no fixture.
REAL_ACTIVATION_RATE=$(python3 -c "print(f'{${ACTIVATED_TURNS}/${TOTAL_TURNS}:.3f}')")
echo ""
echo "── real_activation_rate (router side) ──"
echo "  ${ACTIVATED_TURNS}/${TOTAL_TURNS} = ${REAL_ACTIVATION_RATE}"
echo "  posted: ${POSTED_OK} ok / ${POSTED_FAIL} fail"

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
    "${TS}" "${RUN_SESSION}" "${i}")
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
echo "  real_activation_rate:   ${REAL_ACTIVATION_RATE} (${ACTIVATED_TURNS}/${TOTAL_TURNS})"
echo "  eventos no pipeline:    ${POSTED_OK} (tokens=${TOTAL_TOKENS})"
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
