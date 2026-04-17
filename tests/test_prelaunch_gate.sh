#!/usr/bin/env bash
# test_prelaunch_gate.sh — Testa as 4 validações core do prelaunch_gate.sh via --check-only.
#
# Garante que VEREDITO != GO quando qualquer validação falha:
#   1. Tracking server parado             → exit 1 (NO-GO)
#   2. /health retorna status != "ok"     → exit 1 (NO-GO)
#   3. /metrics retorna vazio             → exit 1 (NO-GO)
#   4. /metrics sem métrica obrigatória   → exit 1 (NO-GO)
#   5. orbit não encontrado no PATH       → exit 1 (NO-GO)
#   6. Tudo correto                       → exit 0 (GO)
#
# Uso:
#   ./tests/test_prelaunch_gate.sh
#
# Requisitos: bash 4+, python3

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
GATE="${REPO_ROOT}/scripts/prelaunch_gate.sh"

GREEN='\033[0;32m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

PASS=0
FAIL=0

TRACK_PORT=19200
MOCK_PID=""

# ── Helpers ───────────────────────────────────────────────────────────────────

_result() {
  local name="$1" expected="$2" got="$3"
  if [[ "$expected" == "$got" ]]; then
    echo -e "  ${GREEN}[PASS]${NC} ${name}"
    ((PASS++)) || true
  else
    echo -e "  ${RED}[FAIL]${NC} ${name}"
    echo -e "         esperado=${expected}  obtido=${got}"
    ((FAIL++)) || true
  fi
}

_result_contains() {
  local name="$1" pattern="$2" output="$3"
  if echo "$output" | grep -q "$pattern"; then
    echo -e "  ${GREEN}[PASS]${NC} ${name}"
    ((PASS++)) || true
  else
    echo -e "  ${RED}[FAIL]${NC} ${name}"
    echo -e "         padrão '${pattern}' não encontrado na saída"
    ((FAIL++)) || true
  fi
}

_result_not_contains() {
  local name="$1" pattern="$2" output="$3"
  if ! echo "$output" | grep -q "$pattern"; then
    echo -e "  ${GREEN}[PASS]${NC} ${name}"
    ((PASS++)) || true
  else
    echo -e "  ${RED}[FAIL]${NC} ${name}"
    echo -e "         padrão proibido '${pattern}' encontrado na saída"
    ((FAIL++)) || true
  fi
}

_start_mock() {
  local port="$1" health="$2" metrics="$3"
  python3 -c "
import sys, http.server, os, signal

port    = int(sys.argv[1])
health  = sys.argv[2].encode()
metrics = sys.argv[3].encode()

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-Type','application/json')
            self.end_headers()
            self.wfile.write(health)
        elif self.path == '/metrics':
            self.send_response(200)
            self.send_header('Content-Type','text/plain')
            self.end_headers()
            self.wfile.write(metrics)
        else:
            self.send_response(404); self.end_headers()

http.server.HTTPServer(('127.0.0.1', port), H).serve_forever()
" "$port" "$health" "$metrics" &
  MOCK_PID=$!
  local i=0
  while (( i < 30 )); do
    curl -sf --max-time 1 "http://127.0.0.1:${port}/health" >/dev/null 2>&1 && return 0
    sleep 0.1
    (( i++ )) || true
  done
  echo "ERRO: mock server não subiu na porta ${port}" >&2
  return 1
}

_stop_mock() {
  if [[ -n "$MOCK_PID" ]]; then
    kill "$MOCK_PID" 2>/dev/null || true
    wait "$MOCK_PID" 2>/dev/null || true
    MOCK_PID=""
  fi
}

_run_gate() {
  local env_prefix="$1"
  shift
  local out tmpout
  tmpout=$(mktemp /tmp/gate-test-XXXXXX)
  ( eval "$env_prefix" bash "$GATE" "$@" >"$tmpout" 2>&1 )
  LAST_EXIT=$?
  LAST_OUTPUT=$(cat "$tmpout")
  rm -f "$tmpout"
}

LAST_EXIT=0
LAST_OUTPUT=""

trap '_stop_mock' EXIT

VALID_METRICS=$(printf '%s\n' \
  'orbit_skill_activations_total{mode="auto"} 42' \
  'orbit_tracking_rejected_total{reason="replay"} 3' \
  'orbit_behavior_abuse_total 0' \
  'orbit_tracking_up 1')

HEALTH_OK='{"status":"ok"}'

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}  prelaunch_gate --check-only — Test Suite${NC}"
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"

# ════════════════════════════════════════════════════════════════════════════
# Cenário 1: Tracking server parado → NO-GO
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 1: Tracking server parado (NO-GO)${NC}"

_stop_mock
_run_gate "TRACKING_HOST='127.0.0.1:${TRACK_PORT}'" --check-only

_result           "exit code = 1"          "1"     "$LAST_EXIT"
_result_not_contains "VEREDITO não é GO"   "VEREDITO: GO" "$LAST_OUTPUT"
_result_contains  "reporta falha de server" "GATE ABORTADO" "$LAST_OUTPUT"

# ════════════════════════════════════════════════════════════════════════════
# Cenário 2: /health retorna status != "ok" → NO-GO
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 2: /health retorna status=degraded (NO-GO)${NC}"

_start_mock "$TRACK_PORT" '{"status":"degraded"}' "$VALID_METRICS"

_run_gate "TRACKING_HOST='127.0.0.1:${TRACK_PORT}'" --check-only

_result           "exit code = 1"          "1"     "$LAST_EXIT"
_result_not_contains "VEREDITO não é GO"   "VEREDITO: GO" "$LAST_OUTPUT"
_result_contains  "reporta status inválido" "degraded" "$LAST_OUTPUT"

_stop_mock

# ════════════════════════════════════════════════════════════════════════════
# Cenário 3: /metrics retorna vazio (404) → NO-GO
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 3: /metrics retorna vazio (NO-GO)${NC}"

_start_mock "$TRACK_PORT" "$HEALTH_OK" ""

_run_gate "TRACKING_HOST='127.0.0.1:${TRACK_PORT}'" --check-only

_result           "exit code = 1"          "1"     "$LAST_EXIT"
_result_not_contains "VEREDITO não é GO"   "VEREDITO: GO" "$LAST_OUTPUT"
_result_contains  "reporta /metrics inativo" "GATE ABORTADO" "$LAST_OUTPUT"

_stop_mock

# ════════════════════════════════════════════════════════════════════════════
# Cenário 4: /metrics sem métrica obrigatória → NO-GO
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 4: /metrics sem orbit_behavior_abuse_total (NO-GO)${NC}"

PARTIAL_METRICS=$(printf '%s\n' \
  'orbit_skill_activations_total{mode="auto"} 10' \
  'orbit_tracking_rejected_total{reason="replay"} 1' \
  'orbit_tracking_up 1')

_start_mock "$TRACK_PORT" "$HEALTH_OK" "$PARTIAL_METRICS"

_run_gate "TRACKING_HOST='127.0.0.1:${TRACK_PORT}'" --check-only

_result           "exit code = 1"                    "1"     "$LAST_EXIT"
_result_not_contains "VEREDITO não é GO"             "VEREDITO: GO" "$LAST_OUTPUT"
_result_contains  "identifica métrica ausente"       "orbit_behavior_abuse_total" "$LAST_OUTPUT"

_stop_mock

# ════════════════════════════════════════════════════════════════════════════
# Cenário 5: orbit não encontrado no PATH → NO-GO
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 5: orbit não encontrado no PATH (NO-GO)${NC}"

_start_mock "$TRACK_PORT" "$HEALTH_OK" "$VALID_METRICS"

_run_gate "TRACKING_HOST='127.0.0.1:${TRACK_PORT}' PATH='/bin:/usr/bin'" --check-only

_result           "exit code = 1"          "1"     "$LAST_EXIT"
_result_not_contains "VEREDITO não é GO"   "VEREDITO: GO" "$LAST_OUTPUT"
_result_contains  "reporta orbit ausente"  "não encontrado no PATH" "$LAST_OUTPUT"

_stop_mock

# ════════════════════════════════════════════════════════════════════════════
# Cenário 6: Tudo correto → GO
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 6: Sistema saudável → GO${NC}"

if ! command -v orbit >/dev/null 2>&1; then
  echo -e "  ${YELLOW:-}[SKIP]${NC} orbit não instalado — cenário 6 requer binário orbit no PATH"
else
  _start_mock "$TRACK_PORT" "$HEALTH_OK" "$VALID_METRICS"

  _run_gate "TRACKING_HOST='127.0.0.1:${TRACK_PORT}'" --check-only

  _result        "exit code = 0"     "0"     "$LAST_EXIT"
  _result_contains "VEREDITO é GO"   "VEREDITO: GO" "$LAST_OUTPUT"

  _stop_mock
fi

# ════════════════════════════════════════════════════════════════════════════
# RESULTADO FINAL
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  PASS: ${PASS}"
echo -e "  FAIL: ${FAIL}"
echo ""

if [[ "$FAIL" -eq 0 ]]; then
  echo -e "${GREEN}${BOLD}  ✅ Todos os testes passaram. Gate fail-closed confirmado.${NC}"
  echo ""
  exit 0
else
  echo -e "${RED}${BOLD}  ❌ ${FAIL} teste(s) falharam.${NC}"
  echo ""
  exit 1
fi
