#!/usr/bin/env bash
# scripts/ssh_setup.sh — Secure SSH setup and connection validation for orbit-engine EC2
#
# Fail-closed: any error aborts immediately.
# Never uses root. Never exposes credentials.
#
# Required env vars:
#   ORBIT_EC2_HOST   — EC2 public DNS or IP
#   ORBIT_EC2_USER   — non-root SSH user (e.g. ubuntu, ec2-user)
#   ORBIT_SSH_KEY    — path to .pem private key
#
# Optional:
#   ORBIT_SSH_PORT   — SSH port (default: 22)
#   ORBIT_PROJECT_DIR — remote project directory (default: /opt/orbit-engine)

set -euo pipefail

# ── Helpers ───────────────────────────────────────────────────────────────────

log()  { echo "[orbit-ssh] $*"; }
err()  { echo "[orbit-ssh] ERROR: $*" >&2; }
fail() { err "$*"; exit 1; }

# ── Defaults ──────────────────────────────────────────────────────────────────

SSH_PORT="${ORBIT_SSH_PORT:-22}"
PROJECT_DIR="${ORBIT_PROJECT_DIR:-/opt/orbit-engine}"

# ── Validate required env vars ────────────────────────────────────────────────

[[ -z "${ORBIT_EC2_HOST:-}"  ]] && fail "ORBIT_EC2_HOST is not set"
[[ -z "${ORBIT_EC2_USER:-}"  ]] && fail "ORBIT_EC2_USER is not set"
[[ -z "${ORBIT_SSH_KEY:-}"   ]] && fail "ORBIT_SSH_KEY is not set"

# Safety: refuse to run as root
if [[ "$(id -u)" -eq 0 ]]; then
    fail "This script must NOT be run as root"
fi

# Safety: refuse to connect as root
if [[ "${ORBIT_EC2_USER}" == "root" ]]; then
    fail "ORBIT_EC2_USER must not be 'root'. Use a non-privileged user."
fi

# ── Validate .pem key ─────────────────────────────────────────────────────────

KEY_PATH="${ORBIT_SSH_KEY}"

[[ -f "${KEY_PATH}" ]] || fail "Key file not found: ${KEY_PATH}"

# Enforce strict permissions (400 = owner read-only)
PERMS=$(stat -c "%a" "${KEY_PATH}" 2>/dev/null || stat -f "%OLp" "${KEY_PATH}" 2>/dev/null)
if [[ "${PERMS}" != "400" ]]; then
    log "Fixing key permissions: ${KEY_PATH} (was ${PERMS}, setting to 400)"
    chmod 400 "${KEY_PATH}"
    PERMS=$(stat -c "%a" "${KEY_PATH}" 2>/dev/null || stat -f "%OLp" "${KEY_PATH}" 2>/dev/null)
    [[ "${PERMS}" == "400" ]] || fail "Could not set permissions to 400 on ${KEY_PATH}"
fi
log "Key permissions: ${PERMS} (OK)"

# ── SSH base command (no credentials exposed in process list) ─────────────────

SSH_OPTS=(
    -i "${KEY_PATH}"
    -p "${SSH_PORT}"
    -o "IdentitiesOnly=yes"
    -o "BatchMode=yes"             # fail instead of prompting
    -o "ConnectTimeout=10"
    -o "StrictHostKeyChecking=accept-new"
    -o "ServerAliveInterval=30"
    -o "ServerAliveCountMax=3"
    -o "ForwardAgent=no"
    -o "ForwardX11=no"
)

SSH_TARGET="${ORBIT_EC2_USER}@${ORBIT_EC2_HOST}"

ssh_run() {
    # Run a single remote command; inherit SSH_OPTS
    ssh "${SSH_OPTS[@]}" "${SSH_TARGET}" -- "$@"
}

# ── Step 1: Connectivity check ────────────────────────────────────────────────

log "Testing SSH connectivity to ${ORBIT_EC2_HOST}:${SSH_PORT} as ${ORBIT_EC2_USER} ..."
if ! ssh_run "true" 2>&1; then
    fail "SSH connection failed. Check host, user, key, and security group rules."
fi
log "Connectivity: OK"

# ── Step 2: Refuse root login on remote ───────────────────────────────────────

REMOTE_USER=$(ssh_run "id -un")
if [[ "${REMOTE_USER}" == "root" ]]; then
    fail "Remote session is running as root — aborting (fail-closed)"
fi
log "Remote user: ${REMOTE_USER} (non-root OK)"

# ── Step 3: Basic remote commands ─────────────────────────────────────────────

log "Running: ls /tmp (smoke test) ..."
ssh_run "ls /tmp" > /dev/null
log "Remote ls: OK"

log "Running: systemctl status orbit-gateway (may be inactive/not-found — non-fatal) ..."
if ssh_run "systemctl status orbit-gateway --no-pager" 2>&1; then
    log "orbit-gateway service: active"
else
    STATUS=$?
    # systemctl exit 3 = inactive, 4 = not-found
    if [[ ${STATUS} -eq 3 ]]; then
        log "orbit-gateway service: installed but inactive"
    elif [[ ${STATUS} -eq 4 ]]; then
        log "orbit-gateway service: not yet installed (expected on fresh host)"
    else
        fail "Unexpected systemctl exit code ${STATUS}"
    fi
fi

# ── Step 4: Validate project directory ───────────────────────────────────────

log "Validating project directory: ${PROJECT_DIR} ..."
if ssh_run "test -d '${PROJECT_DIR}'"; then
    log "Project directory exists: ${PROJECT_DIR}"
    ssh_run "ls '${PROJECT_DIR}'"
else
    log "Project directory not found: ${PROJECT_DIR} (will need deployment)"
fi

# ── Done ──────────────────────────────────────────────────────────────────────

log ""
log "SSH validation complete."
log "  Host      : ${ORBIT_EC2_HOST}"
log "  Port      : ${SSH_PORT}"
log "  User      : ${ORBIT_EC2_USER}"
log "  Key perms : 400 (OK)"
log "  Connection: OK"
log "  Non-root  : OK"
