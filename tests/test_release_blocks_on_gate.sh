#!/usr/bin/env bash
# test_release_blocks_on_gate.sh — Garante que release_orbit.sh nunca prossegue
# sem [VERDICT] GO do prelaunch gate.
#
# Cenários:
#   1. Gate mock retorna exit 1                         → release aborta em STEP 6
#   2. Gate mock retorna exit 0, log sem [VERDICT] GO   → release aborta (defesa em profundidade)
#   3. --skip-gate + prelaunch_gate.log = NO-GO         → release aborta em STEP 6
#   4. --skip-gate + prelaunch_gate.log = GO + dry-run  → release prossegue (exit 0)
#
# Pré-requisitos (cenários 1-4):
#   - Branch main, working tree limpo, sincronizado com origin
#   - Tag v9.9.9-test inexistente (local e remote)
#   - Cenários 1-2 executam sem --dry-run: git fetch origin roda de verdade
#
# Se pré-requisitos não forem atendidos, cenários afetados são marcados SKIP.
#
# Uso:
#   ./tests/test_release_blocks_on_gate.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
RELEASE_SCRIPT="${REPO_ROOT}/scripts/release_orbit.sh"
GATE_LOG="${REPO_ROOT}/prelaunch_gate.log"
RELEASE_LOG="${REPO_ROOT}/release_orbit.log"
TEST_VERSION="v9.9.9-test"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

PASS=0
FAIL=0
SKIP=0

LAST_EXIT=0
LAST_OUTPUT=""

# ── Helpers ───────────────────────────────────────────────────────────────────

_result() {
  local name="$1" expected="$2" got="$3"
  if [[ "${expected}" == "${got}" ]]; then
    echo -e "  ${GREEN}[PASS]${NC} ${name}"
    ((PASS++)) || true
  else
    echo -e "  ${RED}[FAIL]${NC} ${name} — esperado=${expected} obtido=${got}"
    if [[ -n "${LAST_OUTPUT}" ]]; then
      echo "  Output (últimas 8 linhas):"
      echo "${LAST_OUTPUT}" | tail -8 | sed 's/^/    /'
    fi
    ((FAIL++)) || true
  fi
}

_result_not_contains() {
  local name="$1" pattern="$2"
  if ! echo "${LAST_OUTPUT}" | grep -q "${pattern}"; then
    echo -e "  ${GREEN}[PASS]${NC} ${name}"
    ((PASS++)) || true
  else
    echo -e "  ${RED}[FAIL]${NC} ${name} — padrão proibido '${pattern}' encontrado"
    ((FAIL++)) || true
  fi
}

_skip() {
  echo -e "  ${YELLOW}[SKIP]${NC} $1"
  ((SKIP++)) || true
}

_run_release() {
  # _run_release [env_var=value ...] -- [release args...]
  # Tudo antes de "--" são pares VAR=value; depois são argumentos do release script.
  local env_vars=()
  while [[ $# -gt 0 && "$1" != "--" ]]; do
    env_vars+=("$1")
    shift
  done
  [[ "${1:-}" == "--" ]] && shift

  local tmpout
  tmpout=$(mktemp /tmp/release-test-out-XXXXXX)
  local tmplog
  tmplog=$(mktemp /tmp/release-test-log-XXXXXX)

  LAST_EXIT=0
  # Redirecionar RELEASE_LOG para arquivo temporário para não poluir o log real
  ( env "${env_vars[@]}" RELEASE_LOG="${tmplog}" bash "${RELEASE_SCRIPT}" "$@" \
    >"${tmpout}" 2>&1 ) || LAST_EXIT=$?

  LAST_OUTPUT=$(cat "${tmpout}")
  rm -f "${tmpout}" "${tmplog}"
}

# Backup/restore do gate log (preserva estado pré-teste)
GATE_LOG_BAK="${GATE_LOG}.test-bak-$$"
_save_gate_log()    { [[ -f "${GATE_LOG}" ]] && cp "${GATE_LOG}" "${GATE_LOG_BAK}" || true; }
_restore_gate_log() {
  if [[ -f "${GATE_LOG_BAK}" ]]; then
    mv "${GATE_LOG_BAK}" "${GATE_LOG}"
  else
    rm -f "${GATE_LOG}"
  fi
}
trap '_restore_gate_log' EXIT

# ── Pré-condições ─────────────────────────────────────────────────────────────
#
# Todos os cenários precisam de git state válido para STEPS 1-5 do release passarem.

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}  release_orbit.sh — Gate Integration Tests${NC}"
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo ""
echo "  Verificando pré-condições..."

PRECOND_OK=1
PRECOND_MSGS=()

CURRENT_BRANCH=$(git -C "${REPO_ROOT}" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
if [[ "${CURRENT_BRANCH}" != "main" ]]; then
  PRECOND_OK=0
  PRECOND_MSGS+=("branch atual = '${CURRENT_BRANCH}' (precisa ser 'main')")
fi

DIRTY=$(git -C "${REPO_ROOT}" status --porcelain 2>/dev/null || echo "err")
if [[ -n "${DIRTY}" ]]; then
  PRECOND_OK=0
  PRECOND_MSGS+=("working tree sujo (precisa estar limpo)")
fi

# Fetch silencioso para atualizar origin/main antes de comparar SHAs
git -C "${REPO_ROOT}" fetch origin --quiet 2>/dev/null || true
LOCAL_SHA=$(git -C "${REPO_ROOT}" rev-parse HEAD 2>/dev/null || echo "local")
REMOTE_SHA=$(git -C "${REPO_ROOT}" rev-parse origin/main 2>/dev/null || echo "remote")
if [[ "${LOCAL_SHA}" != "${REMOTE_SHA}" ]]; then
  PRECOND_OK=0
  PRECOND_MSGS+=("HEAD local (${LOCAL_SHA:0:7}) != origin/main (${REMOTE_SHA:0:7}) — faça push ou pull")
fi

if git -C "${REPO_ROOT}" rev-parse "refs/tags/${TEST_VERSION}" >/dev/null 2>&1; then
  PRECOND_OK=0
  PRECOND_MSGS+=("tag ${TEST_VERSION} já existe — delete antes de rodar o teste")
fi

if [[ "${PRECOND_OK}" -eq 1 ]]; then
  echo -e "  ${GREEN}[OK]${NC} Pré-condições atendidas — todos os cenários serão executados"
else
  echo -e "  ${YELLOW}[WARN]${NC} Pré-condições não atendidas:"
  for msg in "${PRECOND_MSGS[@]}"; do
    echo -e "         • ${msg}"
  done
  echo -e "  ${YELLOW}       Todos os cenários serão SKIP${NC}"
fi

# ════════════════════════════════════════════════════════════════════════════
# Cenário 1: Gate mock retorna exit 1 → release aborta em STEP 6
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 1: Gate exits 1 → release aborta${NC}"

if [[ "${PRECOND_OK}" -ne 1 ]]; then
  _skip "pré-condições de git não atendidas"
else
  MOCK_GATE=$(mktemp /tmp/mock-gate-XXXXXX.sh)
  # O mock usa $(pwd) pois herda o CWD da instância do release script (REPO_ROOT)
  cat > "${MOCK_GATE}" <<'MOCK'
#!/usr/bin/env bash
GATE_LOG_PATH="$(pwd)/prelaunch_gate.log"
: > "${GATE_LOG_PATH}"
printf 'prelaunch_gate started at %s\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" >> "${GATE_LOG_PATH}"
printf '[FAIL] tracking server não responde\n' >> "${GATE_LOG_PATH}"
printf '[VERDICT] NO-GO\n' >> "${GATE_LOG_PATH}"
exit 1
MOCK
  chmod +x "${MOCK_GATE}"

  _save_gate_log
  _run_release "ORBIT_GATE_SCRIPT=${MOCK_GATE}" -- "${TEST_VERSION}"
  _restore_gate_log
  rm -f "${MOCK_GATE}"

  _result        "exit code != 0 (release abortado)"         "1" "$([[ $LAST_EXIT -ne 0 ]] && echo 1 || echo 0)"
  _result_not_contains "nenhum push executado"               "RELEASE.*PUBLICADO"
fi

# ════════════════════════════════════════════════════════════════════════════
# Cenário 2: Gate exits 0 mas log sem [VERDICT] GO → release aborta (defesa)
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 2: Gate exits 0, log incompleto (sem [VERDICT] GO) → release aborta${NC}"

if [[ "${PRECOND_OK}" -ne 1 ]]; then
  _skip "pré-condições de git não atendidas"
else
  MOCK_GATE=$(mktemp /tmp/mock-gate-XXXXXX.sh)
  cat > "${MOCK_GATE}" <<'MOCK'
#!/usr/bin/env bash
# Simula gate que retorna 0 mas foi interrompido antes de emitir [VERDICT] GO.
GATE_LOG_PATH="$(pwd)/prelaunch_gate.log"
: > "${GATE_LOG_PATH}"
printf 'prelaunch_gate started\n' >> "${GATE_LOG_PATH}"
printf '[PASS] tracking server responde\n' >> "${GATE_LOG_PATH}"
printf '[PASS] /metrics acessível\n' >> "${GATE_LOG_PATH}"
# Log truncado — sem [VERDICT] GO
exit 0
MOCK
  chmod +x "${MOCK_GATE}"

  _save_gate_log
  _run_release "ORBIT_GATE_SCRIPT=${MOCK_GATE}" -- "${TEST_VERSION}"
  _restore_gate_log
  rm -f "${MOCK_GATE}"

  _result        "exit code != 0 (defesa em profundidade ativada)" "1" "$([[ $LAST_EXIT -ne 0 ]] && echo 1 || echo 0)"
  _result_not_contains "nenhuma mutação executada"                 "RELEASE.*PUBLICADO"
fi

# ════════════════════════════════════════════════════════════════════════════
# Cenário 3: --skip-gate + log NO-GO → release aborta em STEP 6
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 3: --skip-gate + log NO-GO → release aborta${NC}"

if [[ "${PRECOND_OK}" -ne 1 ]]; then
  _skip "pré-condições de git não atendidas"
else
  _save_gate_log
  printf 'prelaunch_gate started\n[FAIL] métricas ausentes\n[VERDICT] NO-GO\n' > "${GATE_LOG}"

  _run_release -- --skip-gate --dry-run --yes "${TEST_VERSION}"
  _restore_gate_log

  _result        "exit code != 0 (NO-GO bloqueou release)"   "1" "$([[ $LAST_EXIT -ne 0 ]] && echo 1 || echo 0)"
  _result_not_contains "nenhuma tag criada"                   "git tag"
fi

# ════════════════════════════════════════════════════════════════════════════
# Cenário 4: --skip-gate + log GO + dry-run → release prossegue (exit 0)
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}── Cenário 4: --skip-gate + log GO + dry-run → release prossegue${NC}"

if [[ "${PRECOND_OK}" -ne 1 ]]; then
  _skip "pré-condições de git não atendidas"
else
  _save_gate_log
  printf 'prelaunch_gate started\n[PASS] tracking ok\n[PASS] /metrics ok\n[VERDICT] GO\n' > "${GATE_LOG}"

  _run_release -- --skip-gate --dry-run --yes "${TEST_VERSION}"
  _restore_gate_log

  _result        "exit code = 0 (GO permitiu prosseguir)"    "0" "$([[ $LAST_EXIT -eq 0 ]] && echo 0 || echo 1)"
fi

# ════════════════════════════════════════════════════════════════════════════
# RESULTADO FINAL
# ════════════════════════════════════════════════════════════════════════════

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  PASS: ${PASS}   FAIL: ${FAIL}   SKIP: ${SKIP}"
echo ""

if [[ "${FAIL}" -gt 0 ]]; then
  echo -e "${RED}${BOLD}  ❌ ${FAIL} teste(s) falharam — gate integration QUEBRADA.${NC}"
  echo ""
  exit 1
elif [[ "${SKIP}" -gt 0 && "${PASS}" -eq 0 ]]; then
  echo -e "${YELLOW}${BOLD}  ⚠️  Todos os cenários foram SKIP — execute em branch main limpo.${NC}"
  echo ""
  exit 0
else
  echo -e "${GREEN}${BOLD}  ✅ Gate integration confirmada — release nunca prossegue sem [VERDICT] GO.${NC}"
  echo ""
  exit 0
fi
