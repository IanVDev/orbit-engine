#!/usr/bin/env bash
# validate_dashboard_queries.sh — Valida todas as queries do orbit-engine
# contra o gateway PromQL. Sem dependência do Grafana.
set -uo pipefail

GW="http://127.0.0.1:9091"
PASS=0
FAIL=0
WARN=0
NOW=$(date +%s)
START=$((NOW - 300))

# ── Helpers ──────────────────────────────────────────────────────────────

check_instant() {
  local label="$1" query="$2"
  resp=$(curl -s "${GW}/api/v1/query?query=$(python3 -c "import urllib.parse; print(urllib.parse.quote('''${query}'''))")" 2>&1) || resp=""

  if [ -z "$resp" ]; then
    echo "  ❌ ${label} — sem resposta (gateway down?)"
    ((FAIL++)) || true; return
  fi

  status=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
  if [ "$status" != "success" ]; then
    err=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('error','?')[:80])" 2>/dev/null)
    echo "  ❌ ${label} — ${err}"
    ((FAIL++)) || true; return
  fi

  count=$(echo "$resp" | python3 -c "import json,sys; print(len(json.load(sys.stdin)['data']['result']))" 2>/dev/null)
  if [ "$count" = "0" ]; then
    echo "  ⚠️  ${label} — sem dados (esperado se não houve uso)"
    ((WARN++)) || true; return
  fi

  echo "  ✅ ${label} — ${count} série(s)"
  ((PASS++)) || true
}

check_range() {
  local label="$1" query="$2"
  encoded=$(python3 -c "import urllib.parse; print(urllib.parse.quote('''${query}'''))")
  resp=$(curl -s "${GW}/api/v1/query_range?query=${encoded}&start=${START}&end=${NOW}&step=15" 2>&1) || resp=""

  if [ -z "$resp" ]; then
    echo "  ❌ ${label} — sem resposta (gateway down?)"
    ((FAIL++)) || true; return
  fi

  status=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null)
  if [ "$status" != "success" ]; then
    err=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('error','?')[:80])" 2>/dev/null)
    echo "  ❌ ${label} — ${err}"
    ((FAIL++)) || true; return
  fi

  count=$(echo "$resp" | python3 -c "import json,sys; print(len(json.load(sys.stdin)['data']['result']))" 2>/dev/null)
  if [ "$count" = "0" ]; then
    echo "  ⚠️  ${label} — sem dados (esperado se não houve uso)"
    ((WARN++)) || true; return
  fi

  echo "  ✅ ${label} — ${count} série(s)"
  ((PASS++)) || true
}

# ── Queries Instant ──────────────────────────────────────────────────────

echo ""
echo "══════════════════════════════════════════════"
echo "  orbit-engine — validação de queries"
echo "  gateway: ${GW}"
echo "══════════════════════════════════════════════"
echo ""
echo "── Instant ──"

check_instant "tracking_up"           "orbit_tracking_up"
check_instant "seed_mode"             "orbit_seed_mode"
check_instant "instance_id"           "orbit_instance_id"
check_instant "tokens_saved"          "orbit:tokens_saved_total:prod"
check_instant "activations"           "orbit:activations_total:prod"
check_instant "staleness"             "orbit:event_staleness_seconds:prod"
check_instant "sessions_total"        "orbit:sessions_total:prod"
check_instant "sessions_w_activation" "orbit:sessions_with_activation:prod"
check_instant "sessions_no_activation" "orbit:sessions_without_activation:prod"
check_instant "tracking_failures"     "orbit:tracking_failures_total:prod"
check_instant "seed_contamination"    "orbit:seed_contamination"

# ── Queries Range ────────────────────────────────────────────────────────

echo ""
echo "── Range (últimos 5 min) ──"

check_range "requests rate"    "rate(orbit_gateway_requests_total[5m])"
check_range "blocked rate"     "rate(orbit_gateway_blocked_total[5m])"
check_range "latência p95"     "histogram_quantile(0.95, rate(orbit_gateway_latency_ms_bucket[5m]))"
check_range "activation rate"  "100 * orbit:sessions_with_activation:prod / (orbit:sessions_total:prod > 0)"

# ── Governança (deve ser bloqueada) ──────────────────────────────────────

echo ""
echo "── Governança (devem ser BLOQUEADAS) ──"

for q in "orbit_skill_tokens_saved_total" "orbit_skill_activations_total" ""; do
  label="${q:-<vazia>}"
  resp=$(curl -s "${GW}/api/v1/query?query=$(python3 -c "import urllib.parse; print(urllib.parse.quote('''${q}'''))")" 2>&1) || resp=""
  status=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
  if [ "$status" = "error" ]; then
    echo "  ✅ ${label} — bloqueada corretamente"
    ((PASS++)) || true
  else
    echo "  ❌ ${label} — deveria ser bloqueada mas passou!"
    ((FAIL++)) || true
  fi
done

# ── Resultado ────────────────────────────────────────────────────────────

echo ""
echo "══════════════════════════════════════════════"
echo "  RESULTADO: ${PASS} ok / ${WARN} warnings / ${FAIL} falhas"
echo "══════════════════════════════════════════════"
echo ""

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
