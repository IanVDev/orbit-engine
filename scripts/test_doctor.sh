#!/usr/bin/env bash
# scripts/test_doctor.sh — Testa o comando orbit doctor em múltiplos cenários.
#
# Cenários validados:
#   1. Instalação limpa:  ~/.orbit/bin no PATH, único orbit → OK
#   2. Conflito de PATH:  ~/.local/bin antes de ~/.orbit/bin → WARNING
#   3. PATH duplicado:    dois orbits encontrados → WARNING
#   4. --strict com aviso → exit 1
#   5. --strict sem aviso → exit 0
#   6. orbit ausente do PATH → WARNING (nenhum orbit encontrado)
#
# Fail-closed: qualquer falha no script aborta com exit 1.
#
# Uso:
#   ./scripts/test_doctor.sh
#   ./scripts/test_doctor.sh --verbose
set -euo pipefail

# ── Funções ──────────────────────────────────────────────────────────────────

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
ORBIT_BIN="/tmp/orbit-doctor-test-$$"
HOME_TMP="/tmp/orbit-doctor-home-$$"

# Limpeza garantida ao sair.
trap 'rm -rf "${ORBIT_BIN}" "${HOME_TMP}"' EXIT

# ── Build ─────────────────────────────────────────────────────────────────────

_header "TEST orbit doctor"

echo "[build] Compilando orbit CLI..."
(cd "${TRACKING_DIR}" && go build -o "${ORBIT_BIN}" ./cmd/orbit/) \
  || _abort "go build falhou"
echo "[build] ✓  binário em ${ORBIT_BIN}"

# Cria um "orbit" falso para simular conflito de PATH.
mkdir -p "${HOME_TMP}/.orbit/bin"
mkdir -p "${HOME_TMP}/.local/bin"
# Cria fake orbit em .local/bin para testar conflito
cat > "${HOME_TMP}/.local/bin/orbit" <<'EOF'
#!/usr/bin/env sh
echo "fake orbit v0"
EOF
chmod +x "${HOME_TMP}/.local/bin/orbit"

# Cria fake orbit em .orbit/bin para testar instalação correta
cp "${ORBIT_BIN}" "${HOME_TMP}/.orbit/bin/orbit"

# ── Função auxiliar para rodar doctor com PATH controlado ─────────────────────

run_doctor() {
  local extra_flags="${1:-}"
  local path_override="${2:-}"
  local home_override="${3:-}"

  local env_home="${home_override:-${HOME}}"
  local env_path="${path_override:-${PATH}}"

  # Suprime o log do pacote security (informativo, não é erro de doctor).
  HOME="${env_home}" PATH="${env_path}" \
    "${ORBIT_BIN}" doctor ${extra_flags} 2>&1 \
    | grep -v "^\(2026\|SECURITY\)" || true
}

show_output() {
  local output="$1"
  if [[ "${VERBOSE}" -eq 1 ]]; then
    echo "${output}" | sed 's/^/    | /'
    _sep
  fi
}

# ── Cenário 1 — Instalação limpa ──────────────────────────────────────────────

_header "Cenário 1: ~/.orbit/bin único e primeiro no PATH"

CLEAN_PATH="${HOME_TMP}/.orbit/bin:/usr/bin:/bin"
OUT1="$(run_doctor "" "${CLEAN_PATH}" "${HOME_TMP}")"
show_output "${OUT1}"

if echo "${OUT1}" | grep -q "Tudo OK"; then
  _pass "cenário 1: status 'Tudo OK'"
else
  _fail "cenário 1: 'Tudo OK' AUSENTE"
fi

if echo "${OUT1}" | grep -q "1 (sem conflito)"; then
  _pass "cenário 1: binários únicos detectados"
else
  _fail "cenário 1: contagem de binários AUSENTE"
fi

if echo "${OUT1}" | grep -qE "~/.orbit/bin no PATH.*posição"; then
  _pass "cenário 1: ~/.orbit/bin no PATH confirmado"
else
  _fail "cenário 1: posição de ~/.orbit/bin AUSENTE"
fi

# ── Cenário 2 — ~/.local/bin antes de ~/.orbit/bin ───────────────────────────

_header "Cenário 2: ~/.local/bin ANTES de ~/.orbit/bin (conflito de ordem)"

INVERTED_PATH="${HOME_TMP}/.local/bin:${HOME_TMP}/.orbit/bin:/usr/local/bin:/usr/bin:/bin"
OUT2="$(run_doctor "" "${INVERTED_PATH}" "${HOME_TMP}")"
show_output "${OUT2}"

if echo "${OUT2}" | grep -q "INVERTIDO"; then
  _pass "cenário 2: conflito de ordem detectado (INVERTIDO)"
else
  _fail "cenário 2: conflito de ordem NÃO detectado"
fi

if echo "${OUT2}" | grep -q "aviso"; then
  _pass "cenário 2: aviso emitido"
else
  _fail "cenário 2: aviso AUSENTE"
fi

# ── Cenário 3 — Dois orbits no PATH ──────────────────────────────────────────

_header "Cenário 3: dois orbits no PATH (conflito de duplicata)"

DUAL_PATH="${HOME_TMP}/.orbit/bin:${HOME_TMP}/.local/bin:/usr/local/bin:/usr/bin:/bin"
OUT3="$(run_doctor "" "${DUAL_PATH}" "${HOME_TMP}")"
show_output "${OUT3}"

if echo "${OUT3}" | grep -qE "[2-9] encontrados|conflito"; then
  _pass "cenário 3: duplicata detectada"
else
  _fail "cenário 3: duplicata NÃO detectada"
fi

if echo "${OUT3}" | grep -q "aviso"; then
  _pass "cenário 3: aviso emitido para duplicata"
else
  _fail "cenário 3: aviso AUSENTE para duplicata"
fi

# ── Cenário 4 — --strict com aviso → exit 1 ──────────────────────────────────

_header "Cenário 4: --strict deve falhar com exit 1 quando há avisos"

STRICT_PATH="${HOME_TMP}/.local/bin:${HOME_TMP}/.orbit/bin:/usr/bin:/bin"
STRICT_EXIT=0
HOME="${HOME_TMP}" PATH="${STRICT_PATH}" "${ORBIT_BIN}" doctor --strict 2>&1 \
  | grep -v "^\(2026\|SECURITY\)" > /dev/null || STRICT_EXIT=$?

if [[ "${STRICT_EXIT}" -ne 0 ]]; then
  _pass "cenário 4: --strict retornou exit ${STRICT_EXIT} (fail-closed)"
else
  _fail "cenário 4: --strict deveria ter retornado exit != 0 com avisos presentes"
fi

# ── Cenário 5 — --strict sem avisos → exit 0 ─────────────────────────────────

_header "Cenário 5: --strict sem avisos deve retornar exit 0"

OK_PATH="${HOME_TMP}/.orbit/bin:/usr/bin:/bin"
STRICT_OK_EXIT=0
HOME="${HOME_TMP}" PATH="${OK_PATH}" "${ORBIT_BIN}" doctor --strict 2>&1 \
  | grep -v "^\(2026\|SECURITY\)" > /dev/null || STRICT_OK_EXIT=$?

if [[ "${STRICT_OK_EXIT}" -eq 0 ]]; then
  _pass "cenário 5: --strict retornou exit 0 (instalação limpa)"
else
  _fail "cenário 5: --strict retornou exit ${STRICT_OK_EXIT} inesperado"
fi

# ── Cenário 6 — orbit ausente do PATH ────────────────────────────────────────

_header "Cenário 6: orbit ausente do PATH → WARNING"

EMPTY_PATH="/usr/bin:/bin"
OUT6="$(run_doctor "" "${EMPTY_PATH}" "${HOME_TMP}")"
show_output "${OUT6}"

if echo "${OUT6}" | grep -q "AUSENTE"; then
  _pass "cenário 6: ausência de ~/.orbit/bin detectada"
else
  _fail "cenário 6: ausência de ~/.orbit/bin NÃO detectada"
fi

if echo "${OUT6}" | grep -q "aviso"; then
  _pass "cenário 6: aviso emitido quando orbit ausente"
else
  _fail "cenário 6: aviso AUSENTE quando orbit não está no PATH"
fi

# ── Cenário 7 — output de help menciona doctor ────────────────────────────────

_header "Cenário 7: orbit help menciona o subcomando doctor"

HELP_OUT="$("${ORBIT_BIN}" help 2>&1)" || true
if echo "${HELP_OUT}" | grep -q "doctor"; then
  _pass "cenário 7: 'doctor' listado em orbit help"
else
  _fail "cenário 7: 'doctor' AUSENTE em orbit help"
fi

if echo "${HELP_OUT}" | grep -q "strict"; then
  _pass "cenário 7: flag --strict documentada em orbit help"
else
  _fail "cenário 7: flag --strict AUSENTE em orbit help"
fi

# ── Resultado ────────────────────────────────────────────────────────────────

echo ""
echo "───────────────────────────────────────────────────────"
printf '  Resultado: %d ✅ passou   %d ❌ falhou\n' "${PASS}" "${FAIL}"
echo "───────────────────────────────────────────────────────"

if [[ "${FAIL}" -gt 0 ]]; then
  echo ""
  echo "❌  test_doctor.sh FALHOU — ${FAIL} verificação(ões) não passou(aram)"
  exit 1
fi

echo ""
echo "✅  test_doctor.sh PASSOU — orbit doctor validado em todos os cenários"
echo ""
