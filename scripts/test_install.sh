#!/usr/bin/env bash
# scripts/test_install.sh — Teste automatizado da instalação local do orbit.
#
# Valida o fluxo completo de instalação:
#   1. Executa install.sh --prefix /tmp/orbit-test-install-$$
#   2. Verifica que o binário existe e é executável
#   3. Verifica que `orbit version` responde corretamente
#   4. Verifica que `orbit quickstart` executa com exit 0
#   5. Verifica que `orbit stats --host ...` falha com mensagem clara (sem servidor)
#   6. Limpeza do diretório temporário de instalação
#
# Fail-closed: qualquer falha aborta com exit 1.
#
# Uso:
#   ./scripts/test_install.sh
#   ./scripts/test_install.sh --verbose
set -euo pipefail

# ── Funções ──────────────────────────────────────────────────────────────────

PASS=0
FAIL=0
VERBOSE=0

_pass()  { PASS=$((PASS+1)); printf '  ✅  %s\n' "$1"; }
_fail()  { FAIL=$((FAIL+1)); printf '  ❌  %s\n' "$1" >&2; }
_abort() { printf '\n❌  FATAL: %s\n' "$1" >&2; exit 1; }
_header(){ printf '\n%s\n%s\n' "$1" "$(printf '─%.0s' {1..50})"; }

# ── Argumentos ───────────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
  case "$1" in
    --verbose|-v) VERBOSE=1; shift ;;
    *) _abort "argumento desconhecido: $1" ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_PREFIX="/tmp/orbit-test-install-$$"
ORBIT_BIN="${INSTALL_PREFIX}/orbit"

# Limpeza garantida ao sair (sucesso ou falha).
trap 'rm -rf "${INSTALL_PREFIX}"' EXIT

# ── Etapa 1 — Executar install.sh ────────────────────────────────────────────

_header "TEST orbit install — instalação local sem dependências externas"

echo "[install] Executando install.sh --prefix ${INSTALL_PREFIX}..."

INSTALL_OUTPUT="$("${REPO_ROOT}/scripts/install.sh" --prefix "${INSTALL_PREFIX}" 2>&1)" || {
  echo "${INSTALL_OUTPUT}"
  _abort "install.sh retornou exit code != 0"
}

if [[ "${VERBOSE}" -eq 1 ]]; then
  echo "${INSTALL_OUTPUT}"
fi
echo "[install] ✓  install.sh concluído"

# ── Verificações ─────────────────────────────────────────────────────────────

_header "Verificações"

# 2.1 — Binário existe
if [[ -f "${ORBIT_BIN}" ]]; then
  _pass "binário existe em ${ORBIT_BIN}"
else
  _fail "binário NÃO encontrado em ${ORBIT_BIN}"
fi

# 2.2 — Binário é executável
if [[ -x "${ORBIT_BIN}" ]]; then
  _pass "binário é executável (chmod +x)"
else
  _fail "binário NÃO é executável"
fi

# 2.3 — orbit version responde
VERSION_OUT="$("${ORBIT_BIN}" version 2>&1)" || true
if echo "${VERSION_OUT}" | grep -q "orbit version"; then
  _pass "'orbit version' responde: ${VERSION_OUT}"
else
  _fail "'orbit version' não retornou texto esperado: ${VERSION_OUT}"
fi

# 2.4 — orbit version contém commit
if echo "${VERSION_OUT}" | grep -q "commit="; then
  _pass "'orbit version' contém commit SHA"
else
  _fail "'orbit version' não contém commit SHA"
fi

# 2.5 — install.sh output confirma sucesso
if echo "${INSTALL_OUTPUT}" | grep -q "instalado com sucesso"; then
  _pass "install.sh emitiu mensagem de sucesso"
else
  _fail "mensagem de sucesso do install.sh AUSENTE"
fi

# 2.6 — orbit quickstart (servidor embutido) — exit 0
echo ""
echo "[quickstart] Executando orbit quickstart..."
QS_OUTPUT="$("${ORBIT_BIN}" quickstart 2>&1)" && QS_EXIT=0 || QS_EXIT=$?

if [[ "${VERBOSE}" -eq 1 ]]; then
  echo "${QS_OUTPUT}"
fi

if [[ "${QS_EXIT}" -eq 0 ]]; then
  _pass "orbit quickstart saiu com exit 0"
else
  _fail "orbit quickstart saiu com exit ${QS_EXIT}"
  if [[ "${VERBOSE}" -eq 0 ]]; then
    echo "    output: ${QS_OUTPUT}" >&2
  fi
fi

# 2.7 — quickstart contém mensagem de conclusão
if echo "${QS_OUTPUT}" | grep -q "Quickstart concluído"; then
  _pass "orbit quickstart: mensagem de conclusão presente"
else
  _fail "orbit quickstart: mensagem de conclusão AUSENTE"
fi

# 2.8 — quickstart validou a proof
if echo "${QS_OUTPUT}" | grep -q "proof válido"; then
  _pass "orbit quickstart: proof verificado"
else
  _fail "orbit quickstart: verificação de proof AUSENTE"
fi

# 2.9 — orbit stats sem servidor ativo deve falhar com mensagem clara
echo ""
echo "[stats] Testando orbit stats sem servidor ativo..."
STATS_OUT="$("${ORBIT_BIN}" stats --host "http://127.0.0.1:19999" 2>&1)" && STATS_EXIT=0 || STATS_EXIT=$?

if [[ "${STATS_EXIT}" -ne 0 ]]; then
  _pass "orbit stats falhou corretamente sem servidor (exit ${STATS_EXIT})"
else
  _fail "orbit stats deveria ter falhado sem servidor ativo"
fi

if echo "${STATS_OUT}" | grep -qi "não foi possível\|connection refused\|ERRO"; then
  _pass "orbit stats exibiu mensagem de erro clara"
else
  _fail "orbit stats: mensagem de erro AUSENTE ou genérica"
fi

# 2.10 — orbit help não causa panic
HELP_OUT="$("${ORBIT_BIN}" help 2>&1)" && HELP_EXIT=0 || HELP_EXIT=$?
if echo "${HELP_OUT}" | grep -q "quickstart"; then
  _pass "orbit help exibe comandos disponíveis"
else
  _fail "orbit help: lista de comandos AUSENTE"
fi

# ── Resultado ────────────────────────────────────────────────────────────────

echo ""
echo "────────────────────────────────────────────────────"
printf '  Resultado: %d ✅ passou   %d ❌ falhou\n' "${PASS}" "${FAIL}"
echo "────────────────────────────────────────────────────"

if [[ "${FAIL}" -gt 0 ]]; then
  echo ""
  echo "❌  test_install.sh FALHOU — ${FAIL} verificação(ões) não passou(aram)"
  exit 1
fi

echo ""
echo "✅  test_install.sh PASSOU — instalação local validada"
echo ""
