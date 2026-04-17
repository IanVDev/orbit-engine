#!/usr/bin/env bash
# scripts/test_run.sh — Testa o comando orbit run em múltiplos cenários.
#
# Cenários validados:
#   1. orbit run echo hello           → exit 0, output capturado, proof presente
#   2. orbit run echo hello world     → output com múltiplas palavras
#   3. orbit run --json echo hi       → JSON válido com campos obrigatórios
#   4. orbit run false                → exit 1 (fail-closed, exit code != 0)
#   5. orbit run                      → exit 1 (nenhum argumento → usage error)
#   6. orbit run cat /arquivo-inexistente-$$ → exit 1 (exit code != 0)
#   7. Proof é sha256 (64 hex chars)
#   8. Session ID começa com "run-"
#
# Fail-closed: qualquer falha aborta com exit 1.
#
# Uso:
#   ./scripts/test_run.sh
#   ./scripts/test_run.sh --verbose
set -euo pipefail

# ── Funções ───────────────────────────────────────────────────────────────────

PASS=0
FAIL=0
VERBOSE=0

_pass()  { PASS=$((PASS+1)); printf '  ✅  %s\n' "$1"; }
_fail()  { FAIL=$((FAIL+1)); printf '  ❌  %s\n' "$1" >&2; }
_abort() { printf '\n❌  FATAL: %s\n' "$1" >&2; exit 1; }
_header(){ printf '\n%s\n%s\n' "$1" "$(printf '─%.0s' {1..55})"; }
_sep()   { printf '%s\n' "$(printf '·%.0s' {1..55})"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --verbose|-v) VERBOSE=1; shift ;;
    *) _abort "argumento desconhecido: $1" ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TRACKING_DIR="${REPO_ROOT}/tracking"
ORBIT_BIN="/tmp/orbit-run-test-$$"

trap 'rm -f "${ORBIT_BIN}"' EXIT

# ── Build ─────────────────────────────────────────────────────────────────────

_header "TEST orbit run"

echo "[build] Compilando orbit CLI..."
(cd "${TRACKING_DIR}" && go build -o "${ORBIT_BIN}" ./cmd/orbit/) \
  || _abort "go build falhou"
echo "[build] ✓  binário em ${ORBIT_BIN}"

show_output() {
  local output="$1"
  if [[ "${VERBOSE}" -eq 1 ]]; then
    echo "${output}" | sed 's/^/    | /'
    _sep
  fi
}

# Helper: extrai o primeiro bloco JSON de um output que pode ter log lines antes.
extract_json() {
  python3 -c "
import sys, json
raw = sys.stdin.read()
start = raw.find('{')
if start < 0:
    sys.exit(1)
print(raw[start:])
"
}

# ── Cenário 1 — orbit run echo hello ─────────────────────────────────────────

_header "Cenário 1: orbit run echo hello → exit 0 + proof"

OUT1="$("${ORBIT_BIN}" run echo hello 2>&1)" || _abort "orbit run echo hello falhou inesperadamente"
show_output "${OUT1}"

if echo "${OUT1}" | grep -q "hello"; then
  _pass "cenário 1: output 'hello' capturado"
else
  _fail "cenário 1: output 'hello' AUSENTE"
fi

if echo "${OUT1}" | grep -q "Proof (sha256):"; then
  _pass "cenário 1: proof exibido"
else
  _fail "cenário 1: proof AUSENTE"
fi

if echo "${OUT1}" | grep -q "exit 0\|Exit code:.*0\|concluído com sucesso"; then
  _pass "cenário 1: sucesso confirmado (exit 0)"
else
  _fail "cenário 1: confirmação de sucesso AUSENTE"
fi

# ── Cenário 2 — orbit run echo hello world ───────────────────────────────────

_header "Cenário 2: orbit run echo hello world → palavras múltiplas capturadas"

OUT2="$("${ORBIT_BIN}" run echo hello world 2>&1)" \
  || _abort "orbit run echo hello world falhou"
show_output "${OUT2}"

if echo "${OUT2}" | grep -q "hello world"; then
  _pass "cenário 2: 'hello world' capturado"
else
  _fail "cenário 2: 'hello world' AUSENTE"
fi

if echo "${OUT2}" | grep -q "Session:"; then
  _pass "cenário 2: Session ID exibido"
else
  _fail "cenário 2: Session ID AUSENTE"
fi

# ── Cenário 3 — orbit run --json echo hi ─────────────────────────────────────

_header "Cenário 3: orbit run --json echo hi → JSON estruturado"

OUT3="$("${ORBIT_BIN}" run --json echo hi 2>&1)" \
  || _abort "orbit run --json falhou"
show_output "${OUT3}"

# Valida que é JSON válido e tem campos obrigatórios.
if echo "${OUT3}" | extract_json | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'proof' in d and 'exit_code' in d and 'output' in d and 'session_id' in d" 2>/dev/null; then
  _pass "cenário 3: JSON válido com campos obrigatórios (proof, exit_code, output, session_id)"
else
  _fail "cenário 3: JSON inválido ou campos obrigatórios ausentes"
fi

if echo "${OUT3}" | extract_json | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['exit_code'] == 0" 2>/dev/null; then
  _pass "cenário 3: exit_code=0 no JSON"
else
  _fail "cenário 3: exit_code != 0 no JSON"
fi

if echo "${OUT3}" | extract_json | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'hi' in d['output']" 2>/dev/null; then
  _pass "cenário 3: output 'hi' presente no JSON"
else
  _fail "cenário 3: output 'hi' AUSENTE no JSON"
fi

# ── Cenário 4 — orbit run false → exit 1 (fail-closed) ───────────────────────

_header "Cenário 4: orbit run false → exit 1 (fail-closed)"

RUN_EXIT=0
OUT4="$("${ORBIT_BIN}" run false 2>&1)" || RUN_EXIT=$?
show_output "${OUT4}"

if [[ "${RUN_EXIT}" -ne 0 ]]; then
  _pass "cenário 4: exit != 0 (fail-closed ativo)"
else
  _fail "cenário 4: exit 0 inesperado — fail-closed NÃO está funcionando"
fi

# ── Cenário 5 — orbit run (sem argumentos) ────────────────────────────────────

_header "Cenário 5: orbit run (sem argumentos) → usage error"

NO_ARGS_EXIT=0
OUT5="$("${ORBIT_BIN}" run 2>&1)" || NO_ARGS_EXIT=$?
show_output "${OUT5}"

if [[ "${NO_ARGS_EXIT}" -ne 0 ]]; then
  _pass "cenário 5: exit != 0 com nenhum argumento"
else
  _fail "cenário 5: exit 0 inesperado sem argumentos"
fi

if echo "${OUT5}" | grep -qi "uso:\|orbit run\|comando\|uso"; then
  _pass "cenário 5: mensagem de uso exibida"
else
  _fail "cenário 5: mensagem de uso AUSENTE"
fi

# ── Cenário 6 — orbit run cat /arquivo-inexistente → exit 1 ──────────────────

_header "Cenário 6: orbit run cat /inexistente → exit != 0"

MISSING_EXIT=0
OUT6="$("${ORBIT_BIN}" run cat "/arquivo-inexistente-orbit-test-$$" 2>&1)" \
  || MISSING_EXIT=$?
show_output "${OUT6}"

if [[ "${MISSING_EXIT}" -ne 0 ]]; then
  _pass "cenário 6: exit != 0 para arquivo inexistente"
else
  _fail "cenário 6: exit 0 inesperado para arquivo inexistente"
fi

# ── Cenário 7 — Proof tem formato sha256 (64 hex chars) ──────────────────────

_header "Cenário 7: proof tem formato sha256 (64 hex chars)"

OUT7="$("${ORBIT_BIN}" run --json echo verify 2>&1)"

# Extrai o campo proof do JSON e verifica comprimento.
PROOF_LEN="$(echo "${OUT7}" | extract_json | python3 -c "
import json, sys
d = json.load(sys.stdin)
print(len(d.get('proof', '')))
" 2>/dev/null || echo "0")"

if [[ "${PROOF_LEN}" -eq 64 ]]; then
  _pass "cenário 7: proof tem 64 hex chars (sha256)"
else
  _fail "cenário 7: proof tem ${PROOF_LEN} chars (esperado: 64)"
fi

# ── Cenário 8 — Session ID começa com "run-" ──────────────────────────────────

_header "Cenário 8: session_id começa com 'run-'"

OUT8="$("${ORBIT_BIN}" run --json echo session 2>&1)"

SESSION_PREFIX="$(echo "${OUT8}" | extract_json | python3 -c "
import json, sys
d = json.load(sys.stdin)
sid = d.get('session_id', '')
print('ok' if sid.startswith('run-') else 'fail')
" 2>/dev/null || echo "fail")"

if [[ "${SESSION_PREFIX}" == "ok" ]]; then
  _pass "cenário 8: session_id começa com 'run-'"
else
  _fail "cenário 8: session_id não começa com 'run-'"
fi

# ── Resultado ─────────────────────────────────────────────────────────────────

echo ""
echo "$(printf '─%.0s' {1..55})"
printf '  Resultado: %d ✅ passou   %d ❌ falhou\n' "${PASS}" "${FAIL}"
echo "$(printf '─%.0s' {1..55})"

if [[ "${FAIL}" -gt 0 ]]; then
  echo ""
  echo "❌  test_run.sh FALHOU — ${FAIL} verificação(ões) não passou(aram)"
  exit 1
fi

echo ""
echo "✅  test_run.sh PASSOU — orbit run validado em todos os cenários"
echo ""
