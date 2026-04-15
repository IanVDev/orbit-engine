#!/usr/bin/env bash
# mission_24h.sh — Validação operacional contínua do orbit-engine.
#
# Simula uso realista por 24h (ou --duration N minutos) com 6 cenários:
#   1. Sessões longas com muitos eventos
#   2. Sessões sem ativação (waste puro)
#   3. Sessões com ativação tardia
#   4. Sessões com alto waste
#   5. Sessões multi-modo (auto → suggest → off)
#   6. Sessões rápidas (1-2 eventos)
#
# Coleta checkpoints a cada ciclo e registra em mission_log.jsonl
#
# Uso:
#   ./scripts/mission_24h.sh                    # 24h completas
#   ./scripts/mission_24h.sh --duration 60      # 60 minutos
#   ./scripts/mission_24h.sh --duration 5       # 5 minutos (smoke)
#   ./scripts/mission_24h.sh --cycle-only 1     # 1 ciclo e sai (dry-run)
set -uo pipefail

TRACKING="http://127.0.0.1:9100"
GW="http://127.0.0.1:9091"
DURATION_MINUTES=$((24 * 60))
CYCLE_ONLY=0
LOG_FILE="mission_log.jsonl"
CYCLE_INTERVAL=60  # seconds between cycles

while [[ $# -gt 0 ]]; do
  case "$1" in
    --duration) DURATION_MINUTES="$2"; shift 2 ;;
    --cycle-only) CYCLE_ONLY="$2"; shift 2 ;;
    *) echo "Uso: $0 [--duration MINUTOS] [--cycle-only N]"; exit 1 ;;
  esac
done

DURATION_SECONDS=$((DURATION_MINUTES * 60))
START_EPOCH=$(date +%s)
END_EPOCH=$((START_EPOCH + DURATION_SECONDS))
CYCLE=0
TOTAL_EVENTS=0
TOTAL_ERRORS=0

# ── Helpers ──────────────────────────────────────────────────────────

ts_now() {
  python3 -c "import datetime; print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%S.%fZ'))"
}

post_event() {
  local payload="$1"
  resp=$(curl -s -w "\n%{http_code}" -X POST "${TRACKING}/track" \
    -H "Content-Type: application/json" \
    -d "${payload}" 2>&1)
  http_code=$(echo "$resp" | tail -1)
  body=$(echo "$resp" | head -n -1)
  if [ "$http_code" = "200" ]; then
    TOTAL_EVENTS=$((TOTAL_EVENTS + 1))
    return 0
  else
    TOTAL_ERRORS=$((TOTAL_ERRORS + 1))
    echo "  ⚠️  HTTP ${http_code}: ${body}" >&2
    return 1
  fi
}

checkpoint() {
  local label="$1"
  local ts
  ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local elapsed=$(( $(date +%s) - START_EPOCH ))

  # Query key metrics via gateway
  local sessions tokens staleness failures
  sessions=$(curl -s "${GW}/api/v1/query?query=orbit:sessions_total:prod" 2>/dev/null | \
    python3 -c "import json,sys; r=json.load(sys.stdin)['data']['result']; print(r[0]['value'][1] if r else '0')" 2>/dev/null || echo "ERR")
  tokens=$(curl -s "${GW}/api/v1/query?query=orbit:tokens_saved_total:prod" 2>/dev/null | \
    python3 -c "import json,sys; r=json.load(sys.stdin)['data']['result']; print(r[0]['value'][1] if r else '0')" 2>/dev/null || echo "ERR")
  staleness=$(curl -s "${GW}/api/v1/query?query=orbit:event_staleness_seconds:prod" 2>/dev/null | \
    python3 -c "import json,sys; r=json.load(sys.stdin)['data']['result']; print(r[0]['value'][1] if r else '0')" 2>/dev/null || echo "ERR")
  failures=$(curl -s "${GW}/api/v1/query?query=orbit:tracking_failures_total:prod" 2>/dev/null | \
    python3 -c "import json,sys; r=json.load(sys.stdin)['data']['result']; print(r[0]['value'][1] if r else '0')" 2>/dev/null || echo "ERR")

  local entry
  entry=$(python3 -c "
import json
print(json.dumps({
    'timestamp': '${ts}',
    'cycle': ${CYCLE},
    'label': '${label}',
    'elapsed_s': ${elapsed},
    'total_events': ${TOTAL_EVENTS},
    'total_errors': ${TOTAL_ERRORS},
    'sessions_prod': '${sessions}',
    'tokens_saved_prod': '${tokens}',
    'staleness_s': '${staleness}',
    'failures_prod': '${failures}'
}))
")
  echo "${entry}" >> "${LOG_FILE}"
  echo "  📊 [${label}] cycle=${CYCLE} events=${TOTAL_EVENTS} errors=${TOTAL_ERRORS} sessions=${sessions} tokens=${tokens} staleness=${staleness}s failures=${failures}"
}

# ── Cenário runners ──────────────────────────────────────────────────

# Cenário 1: Sessão longa (8-15 eventos, com ativação no meio)
scenario_long_session() {
  local sid="long-$(date +%s)-${RANDOM}"
  local n=$((8 + RANDOM % 8))
  local activate_at=$((n / 2))
  for i in $(seq 1 "$n"); do
    local ts
    ts=$(ts_now)
    local etype="suggestion"
    [ "$i" -eq "$activate_at" ] && etype="activation"
    local mode="auto"
    local waste=$((100 + RANDOM % 900))
    local tokens=$((200 + RANDOM % 800))
    post_event "{\"event_type\":\"${etype}\",\"timestamp\":\"${ts}\",\"session_id\":\"${sid}\",\"mode\":\"${mode}\",\"trigger\":\"mission_long\",\"estimated_waste\":${waste},\"actions_suggested\":3,\"actions_applied\":2,\"impact_estimated_tokens\":${tokens}}"
    sleep 0.05
  done
}

# Cenário 2: Sessão sem ativação (20+ eventos, nunca activa)
scenario_no_activation() {
  local sid="noact-$(date +%s)-${RANDOM}"
  local n=$((20 + RANDOM % 5))
  for i in $(seq 1 "$n"); do
    local ts
    ts=$(ts_now)
    local waste=$((50 + RANDOM % 200))
    local tokens=$((50 + RANDOM % 150))
    post_event "{\"event_type\":\"suggestion\",\"timestamp\":\"${ts}\",\"session_id\":\"${sid}\",\"mode\":\"auto\",\"trigger\":\"mission_noact\",\"estimated_waste\":${waste},\"actions_suggested\":2,\"actions_applied\":0,\"impact_estimated_tokens\":${tokens}}"
    sleep 0.03
  done
}

# Cenário 3: Ativação tardia (muitos eventos, ativação no penúltimo)
scenario_late_activation() {
  local sid="late-$(date +%s)-${RANDOM}"
  local n=$((12 + RANDOM % 6))
  for i in $(seq 1 "$n"); do
    local ts
    ts=$(ts_now)
    local etype="suggestion"
    [ "$i" -eq $((n - 1)) ] && etype="activation"
    local waste=$((300 + RANDOM % 700))
    local tokens=$((100 + RANDOM % 400))
    post_event "{\"event_type\":\"${etype}\",\"timestamp\":\"${ts}\",\"session_id\":\"${sid}\",\"mode\":\"suggest\",\"trigger\":\"mission_late\",\"estimated_waste\":${waste},\"actions_suggested\":4,\"actions_applied\":1,\"impact_estimated_tokens\":${tokens}}"
    sleep 0.04
  done
}

# Cenário 4: Alto waste (poucos eventos, waste desproporcional)
scenario_high_waste() {
  local sid="waste-$(date +%s)-${RANDOM}"
  for i in 1 2 3; do
    local ts
    ts=$(ts_now)
    local waste=$((5000 + RANDOM % 5000))
    post_event "{\"event_type\":\"activation\",\"timestamp\":\"${ts}\",\"session_id\":\"${sid}\",\"mode\":\"auto\",\"trigger\":\"mission_waste\",\"estimated_waste\":${waste},\"actions_suggested\":5,\"actions_applied\":1,\"impact_estimated_tokens\":$((waste / 10))}"
    sleep 0.05
  done
}

# Cenário 5: Multi-modo (mesma sessão alterna auto → suggest → off)
scenario_multi_mode() {
  local sid="multi-$(date +%s)-${RANDOM}"
  local modes=("auto" "auto" "suggest" "suggest" "off" "auto")
  for i in "${!modes[@]}"; do
    local ts
    ts=$(ts_now)
    local mode="${modes[$i]}"
    local etype="suggestion"
    [ "$i" -eq 3 ] && etype="activation"
    local tokens=$((200 + RANDOM % 300))
    post_event "{\"event_type\":\"${etype}\",\"timestamp\":\"${ts}\",\"session_id\":\"${sid}\",\"mode\":\"${mode}\",\"trigger\":\"mission_multi\",\"estimated_waste\":$((100 + RANDOM % 200)),\"actions_suggested\":2,\"actions_applied\":1,\"impact_estimated_tokens\":${tokens}}"
    sleep 0.05
  done
}

# Cenário 6: Sessão rápida (1-2 eventos, rápida saída)
scenario_quick() {
  local sid="quick-$(date +%s)-${RANDOM}"
  local ts
  ts=$(ts_now)
  post_event "{\"event_type\":\"activation\",\"timestamp\":\"${ts}\",\"session_id\":\"${sid}\",\"mode\":\"auto\",\"trigger\":\"mission_quick\",\"estimated_waste\":$((50 + RANDOM % 100)),\"actions_suggested\":1,\"actions_applied\":1,\"impact_estimated_tokens\":$((100 + RANDOM % 200))}"
}

# ── Pre-flight ───────────────────────────────────────────────────────

echo ""
echo "══════════════════════════════════════════════════════════════"
echo "  orbit-engine — MISSÃO DE VALIDAÇÃO 24h"
echo "  Duração: ${DURATION_MINUTES} minutos"
echo "  Intervalo entre ciclos: ${CYCLE_INTERVAL}s"
echo "  Log: ${LOG_FILE}"
echo "══════════════════════════════════════════════════════════════"
echo ""

# Health checks
for svc in "${TRACKING}/health:tracking" "${GW}/health:gateway"; do
  url="${svc%%:*}:${svc#*:}"
  # Extract URL and name
  svc_url="${svc%:*}"
  svc_name="${svc##*:}"
  if ! curl -sf "${svc_url}" >/dev/null 2>&1; then
    echo "  ❌ ${svc_name} não responde em ${svc_url}"
    exit 1
  fi
  echo "  ✅ ${svc_name} OK"
done

echo ""
echo "  Iniciando missão em $(date -u +"%Y-%m-%dT%H:%M:%SZ")..."
echo "  Ctrl+C para abortar a qualquer momento."
echo ""

# Truncate log
> "${LOG_FILE}"
checkpoint "START"

# ── Main loop ────────────────────────────────────────────────────────

while true; do
  NOW=$(date +%s)
  if [ "$CYCLE_ONLY" -gt 0 ] && [ "$CYCLE" -ge "$CYCLE_ONLY" ]; then
    break
  fi
  if [ "$CYCLE_ONLY" -eq 0 ] && [ "$NOW" -ge "$END_EPOCH" ]; then
    break
  fi

  CYCLE=$((CYCLE + 1))
  echo ""
  echo "── Ciclo ${CYCLE} ──────────────────────────────────────────"

  # Pick a weighted random mix of scenarios per cycle
  # Weighted: 30% long, 15% no-act, 15% late, 10% waste, 15% multi, 15% quick
  ROLL=$((RANDOM % 100))
  if   [ "$ROLL" -lt 30 ]; then
    echo "  🔵 Cenário: sessão longa"
    scenario_long_session
  elif [ "$ROLL" -lt 45 ]; then
    echo "  🔴 Cenário: sem ativação"
    scenario_no_activation
  elif [ "$ROLL" -lt 60 ]; then
    echo "  🟡 Cenário: ativação tardia"
    scenario_late_activation
  elif [ "$ROLL" -lt 70 ]; then
    echo "  🟠 Cenário: alto waste"
    scenario_high_waste
  elif [ "$ROLL" -lt 85 ]; then
    echo "  🟣 Cenário: multi-modo"
    scenario_multi_mode
  else
    echo "  ⚡ Cenário: sessão rápida"
    scenario_quick
  fi

  # Also run 1-2 quick sessions per cycle for baseline noise
  scenario_quick

  checkpoint "CYCLE_${CYCLE}"

  if [ "$CYCLE_ONLY" -eq 0 ]; then
    # Jitter: ±20% on cycle interval
    jitter=$(( (RANDOM % (CYCLE_INTERVAL / 5 + 1)) - CYCLE_INTERVAL / 10 ))
    sleep_time=$((CYCLE_INTERVAL + jitter))
    [ "$sleep_time" -lt 10 ] && sleep_time=10
    echo "  💤 Próximo ciclo em ${sleep_time}s..."
    sleep "$sleep_time"
  fi
done

# ── Final report ─────────────────────────────────────────────────────

echo ""
echo "══════════════════════════════════════════════════════════════"
echo "  MISSÃO COMPLETA"
echo ""
checkpoint "END"

ELAPSED=$(( $(date +%s) - START_EPOCH ))
ELAPSED_MIN=$((ELAPSED / 60))

echo ""
echo "  Duração real: ${ELAPSED_MIN} minutos"
echo "  Ciclos: ${CYCLE}"
echo "  Eventos enviados: ${TOTAL_EVENTS}"
echo "  Erros: ${TOTAL_ERRORS}"
echo ""

# Análise de drift
echo "── Análise de Estabilidade ──"
python3 -c "
import json, sys

entries = []
with open('${LOG_FILE}') as f:
    for line in f:
        entries.append(json.loads(line))

if len(entries) < 3:
    print('  ⚠️  Poucos checkpoints para análise de drift')
    sys.exit(0)

# Check for failures
failures = [e for e in entries if e.get('failures_prod','0') not in ('0','ERR')]
if failures:
    print(f'  🔴 FAILURES DETECTADAS: {len(failures)} checkpoints com falhas')
    for f in failures:
        print(f'     cycle={f[\"cycle\"]} failures={f[\"failures_prod\"]}')
else:
    print('  ✅ Zero failures durante toda a missão')

# Check staleness drift
staleness_vals = []
for e in entries:
    try:
        staleness_vals.append(float(e.get('staleness_s', '0')))
    except:
        pass

if staleness_vals:
    max_stale = max(staleness_vals)
    avg_stale = sum(staleness_vals) / len(staleness_vals)
    print(f'  📊 Staleness: avg={avg_stale:.1f}s max={max_stale:.1f}s')
    if max_stale > 600:
        print('  🔴 Staleness > 10min detectado — sistema pode ter ficado morto')
    elif max_stale > 120:
        print('  🟡 Staleness > 2min — investigar gaps')
    else:
        print('  ✅ Staleness sempre < 2min')

# Check errors
total_errs = entries[-1].get('total_errors', 0)
total_evts = entries[-1].get('total_events', 0)
if total_errs > 0:
    rate = (total_errs / max(total_evts, 1)) * 100
    print(f'  🟡 Taxa de erro: {total_errs}/{total_evts} ({rate:.1f}%)')
    if rate > 5:
        print('  🔴 Taxa de erro > 5% — inaceitável para v1.0')
    else:
        print('  ✅ Taxa de erro < 5%')
else:
    print('  ✅ Zero erros de tracking')

# Session growth
session_vals = []
for e in entries:
    try:
        session_vals.append(float(e.get('sessions_prod', '0')))
    except:
        pass
if len(session_vals) >= 2 and session_vals[-1] > session_vals[0]:
    growth = session_vals[-1] - session_vals[0]
    print(f'  ✅ Sessões cresceram: +{growth:.0f} durante a missão')
elif len(session_vals) >= 2:
    print('  🔴 Sessões NÃO cresceram — possível problema')

print()
print('  📋 Log completo: ${LOG_FILE}')
print('  📊 Verifique o Grafana para análise visual')
"

echo ""
echo "══════════════════════════════════════════════════════════════"
echo ""
