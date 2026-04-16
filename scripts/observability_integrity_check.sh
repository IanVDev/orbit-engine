#!/usr/bin/env bash
# observability_integrity_check.sh — Integrity gate for orbit-engine observability.
#
# Validates presence, freshness, and value health of critical metrics and
# recording rules in Prometheus before trusting any observability data.
#
# Fail modes:
#   default  — only [✗] critical checks affect exit code
#   --strict — [~] warnings also affect exit code
#
# Output legend:
#   [✓]  pass
#   [✗]  critical failure  (always fatal)
#   [~]  warning           (fatal only in --strict)
#   [!]  expected gap      (informational, never fatal)
#
# Requires: curl, jq
#
# Usage:
#   ./scripts/observability_integrity_check.sh [--strict]
#
# Env vars:
#   PROM_HOST        host:port of Prometheus        (default: 127.0.0.1:9090)
#   TRACKING_HOST    host:port of tracking server   (default: 127.0.0.1:9100)
#   STALE_THRESHOLD  max sample age in seconds      (default: 300)

set -uo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
PROM_HOST="${PROM_HOST:-127.0.0.1:9090}"
TRACKING_HOST="${TRACKING_HOST:-127.0.0.1:9100}"
STALE_THRESHOLD="${STALE_THRESHOLD:-300}"
STRICT=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --strict) STRICT=1; shift ;;
        *) echo "Unknown argument: $1  (usage: $0 [--strict])"; exit 1 ;;
    esac
done

PROM_URL="http://${PROM_HOST}"
TRACKING_URL="http://${TRACKING_HOST}"

# ── Colors ────────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

# ── Counters ──────────────────────────────────────────────────────────────────
PASS=0
CRIT_FAIL=0
WARN=0
EXPECTED=0

# ── Output helpers ────────────────────────────────────────────────────────────
_ok()       { echo -e "  ${GREEN}[✓]${NC} $1"; ((PASS++))     || true; }
_critical() { echo -e "  ${RED}[✗]${NC} $1"; ((CRIT_FAIL++)) || true; }
_warning()  { echo -e "  ${YELLOW}[~]${NC} $1"; ((WARN++))    || true; }
_expected() { echo -e "  ${YELLOW}[!]${NC} $1"; ((EXPECTED++)) || true; }

# Route failure to the right severity handler.
_emit_fail() {
    local severity="$1" msg="$2"
    case "$severity" in
        critical) _critical "$msg" ;;
        warn)     _warning  "$msg" ;;
        expected) _expected "$msg (expected gap)" ;;
    esac
}

# ── Prometheus query helper ───────────────────────────────────────────────────
# Executes an instant query; returns raw JSON or empty string on failure.
_prom_query() {
    curl -sf --max-time 5 \
        --data-urlencode "query=$1" \
        "${PROM_URL}/api/v1/query" 2>/dev/null || echo ""
}

# ── Check: metric has at least one series in Prometheus ──────────────────────
# Returns 0 if found, 1 if missing. Callers use this to guard freshness/value.
_check_metric() {
    local metric="$1" severity="${2:-critical}"
    local count
    count=$(_prom_query "$metric" \
        | jq -r '.data.result | length' 2>/dev/null || echo "0")
    if [[ "${count}" -gt 0 ]]; then
        _ok "${metric} present (${count} series)"
        return 0
    else
        _emit_fail "$severity" "${metric} — no series in Prometheus"
        return 1
    fi
}

# ── Check: most recent sample is not stale ───────────────────────────────────
# Uses timestamp() PromQL function — reflects actual scrape recency, not just
# whether the value changed. max() aggregates across all label combinations.
_check_freshness() {
    local metric="$1" severity="${2:-critical}"
    local now ts_raw ts delta
    now=$(date +%s)

    ts_raw=$(_prom_query "max(timestamp(${metric}))" \
        | jq -r '.data.result[0].value[1] // empty' 2>/dev/null || echo "")

    if [[ -z "$ts_raw" ]]; then
        _emit_fail "$severity" "${metric} freshness — no timestamp (never scraped?)"
        return 1
    fi

    # Strip fractional seconds for integer arithmetic
    ts="${ts_raw%.*}"
    delta=$(( now - ts ))

    if [[ "$delta" -gt "$STALE_THRESHOLD" ]]; then
        _emit_fail "$severity" \
            "${metric} stale — last sample ${delta}s ago (threshold: ${STALE_THRESHOLD}s)"
        return 1
    fi
    _ok "${metric} fresh — last sample ${delta}s ago"
    return 0
}

# ── Check: metric value is not NaN / ±Inf / unexpected zero ──────────────────
# severity applies to NaN/Inf. allow_zero=false also fails on 0.
# Inspects first series only — sufficient for NaN detection on all metric types.
_check_value() {
    local metric="$1" severity="${2:-warn}" allow_zero="${3:-true}"
    local val
    val=$(_prom_query "$metric" \
        | jq -r '.data.result[0].value[1] // "no_data"' 2>/dev/null || echo "no_data")

    case "$val" in
        no_data)
            _emit_fail "$severity" "${metric} value — no data returned"
            return 1 ;;
        NaN|+Inf|-Inf)
            _emit_fail "$severity" "${metric} value is ${val} — computation broken"
            return 1 ;;
        0|0.0|0.00)
            if [[ "$allow_zero" == "false" ]]; then
                _emit_fail "$severity" "${metric} = 0 (expected non-zero)"
                return 1
            fi
            _ok "${metric} value = 0 (zero is acceptable)"
            return 0 ;;
        *)
            _ok "${metric} value = ${val}"
            return 0 ;;
    esac
}

# ── Check: recording rule is loaded and evaluating ───────────────────────────
# Presence verified via /api/v1/rules; freshness via timestamp() PromQL.
_check_rule() {
    local rule_name="$1" severity="${2:-critical}"
    local count
    count=$(curl -sf --max-time 5 "${PROM_URL}/api/v1/rules" 2>/dev/null \
        | jq -r --arg n "$rule_name" \
            '[.data.groups[].rules[] | select(.type == "recording" and .name == $n)] | length' \
            2>/dev/null || echo "0")
    if [[ "${count}" -gt 0 ]]; then
        _ok "rule ${rule_name} loaded"
        return 0
    else
        _emit_fail "$severity" "rule ${rule_name} — not loaded in Prometheus"
        return 1
    fi
}

# ── Dependency check ──────────────────────────────────────────────────────────
for _cmd in curl jq; do
    if ! command -v "$_cmd" >/dev/null 2>&1; then
        echo "FATAL: ${_cmd} not found. Install ${_cmd} and retry."
        exit 1
    fi
done

# ── Header ────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}orbit-engine — OBSERVABILITY INTEGRITY CHECK${NC}"
echo "  prometheus:      ${PROM_URL}"
echo "  tracking:        ${TRACKING_URL}"
echo "  stale_threshold: ${STALE_THRESHOLD}s"
echo "  mode:            $([ "$STRICT" -eq 1 ] && echo 'strict (warnings are fatal)' || echo 'default (only critical failures break)')"
echo ""

# ════════════════════════════════════════════════════════════════════════════════
# 1. Prometheus connectivity — fail-closed regardless of mode
# ════════════════════════════════════════════════════════════════════════════════
echo -e "${BOLD}── 1. Prometheus connectivity${NC}"

if ! curl -sf --max-time 5 "${PROM_URL}/-/healthy" >/dev/null 2>&1; then
    _critical "Prometheus not reachable at ${PROM_URL}/-/healthy"
    echo ""
    echo -e "${RED}FATAL: cannot validate metrics without Prometheus. Exiting.${NC}"
    echo ""
    exit 1
fi
_ok "Prometheus healthy"
echo ""

# ════════════════════════════════════════════════════════════════════════════════
# 2. Core security metrics — presence + freshness + value
#    Presence:   critical   — missing metric = cannot trust security state
#    Freshness:  critical   — stale metric = security decisions based on old data
#    Value:      warn       — 0 is valid (no rejections = healthy); NaN is not
# ════════════════════════════════════════════════════════════════════════════════
echo -e "${BOLD}── 2. Core security metrics${NC}"

if _check_metric "orbit_tracking_rejected_total" "critical"; then
    _check_freshness "orbit_tracking_rejected_total" "critical"
    _check_value     "orbit_tracking_rejected_total" "warn" "true"
fi

echo ""

if _check_metric "orbit_real_usage_alive" "critical"; then
    _check_freshness "orbit_real_usage_alive" "critical"
    # 0 = no real usage in last 5 min; warn, not fatal (expected in shadow/early stage)
    _check_value     "orbit_real_usage_alive" "warn" "true"
fi

echo ""

# ════════════════════════════════════════════════════════════════════════════════
# 3. Shadow mode metrics — expected gaps
#    These metrics do not exist yet (implementation pending).
#    Absence is [!] informational. If present, freshness/value are [~] warnings.
# ════════════════════════════════════════════════════════════════════════════════
echo -e "${BOLD}── 3. Shadow mode metrics${NC}"

if _check_metric "orbit_shadow_activation_total" "expected"; then
    _check_freshness "orbit_shadow_activation_total" "warn"
    _check_value     "orbit_shadow_activation_total" "warn" "true"
fi

if _check_metric "orbit_shadow_activation_rate" "expected"; then
    _check_freshness "orbit_shadow_activation_rate" "warn"
    _check_value     "orbit_shadow_activation_rate" "warn" "true"
fi

echo ""

# ════════════════════════════════════════════════════════════════════════════════
# 4. Recording rules — presence + freshness
#    Presence:   critical  — missing rule means dashboards/alerts use raw metrics
#    Freshness:  warn      — rule loaded but not recently evaluated = data lag
# ════════════════════════════════════════════════════════════════════════════════
echo -e "${BOLD}── 4. Recording rules${NC}"

for _rule in \
    "orbit:activation_rate" \
    "orbit:rejected_total:prod" \
    "orbit:real_usage_total:prod" \
    "orbit:skill_activation_rate_5m:prod" \
    "orbit:heartbeat_rate:prod"
do
    if _check_rule "$_rule" "critical"; then
        _check_freshness "$_rule" "warn"
    fi
done

echo ""

# ════════════════════════════════════════════════════════════════════════════════
# 5. Tracking /metrics direct check — defense-in-depth
#    Prometheus is authoritative; this cross-checks the source.
#    Unreachable server = warning only (Prometheus may still have cached data).
# ════════════════════════════════════════════════════════════════════════════════
echo -e "${BOLD}── 5. Tracking /metrics direct check${NC}"

METRICS_RAW=$(curl -sf --max-time 5 "${TRACKING_URL}/metrics" 2>/dev/null) || METRICS_RAW=""

if [[ -z "$METRICS_RAW" ]]; then
    _warning "tracking server not reachable at ${TRACKING_URL} (Prometheus is authoritative)"
else
    for _metric in "orbit_tracking_rejected_total" "orbit_real_usage_alive"; do
        if echo "$METRICS_RAW" | grep -q "^${_metric}"; then
            _ok "${_metric} present in /metrics"
        else
            _critical "${_metric} missing from /metrics"
        fi
    done
fi

echo ""

# ════════════════════════════════════════════════════════════════════════════════
# Verdict
# ════════════════════════════════════════════════════════════════════════════════

EFFECTIVE_FAIL="$CRIT_FAIL"
[[ "$STRICT" -eq 1 ]] && EFFECTIVE_FAIL=$(( CRIT_FAIL + WARN ))

echo "  PASS:     ${PASS}"
echo "  CRITICAL: ${CRIT_FAIL}"
echo "  WARN:     ${WARN}"
echo "  EXPECTED: ${EXPECTED}"
echo ""

if [[ "${EFFECTIVE_FAIL}" -eq 0 ]]; then
    if [[ "${WARN}" -gt 0 ]]; then
        echo -e "${YELLOW}${BOLD}OK (with warnings): integrity verified — review [~] items before scaling.${NC}"
    else
        echo -e "${GREEN}${BOLD}OK: observability integrity verified — data can be trusted.${NC}"
    fi
    echo ""
    exit 0
else
    STRICT_NOTE="$([ "$STRICT" -eq 1 ] && echo ' [strict mode]' || echo '')"
    echo -e "${RED}${BOLD}FAIL: ${EFFECTIVE_FAIL} check(s) failed${STRICT_NOTE} — do not trust observability data.${NC}"
    echo ""
    echo "  Fix missing metrics/rules before making product decisions from this data."
    echo ""
    exit 1
fi
