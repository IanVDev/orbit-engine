#!/usr/bin/env bash
# scripts/orbit_check_remote.sh — Extended strict validation for orbit-engine on EC2
#
# Runs ON the remote EC2 host. Invoked via SSH pipe from orbit_check.sh.
# Fail-closed: exits 1 if any check fails.
# Never runs as root. No external dependencies beyond curl and ss.
#
# Env vars forwarded by orbit_check.sh (non-sensitive config):
#   ORBIT_GATEWAY_BIN     — binary path   (default: /usr/local/bin/orbit-gateway)
#   ORBIT_GATEWAY_URL     — base URL      (default: http://localhost:9091)
#   ORBIT_GATEWAY_SHA256  — expected hash (optional; enables pinning when set)

set -uo pipefail
# -e intentionally omitted: accumulate all failures before exiting

log()  { echo "[orbit-remote-ext] $*"; }
err()  { echo "[orbit-remote-ext] ERROR: $*" >&2; }
fail() { err "$*"; exit 1; }

PASS=0
FAIL=0

check_pass() { log "  PASS  $1"; ((PASS++)) || true; }
check_fail() { err "  FAIL  $1"; ((FAIL++)) || true; }

check() {
    local label="$1"; shift
    if "$@" > /dev/null 2>&1; then
        check_pass "${label}"
    else
        check_fail "${label}"
    fi
}

# ── Guard: never run as root ──────────────────────────────────────────────────

[[ "$(id -u)" -ne 0 ]] || fail "Must not run as root"
log "Running as: $(id -un) (uid=$(id -u))"

GATEWAY_BIN="${ORBIT_GATEWAY_BIN:-/usr/local/bin/orbit-gateway}"
GATEWAY_URL="${ORBIT_GATEWAY_URL:-http://localhost:9091}"

# ── Check: prerequisites ──────────────────────────────────────────────────────

log ""
log "=== Prerequisites ==="

check "curl available"  which curl
check "ss available"    bash -c 'which ss || which netstat'
check "sha256sum available" which sha256sum

# ── Check: port 9091 LISTENING (strict — not a skip) ─────────────────────────

log ""
log "=== Port 9091 ==="

if ss -tlnp 2>/dev/null | grep -q ':9091'; then
    check_pass "port 9091 LISTENING"
else
    check_fail "port 9091 not listening — orbit-gateway is down or not bound"
fi

# ── Check: GET /health must return exactly 200 ────────────────────────────────

log ""
log "=== HTTP /health ==="

HEALTH_CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 \
    "${GATEWAY_URL}/health" 2>/dev/null || echo "000")

if [[ "${HEALTH_CODE}" == "200" ]]; then
    check_pass "GET /health → 200"
else
    check_fail "GET /health → ${HEALTH_CODE} (expected 200)"
fi

# ── Check: GET /metrics must expose orbit_ series ─────────────────────────────

log ""
log "=== HTTP /metrics ==="

METRICS_BODY=$(curl -sf --max-time 5 "${GATEWAY_URL}/metrics" 2>/dev/null || true)

if [[ -z "${METRICS_BODY}" ]]; then
    check_fail "/metrics returned empty body"
else
    # Match recording rules (orbit:*) and base metrics (orbit_*)
    if echo "${METRICS_BODY}" | grep -qE '^(orbit:|orbit_)'; then
        ORBIT_LINES=$(echo "${METRICS_BODY}" | grep -cE '^(orbit:|orbit_)' || true)
        check_pass "/metrics contains orbit series (${ORBIT_LINES} lines)"
        log "  Sample orbit series:"
        echo "${METRICS_BODY}" | grep -E '^(orbit:|orbit_)' | head -5 | sed 's/^/    /'
    else
        check_fail "/metrics has no orbit_ or orbit: series — governance violation"
    fi
fi

# ── Check: orbit-gateway binary + sha256 ──────────────────────────────────────

log ""
log "=== Binary integrity ==="

if [[ ! -f "${GATEWAY_BIN}" ]]; then
    check_fail "orbit-gateway binary not found: ${GATEWAY_BIN}"
else
    check "orbit-gateway is executable"     test -x "${GATEWAY_BIN}"
    check "orbit-gateway not world-writable" bash -c "! stat -c '%a' '${GATEWAY_BIN}' | grep -qE '[2367]\$'"

    SHA=$(sha256sum "${GATEWAY_BIN}" 2>/dev/null | awk '{print $1}' || echo "")
    if [[ -z "${SHA}" ]]; then
        check_fail "sha256sum failed on ${GATEWAY_BIN}"
    else
        log "  sha256: ${SHA}"
        if [[ -n "${ORBIT_GATEWAY_SHA256:-}" ]]; then
            if [[ "${SHA}" == "${ORBIT_GATEWAY_SHA256}" ]]; then
                check_pass "sha256 matches pinned hash"
            else
                check_fail "sha256 MISMATCH: got ${SHA} / expected ${ORBIT_GATEWAY_SHA256}"
            fi
        else
            check_pass "sha256 computed (set ORBIT_GATEWAY_SHA256 to enable pinning)"
        fi
    fi
fi

# ── Check: orbit-gateway service active (strict) ──────────────────────────────

log ""
log "=== Service ==="

if systemctl list-unit-files "orbit-gateway.service" --no-legend 2>/dev/null | grep -q "orbit-gateway"; then
    SVC_STATE=$(systemctl is-active "orbit-gateway" 2>/dev/null || echo "unknown")
    if [[ "${SVC_STATE}" == "active" ]]; then
        check_pass "orbit-gateway.service is active"
        # Log recent journal lines for context
        log "  Recent journal (last 3 lines):"
        journalctl -u orbit-gateway --no-pager -n 3 2>/dev/null | sed 's/^/    /' || true
    else
        check_fail "orbit-gateway.service is '${SVC_STATE}' (expected active)"
    fi
else
    check_fail "orbit-gateway.service not installed — deploy required"
fi

# ── Summary ───────────────────────────────────────────────────────────────────

log ""
log "==========================================="
log "  Extended checks passed : ${PASS}"
log "  Extended checks failed : ${FAIL}"
log "==========================================="

if [[ "${FAIL}" -gt 0 ]]; then
    fail "${FAIL} extended check(s) failed — remote environment is not healthy"
fi

log "Extended remote validation: ALL PASSED"
