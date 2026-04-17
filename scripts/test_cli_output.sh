#!/usr/bin/env bash
# scripts/test_cli_output.sh — Valida a qualidade do output de todos os subcomandos.
#
# Testa o padrão UX: contexto → resultado → significado → próximo passo.
# Cada subcomando deve exibir seção, dados técnicos e dica de próximo passo.
#
# Cenários:
#   1. orbit quickstart  — fluxo completo com mensagens UX chave
#   2. orbit run echo ok — seção, proof, sucesso, tip
#   3. orbit run --json  — JSON estruturado com todos os campos
#   4. orbit stats --host inválido — mensagem de erro clara + dica
#   5. orbit doctor      — seção de diagnóstico presente
#   6. orbit help        — todos os subcomandos documentados
#   7. orbit version     — formato semver
#   8. NO_COLOR desativa cores ANSI (sem escape sequences)
#
# Fail-closed: qualquer falha aborta com exit 1.
#
# Uso:
#   ./scripts/test_cli_output.sh
#   ./scripts/test_cli_output.sh --verbose
set -euo pipefail

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
ORBIT_BIN="/tmp/orbit-cli-output-test-$$"

trap 'rm -f "${ORBIT_BIN}"' EXIT

# ── Build ─────────────────────────────────────────────────────────────────────

_header "TEST orbit CLI output quality"

echo "[build] Compilando orbit CLI..."
(cd "${TRACKING_DIR}" && go build -o "${ORBIT_BIN}" ./cmd/orbit/) \
  || _abort "go build falhou"
echo "[build] ✓  binário em ${ORBIT_BIN}"

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

show() {
  local out="$1"
  if [[ "${VERBOSE}" -eq 1 ]]; then
    echo "${out}" | sed 's/^/    | /'
    _sep
  fi
}

# ── 1. orbit quickstart ───────────────────────────────────────────────────────

_header "Cenário 1: orbit quickstart — UX chave"

QS="$("${ORBIT_BIN}" quickstart 2>&1)" || _abort "quickstart falhou"
show "${QS}"

for pattern in "orbit quickstart" "Quickstart concluído" "proof válido" "orbit stats"; do
  if echo "${QS}" | grep -q "${pattern}"; then
    _pass "quickstart: '${pattern}' presente"
  else
    _fail "quickstart: '${pattern}' AUSENTE"
  fi
done

# ── 2. orbit run echo ok ──────────────────────────────────────────────────────

_header "Cenário 2: orbit run echo ok — seção + proof + tip"

RUN="$("${ORBIT_BIN}" run echo ok 2>&1)" || _abort "orbit run echo ok falhou"
show "${RUN}"

for pattern in "orbit run" "Proof (sha256):" "Session:" "concluído com sucesso" "orbit stats"; do
  if echo "${RUN}" | grep -q "${pattern}"; then
    _pass "run: '${pattern}' presente"
  else
    _fail "run: '${pattern}' AUSENTE"
  fi
done

# ── 3. orbit run --json echo jsontest ────────────────────────────────────────

_header "Cenário 3: orbit run --json — JSON com campos obrigatórios"

JSON_OUT="$("${ORBIT_BIN}" run --json echo jsontest 2>&1)"
show "${JSON_OUT}"

FIELDS_OK="$(echo "${JSON_OUT}" | extract_json | python3 -c "
import json, sys
d = json.load(sys.stdin)
required = ['command','exit_code','output','proof','session_id','timestamp','output_bytes']
missing = [k for k in required if k not in d]
print('ok' if not missing else 'missing: ' + ', '.join(missing))
" 2>/dev/null || echo "parse error")"

if [[ "${FIELDS_OK}" == "ok" ]]; then
  _pass "run --json: todos os 7 campos obrigatórios presentes"
else
  _fail "run --json: campos faltando — ${FIELDS_OK}"
fi

# ── 4. orbit stats com servidor inválido → mensagem de erro clara ─────────────

_header "Cenário 4: orbit stats (servidor inválido) → mensagem de erro"

STATS_ERR=""
STATS_EXIT=0
STATS_ERR="$("${ORBIT_BIN}" stats --host "http://127.0.0.1:1" 2>&1)" || STATS_EXIT=$?
show "${STATS_ERR}"

if [[ "${STATS_EXIT}" -ne 0 ]]; then
  _pass "stats erro: exit != 0 com servidor inválido"
else
  _fail "stats erro: exit 0 inesperado"
fi

if echo "${STATS_ERR}" | grep -qi "não foi possível\|servidor\|quickstart"; then
  _pass "stats erro: mensagem orientada a ação presente"
else
  _fail "stats erro: mensagem de diagnóstico AUSENTE"
fi

# ── 5. orbit doctor ───────────────────────────────────────────────────────────

_header "Cenário 5: orbit doctor — seção de diagnóstico"

DOCTOR="$("${ORBIT_BIN}" doctor 2>&1)" || true  # pode ter warnings mas não deve falhar
show "${DOCTOR}"

for pattern in "orbit doctor" "Verificações:" "Binário em execução:"; do
  if echo "${DOCTOR}" | grep -q "${pattern}"; then
    _pass "doctor: '${pattern}' presente"
  else
    _fail "doctor: '${pattern}' AUSENTE"
  fi
done

# ── 6. orbit help ─────────────────────────────────────────────────────────────

_header "Cenário 6: orbit help — todos os subcomandos documentados"

HELP="$("${ORBIT_BIN}" help 2>&1)" || true
show "${HELP}"

for cmd in "quickstart" "run" "stats" "doctor" "version"; do
  if echo "${HELP}" | grep -q "${cmd}"; then
    _pass "help: subcomando '${cmd}' documentado"
  else
    _fail "help: subcomando '${cmd}' AUSENTE"
  fi
done

if echo "${HELP}" | grep -q "\-\-json"; then
  _pass "help: flag --json documentada"
else
  _fail "help: flag --json AUSENTE"
fi

# ── 7. orbit version ──────────────────────────────────────────────────────────

_header "Cenário 7: orbit version — formato esperado"

VERSION="$("${ORBIT_BIN}" version 2>&1)"
show "${VERSION}"

if echo "${VERSION}" | grep -qE "orbit version .+ \(commit="; then
  _pass "version: formato 'orbit version X (commit=Y)' presente"
else
  _fail "version: formato AUSENTE — output: ${VERSION}"
fi

# ── 8. NO_COLOR desativa ANSI ─────────────────────────────────────────────────

_header "Cenário 8: NO_COLOR=1 → sem escape sequences ANSI"

NOCOLOR_OUT="$(NO_COLOR=1 "${ORBIT_BIN}" run echo colortest 2>&1)"
show "${NOCOLOR_OUT}"

# Verifica ausência de ESC[ (\x1b\x5b) no output via python3.
if echo "${NOCOLOR_OUT}" | python3 -c "
import sys
data = sys.stdin.read()
if '\x1b[' in data:
    sys.exit(1)
" 2>/dev/null; then
  _pass "NO_COLOR=1: output sem sequências ANSI"
else
  _fail "NO_COLOR: sequências ANSI ainda presentes no output"
fi

# ── Resultado ─────────────────────────────────────────────────────────────────

echo ""
echo "$(printf '─%.0s' {1..55})"
printf '  Resultado: %d ✅ passou   %d ❌ falhou\n' "${PASS}" "${FAIL}"
echo "$(printf '─%.0s' {1..55})"

if [[ "${FAIL}" -gt 0 ]]; then
  echo ""
  echo "❌  test_cli_output.sh FALHOU — ${FAIL} verificação(ões) não passou(aram)"
  exit 1
fi

echo ""
echo "✅  test_cli_output.sh PASSOU — qualidade de output validada"
echo ""
