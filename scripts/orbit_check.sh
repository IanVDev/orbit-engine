#!/usr/bin/env bash
# scripts/orbit_check.sh — Sovereign validation command for orbit-engine EC2
#
# Three-stage pipeline:
#   Stage 1 — SSH setup + key validation     (ssh_setup.sh, runs locally)
#   Stage 2 — Remote environment baseline    (ssh_remote_validate.sh via SSH pipe)
#   Stage 3 — Extended remote checks         (orbit_check_remote.sh via SSH pipe)
#             Strict: port 9091, /health 200, /metrics orbit_ series, sha256, service
#
# Fail-closed: exits 1 if ANY stage fails.
# Prints a clear per-stage OK / FAIL summary before exiting.
# Never uses root. Never exposes credentials.
#
# Required env vars:
#   ORBIT_EC2_HOST   — EC2 public DNS or IP
#   ORBIT_EC2_USER   — non-root SSH user (e.g. ubuntu, ec2-user)
#   ORBIT_SSH_KEY    — path to .pem private key
#
# Optional:
#   ORBIT_SSH_PORT        — SSH port (default: 22)
#   ORBIT_PROJECT_DIR     — remote project path (default: /opt/orbit-engine)
#   ORBIT_GATEWAY_BIN     — remote binary path (default: /usr/local/bin/orbit-gateway)
#   ORBIT_GATEWAY_URL     — gateway base URL on remote (default: http://localhost:9091)
#   ORBIT_GATEWAY_SHA256  — expected sha256 of binary (enables hash pinning when set)

set -uo pipefail
# -e intentionally omitted: all three stages must run so the summary is always printed

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Helpers ───────────────────────────────────────────────────────────────────

log()  { echo "[orbit-check] $*"; }
err()  { echo "[orbit-check] ERROR: $*" >&2; }
fail() { err "$*"; exit 1; }

sep()  { log "----------------------------------------------------------------"; }

# ── Pre-flight: validate required env vars (hard fail before any stage) ───────

[[ -z "${ORBIT_EC2_HOST:-}"  ]] && fail "ORBIT_EC2_HOST is not set"
[[ -z "${ORBIT_EC2_USER:-}"  ]] && fail "ORBIT_EC2_USER is not set"
[[ -z "${ORBIT_SSH_KEY:-}"   ]] && fail "ORBIT_SSH_KEY is not set"
[[ "${ORBIT_EC2_USER}" == "root" ]] && fail "ORBIT_EC2_USER must not be 'root'"
[[ "$(id -u)" -eq 0 ]]           && fail "orbit_check.sh must NOT run as root"

# Resolve optional vars with defaults (forwarded to remote, never logged as secrets)
SSH_PORT="${ORBIT_SSH_PORT:-22}"
GATEWAY_BIN="${ORBIT_GATEWAY_BIN:-/usr/local/bin/orbit-gateway}"
GATEWAY_URL="${ORBIT_GATEWAY_URL:-http://localhost:9091}"
GATEWAY_SHA256="${ORBIT_GATEWAY_SHA256:-}"

# ── SSH options (shared across all remote calls) ───────────────────────────────

SSH_OPTS=(
    -i "${ORBIT_SSH_KEY}"
    -p "${SSH_PORT}"
    -o "IdentitiesOnly=yes"
    -o "BatchMode=yes"
    -o "ConnectTimeout=10"
    -o "StrictHostKeyChecking=accept-new"
    -o "ServerAliveInterval=30"
    -o "ServerAliveCountMax=3"
    -o "ForwardAgent=no"
    -o "ForwardX11=no"
)

SSH_TARGET="${ORBIT_EC2_USER}@${ORBIT_EC2_HOST}"

# Pipe a local script to the remote host (baseline — no extra env vars)
ssh_pipe() {
    local script="$1"
    ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" 'bash -s' < "${script}"
}

# Pipe a local script with forwarded non-sensitive config env vars
ssh_pipe_ext() {
    local script="$1"
    ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" \
        "export ORBIT_GATEWAY_BIN='${GATEWAY_BIN}'; \
         export ORBIT_GATEWAY_URL='${GATEWAY_URL}'; \
         export ORBIT_GATEWAY_SHA256='${GATEWAY_SHA256}'; \
         bash -s" \
        < "${script}"
}

# ── Stage runner ──────────────────────────────────────────────────────────────

declare -a RESULTS=()
OVERALL_FAIL=0

run_stage() {
    local name="$1"; shift
    echo ""
    sep
    log "STAGE: ${name}"
    sep
    if "$@"; then
        RESULTS+=("[OK]   ${name}")
    else
        RESULTS+=("[FAIL] ${name}")
        OVERALL_FAIL=1
    fi
}

# ── Stage 1: SSH setup + key validation ──────────────────────────────────────
# Validates local key permissions (enforces 400), connectivity, non-root on remote

run_stage "1. SSH setup + key validation" \
    bash "${SCRIPT_DIR}/ssh_setup.sh"

# ── Stage 2: Remote environment baseline ─────────────────────────────────────
# Lenient: skips checks for resources not yet deployed, no strict service requirement

run_stage "2. Remote environment baseline" \
    ssh_pipe "${SCRIPT_DIR}/ssh_remote_validate.sh"

# ── Stage 3: Extended remote checks (strict) ──────────────────────────────────
# Strict: port 9091 must be listening, /health must be 200, /metrics must
# expose orbit_ series, sha256 computed (pinned if ORBIT_GATEWAY_SHA256 set),
# orbit-gateway.service must be active — any failure is fatal

run_stage "3. Extended remote checks (port, /health, /metrics, sha256, service)" \
    ssh_pipe_ext "${SCRIPT_DIR}/orbit_check_remote.sh"

# ── Final report ──────────────────────────────────────────────────────────────

echo ""
echo "=================================================================="
echo "  orbit-check — FINAL REPORT"
echo "=================================================================="
echo "  Target  : ${SSH_TARGET}"
echo "  Port    : ${SSH_PORT}"
echo "  Gateway : ${GATEWAY_URL}"
echo "------------------------------------------------------------------"
for r in "${RESULTS[@]}"; do
    echo "  ${r}"
done
echo "------------------------------------------------------------------"
if [[ "${OVERALL_FAIL}" -eq 0 ]]; then
    echo "  Result  : ALL STAGES PASSED — environment is healthy"
else
    echo "  Result  : ONE OR MORE STAGES FAILED — see output above"
fi
echo "=================================================================="
echo ""

exit "${OVERALL_FAIL}"
