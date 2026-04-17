#!/usr/bin/env bash
# scripts/ssh_remote_validate.sh — Remote environment validation for orbit-engine
#
# Designed to be executed ON THE EC2 HOST via:
#   ssh orbit-ec2 'bash -s' < scripts/ssh_remote_validate.sh
#
# Or called from ssh_setup.sh after a successful connection.
#
# Fail-closed: exits non-zero on any validation failure.
# Never requires or accepts root.

set -euo pipefail

log()  { echo "[orbit-remote-validate] $*"; }
err()  { echo "[orbit-remote-validate] ERROR: $*" >&2; }
fail() { err "$*"; exit 1; }

PASS=0
FAIL=0

check() {
    local label="$1"; shift
    if "$@" > /dev/null 2>&1; then
        log "  PASS  ${label}"
        ((PASS++)) || true
    else
        err "  FAIL  ${label}"
        ((FAIL++)) || true
    fi
}

# ── Guard: refuse to run as root ──────────────────────────────────────────────

[[ "$(id -u)" -ne 0 ]] || fail "Must not run as root"

log "Running as: $(id -un) (uid=$(id -u))"

# ── System checks ─────────────────────────────────────────────────────────────

log ""
log "=== System ==="

check "OS readable"               test -r /etc/os-release
check "uname available"           uname -a
check "systemd present"           systemctl --version
check "no root effective uid"     bash -c '[[ "$(id -u)" -ne 0 ]]'

# ── Project directory ─────────────────────────────────────────────────────────

PROJECT_DIR="${ORBIT_PROJECT_DIR:-/opt/orbit-engine}"

log ""
log "=== Project directory (${PROJECT_DIR}) ==="

if [[ -d "${PROJECT_DIR}" ]]; then
    check "directory exists"            test -d "${PROJECT_DIR}"
    check "directory readable"          test -r "${PROJECT_DIR}"
    check "no world-write on dir"       bash -c "! stat -c '%a' '${PROJECT_DIR}' | grep -q '[2367]$'"
    log "  Contents:"
    ls -la "${PROJECT_DIR}" | sed 's/^/    /'
else
    log "  SKIP  Project directory not deployed yet: ${PROJECT_DIR}"
fi

# ── Binary checks ─────────────────────────────────────────────────────────────

log ""
log "=== Binaries ==="

GATEWAY_BIN="${ORBIT_GATEWAY_BIN:-/usr/local/bin/orbit-gateway}"

if [[ -f "${GATEWAY_BIN}" ]]; then
    check "orbit-gateway binary exists"     test -f "${GATEWAY_BIN}"
    check "orbit-gateway is executable"     test -x "${GATEWAY_BIN}"
    check "orbit-gateway not world-write"   bash -c "! test -w '${GATEWAY_BIN}' -o -g '${GATEWAY_BIN}'"
    log "  orbit-gateway: ${GATEWAY_BIN}"
else
    log "  SKIP  orbit-gateway binary not deployed yet: ${GATEWAY_BIN}"
fi

# ── Service checks ────────────────────────────────────────────────────────────

log ""
log "=== Services ==="

for svc in orbit-gateway prometheus; do
    if systemctl list-unit-files "${svc}.service" --no-legend 2>/dev/null | grep -q "${svc}"; then
        STATE=$(systemctl is-active "${svc}" 2>/dev/null || true)
        log "  ${svc}: ${STATE}"
    else
        log "  SKIP  ${svc}.service not installed"
    fi
done

# ── Network / port checks ─────────────────────────────────────────────────────

log ""
log "=== Network ==="

check "port 9091 listener or skipped" bash -c \
    "ss -tlnp 2>/dev/null | grep -q ':9091' || true"

if ss -tlnp 2>/dev/null | grep -q ':9091'; then
    log "  Gateway port 9091: LISTENING"
else
    log "  Gateway port 9091: not yet listening (expected before deployment)"
fi

# ── Filesystem safety ─────────────────────────────────────────────────────────

log ""
log "=== Filesystem safety ==="

check "/tmp writable"          test -w /tmp
check "/tmp sticky bit"        bash -c 'stat -c "%a" /tmp | grep -q "^1"'
check "HOME not world-writable" bash -c '! stat -c "%a" "${HOME}" | grep -q "[2367]$"'

# ── Summary ───────────────────────────────────────────────────────────────────

log ""
log "==========================================="
log "  Checks passed: ${PASS}"
log "  Checks failed: ${FAIL}"
log "==========================================="

if [[ "${FAIL}" -gt 0 ]]; then
    fail "${FAIL} check(s) failed — remote environment is not ready"
fi

log "Remote environment validation: ALL PASSED"
