#!/usr/bin/env bash
# test_orbit_check.sh — Testes do comando orbit-check.
#
# Cobre:
#   1. Serviço parado → service_down
#   2. Health com JSON inválido → health_invalid
#   3. Health sem status "ok" → health_invalid
#   4. Métrica crítica ausente → metrics_missing
#   5. SHA divergente → integrity_mismatch
#   6. SHA ausente em produção → exit 2 (abort)
#   7. Tudo correto → GO (exit 0)
#
# Uso:
#   ./tests/test_orbit_check.sh
#
# Requisitos: bash 4+, python3, nc (netcat) ou socat

set -uo pipefail

# _run_check: executa orbit-check e captura saída + exit code sem deixar
# o set -uo pipefail abortar o teste em caso de exit != 0.
# Uso:
#   _run_check "env_overrides"   →  popula LAST_OUTPUT e LAST_EXIT
LAST_OUTPUT=""
LAST_EXIT=0

_run_check() {
    local tmpout
    tmpout=$(mktemp /tmp/orbit-check-out-XXXXXX)
    # Desabilita set -e localmente via subshell separado
    ( eval "$1" bash "$ORBIT_CHECK" > "$tmpout" 2>&1 )
    LAST_EXIT=$?
    LAST_OUTPUT=$(cat "$tmpout")
    rm -f "$tmpout"
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ORBIT_CHECK="${REPO_ROOT}/scripts/orbit-check.sh"

# ── Cores ─────────────────────────────────────────────────────────────────────

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

PASS=0
FAIL=0

# ── Helpers ───────────────────────────────────────────────────────────────────

_result() {
    local name="$1" expected="$2" got="$3" detail="${4:-}"
    if [[ "$expected" == "$got" ]]; then
        echo -e "  ${GREEN}[PASS]${NC} ${name}"
        ((PASS++)) || true
    else
        echo -e "  ${RED}[FAIL]${NC} ${name}"
        echo -e "         esperado exit=${expected}, obtido exit=${got}"
        [[ -n "$detail" ]] && echo -e "         detalhe: ${detail}"
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

# Inicia um servidor HTTP mínimo em background usando python3 (sem dependências).
# Uso: _start_mock_server <porta> <corpo_resposta_health> <corpo_resposta_metrics>
# Retorna o PID via variável global MOCK_PID.
MOCK_PID=""

_start_mock_server() {
    local port="$1"
    local health_body="$2"
    local metrics_body="$3"

    # Servidor HTTP minimalista embutido em Python 3 (stdlib puro)
    python3 - "$port" "$health_body" "$metrics_body" &
    MOCK_PID=$!

    # Aguardar servidor subir (max 2s)
    local attempts=0
    while (( attempts < 20 )); do
        if curl -sf --max-time 1 "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
            break
        fi
        sleep 0.1
        (( attempts++ )) || true
    done
}

# Script Python embutido para o servidor mock
# Recebe args: porta health_body metrics_body
_mock_server_py() {
    python3 <<'PYEOF'
import sys
import http.server
import threading

port = int(sys.argv[1])
health_body = sys.argv[2].encode()
metrics_body = sys.argv[3].encode()

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass  # silencioso

    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(health_body)
        elif self.path == '/metrics':
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.end_headers()
            self.wfile.write(metrics_body)
        else:
            self.send_response(404)
            self.end_headers()

srv = http.server.HTTPServer(('127.0.0.1', port), Handler)
srv.serve_forever()
PYEOF
}

_stop_mock() {
    if [[ -n "$MOCK_PID" ]]; then
        kill "$MOCK_PID" 2>/dev/null || true
        wait "$MOCK_PID" 2>/dev/null || true
        MOCK_PID=""
    fi
}

# Trap para garantir cleanup
trap '_stop_mock' EXIT

# Portas de teste (altas para evitar conflito)
TRACK_PORT=19100
GATEWAY_PORT=19091

# Métricas válidas contendo todas as críticas obrigatórias
VALID_METRICS=$(cat <<'METRICS'
# HELP orbit_skill_activations_total Total de ativações de skill
# TYPE orbit_skill_activations_total counter
orbit_skill_activations_total{mode="auto"} 42
# HELP orbit_tracking_rejected_total Total de eventos rejeitados
# TYPE orbit_tracking_rejected_total counter
orbit_tracking_rejected_total{reason="replay"} 3
# HELP orbit_behavior_abuse_total Total de abusos detectados
# TYPE orbit_behavior_abuse_total counter
orbit_behavior_abuse_total 0
# HELP orbit_tracking_up 1 se o processo está vivo
# TYPE orbit_tracking_up gauge
orbit_tracking_up 1
METRICS
)

HEALTH_OK='{"status":"ok"}'

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}  orbit-check — Test Suite${NC}"
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo ""

# ════════════════════════════════════════════════════════════════════════════
# Cenário 1: Serviço parado → service_down
# ════════════════════════════════════════════════════════════════════════════

echo -e "${BOLD}── Cenário 1: Serviço parado (service_down)${NC}"

# Garantir que nenhum servidor está rodando nas portas de teste
_stop_mock

_run_check "TRACKING_HOST='127.0.0.1:${TRACK_PORT}' GATEWAY_HOST='127.0.0.1:${GATEWAY_PORT}' ENV=''"
OUTPUT="$LAST_OUTPUT"
EXIT_CODE="$LAST_EXIT"

_result "exit code = 1 (NO-GO)" "1" "$EXIT_CODE"
_result_contains "classifica service_down (tracking)" "service_down" "$OUTPUT"
echo ""

# ════════════════════════════════════════════════════════════════════════════
# Cenário 2: Health retorna JSON sem campo "status" → health_invalid
# ════════════════════════════════════════════════════════════════════════════

echo -e "${BOLD}── Cenário 2: Health com JSON sem status (health_invalid)${NC}"

# Servidor tracking: health sem "status", gateway: normal
python3 -c "
import sys, http.server

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-Type','application/json')
            self.end_headers()
            self.wfile.write(b'{\"version\":\"1.0\"}')
        elif self.path == '/metrics':
            self.send_response(200)
            self.send_header('Content-Type','text/plain')
            self.end_headers()
            self.wfile.write(b'orbit_skill_activations_total 1\norbit_tracking_rejected_total 0\norbit_behavior_abuse_total 0\n')
        else:
            self.send_response(404); self.end_headers()

http.server.HTTPServer(('127.0.0.1', ${TRACK_PORT}), H).serve_forever()
" &
MOCK_PID=$!

python3 -c "
import sys, http.server

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-Type','application/json')
            self.end_headers()
            self.wfile.write(b'{\"status\":\"ok\"}')
        else:
            self.send_response(404); self.end_headers()

http.server.HTTPServer(('127.0.0.1', ${GATEWAY_PORT}), H).serve_forever()
" &
GW_PID=$!
sleep 0.4

_run_check "TRACKING_HOST='127.0.0.1:${TRACK_PORT}' GATEWAY_HOST='127.0.0.1:${GATEWAY_PORT}' ENV=''"
OUTPUT="$LAST_OUTPUT"
EXIT_CODE="$LAST_EXIT"

_result "exit code = 1 (NO-GO)" "1" "$EXIT_CODE"
_result_contains "classifica health_invalid" "health_invalid" "$OUTPUT"

kill "$MOCK_PID" "$GW_PID" 2>/dev/null || true
wait "$MOCK_PID" "$GW_PID" 2>/dev/null || true
MOCK_PID=""
echo ""

# ════════════════════════════════════════════════════════════════════════════
# Cenário 3: Métrica crítica ausente → metrics_missing
# ════════════════════════════════════════════════════════════════════════════

echo -e "${BOLD}── Cenário 3: Métrica crítica ausente (metrics_missing)${NC}"

# orbit_behavior_abuse_total ausente nas métricas
PARTIAL_METRICS="orbit_skill_activations_total{mode=\"auto\"} 10
orbit_tracking_rejected_total{reason=\"replay\"} 1
orbit_tracking_up 1"

python3 -c "
import http.server

health = b'{\"status\":\"ok\"}'
metrics = b'''orbit_skill_activations_total{mode=\"auto\"} 10
orbit_tracking_rejected_total{reason=\"replay\"} 1
orbit_tracking_up 1
'''

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers(); self.wfile.write(health)
        elif self.path == '/metrics':
            self.send_response(200); self.send_header('Content-Type','text/plain'); self.end_headers(); self.wfile.write(metrics)
        else:
            self.send_response(404); self.end_headers()

http.server.HTTPServer(('127.0.0.1', ${TRACK_PORT}), H).serve_forever()
" &
MOCK_PID=$!

python3 -c "
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers(); self.wfile.write(b'{\"status\":\"ok\"}')
        else:
            self.send_response(404); self.end_headers()
http.server.HTTPServer(('127.0.0.1', ${GATEWAY_PORT}), H).serve_forever()
" &
GW_PID=$!
sleep 0.4

_run_check "TRACKING_HOST='127.0.0.1:${TRACK_PORT}' GATEWAY_HOST='127.0.0.1:${GATEWAY_PORT}' ENV=''"
OUTPUT="$LAST_OUTPUT"
EXIT_CODE="$LAST_EXIT"

_result "exit code = 1 (NO-GO)" "1" "$EXIT_CODE"
_result_contains "classifica metrics_missing" "metrics_missing" "$OUTPUT"
_result_contains "identifica a métrica ausente" "orbit_behavior_abuse_total" "$OUTPUT"

kill "$MOCK_PID" "$GW_PID" 2>/dev/null || true
wait "$MOCK_PID" "$GW_PID" 2>/dev/null || true
MOCK_PID=""
echo ""

# ════════════════════════════════════════════════════════════════════════════
# Cenário 4: SHA divergente → integrity_mismatch
# ════════════════════════════════════════════════════════════════════════════

echo -e "${BOLD}── Cenário 4: SHA-256 divergente (integrity_mismatch)${NC}"

# Criar binário temporário e fornecer SHA errado
FAKE_BIN=$(mktemp /tmp/orbit-gateway-test-XXXXXX)
echo "fake gateway binary content" > "$FAKE_BIN"
chmod +x "$FAKE_BIN"

python3 -c "
import http.server
health = b'{\"status\":\"ok\"}'
metrics = b'orbit_skill_activations_total 1\norbit_tracking_rejected_total 0\norbit_behavior_abuse_total 0\n'
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers(); self.wfile.write(health)
        elif self.path == '/metrics':
            self.send_response(200); self.send_header('Content-Type','text/plain'); self.end_headers(); self.wfile.write(metrics)
        else:
            self.send_response(404); self.end_headers()
http.server.HTTPServer(('127.0.0.1', ${TRACK_PORT}), H).serve_forever()
" &
MOCK_PID=$!

python3 -c "
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers(); self.wfile.write(b'{\"status\":\"ok\"}')
        else:
            self.send_response(404); self.end_headers()
http.server.HTTPServer(('127.0.0.1', ${GATEWAY_PORT}), H).serve_forever()
" &
GW_PID=$!
sleep 0.4

_run_check "TRACKING_HOST='127.0.0.1:${TRACK_PORT}' GATEWAY_HOST='127.0.0.1:${GATEWAY_PORT}' GATEWAY_BIN='$FAKE_BIN' ORBIT_GATEWAY_SHA256='0000000000000000000000000000000000000000000000000000000000000000' ENV=''"
OUTPUT="$LAST_OUTPUT"
EXIT_CODE="$LAST_EXIT"

_result "exit code = 1 (NO-GO)" "1" "$EXIT_CODE"
_result_contains "classifica integrity_mismatch" "integrity_mismatch" "$OUTPUT"

kill "$MOCK_PID" "$GW_PID" 2>/dev/null || true
wait "$MOCK_PID" "$GW_PID" 2>/dev/null || true
MOCK_PID=""
rm -f "$FAKE_BIN"
echo ""

# ════════════════════════════════════════════════════════════════════════════
# Cenário 5: ENV=production sem SHA → exit 2 (abort imediato)
# ════════════════════════════════════════════════════════════════════════════

echo -e "${BOLD}── Cenário 5: ENV=production sem SHA (abort fail-closed)${NC}"

_run_check "ENV=production ORBIT_GATEWAY_SHA256='' TRACKING_HOST='127.0.0.1:${TRACK_PORT}' GATEWAY_HOST='127.0.0.1:${GATEWAY_PORT}'"
OUTPUT="$LAST_OUTPUT"
EXIT_CODE="$LAST_EXIT"

_result "exit code = 2 (abort)" "2" "$EXIT_CODE"
_result_contains "menciona ORBIT_GATEWAY_SHA256" "ORBIT_GATEWAY_SHA256" "$OUTPUT"
_result_contains "menciona fail-closed" "fail-closed" "$OUTPUT"
echo ""

# ════════════════════════════════════════════════════════════════════════════
# Cenário 6: Tudo correto → GO (exit 0)
# ════════════════════════════════════════════════════════════════════════════

echo -e "${BOLD}── Cenário 6: Sistema saudável → GO${NC}"

FULL_METRICS="orbit_skill_activations_total{mode=\"auto\"} 100
orbit_tracking_rejected_total{reason=\"replay\"} 5
orbit_behavior_abuse_total 0
orbit_tracking_up 1"

python3 -c "
import http.server
health = b'{\"status\":\"ok\"}'
metrics = b'orbit_skill_activations_total{mode=\"auto\"} 100\norbit_tracking_rejected_total{reason=\"replay\"} 5\norbit_behavior_abuse_total 0\norbit_tracking_up 1\n'
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers(); self.wfile.write(health)
        elif self.path == '/metrics':
            self.send_response(200); self.send_header('Content-Type','text/plain'); self.end_headers(); self.wfile.write(metrics)
        else:
            self.send_response(404); self.end_headers()
http.server.HTTPServer(('127.0.0.1', ${TRACK_PORT}), H).serve_forever()
" &
MOCK_PID=$!

python3 -c "
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers(); self.wfile.write(b'{\"status\":\"ok\"}')
        else:
            self.send_response(404); self.end_headers()
http.server.HTTPServer(('127.0.0.1', ${GATEWAY_PORT}), H).serve_forever()
" &
GW_PID=$!
sleep 0.4

_run_check "TRACKING_HOST='127.0.0.1:${TRACK_PORT}' GATEWAY_HOST='127.0.0.1:${GATEWAY_PORT}' ENV=''"
OUTPUT="$LAST_OUTPUT"
EXIT_CODE="$LAST_EXIT"

_result "exit code = 0 (GO)" "0" "$EXIT_CODE"
_result_contains "veredito GO" "GO" "$OUTPUT"

kill "$MOCK_PID" "$GW_PID" 2>/dev/null || true
wait "$MOCK_PID" "$GW_PID" 2>/dev/null || true
MOCK_PID=""
echo ""

# ════════════════════════════════════════════════════════════════════════════
# RESULTADO FINAL
# ════════════════════════════════════════════════════════════════════════════

echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  PASS: ${PASS}"
echo -e "  FAIL: ${FAIL}"
echo ""

if [[ "$FAIL" -eq 0 ]]; then
    echo -e "${GREEN}${BOLD}  ✅ Todos os testes passaram.${NC}"
    echo ""
    exit 0
else
    echo -e "${RED}${BOLD}  ❌ ${FAIL} teste(s) falharam.${NC}"
    echo ""
    exit 1
fi
