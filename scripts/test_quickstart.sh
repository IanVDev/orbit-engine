#!/usr/bin/env bash
# scripts/test_quickstart.sh — Teste automatizado do fluxo orbit quickstart.
#
# Valida a jornada completa de onboarding:
#   1. Compila o binário orbit a partir do fonte
#   2. Executa orbit quickstart (servidor embutido)
#   3. Verifica exit code = 0
#   4. Verifica presença de mensagens de sucesso no output
#   5. Verifica que proof foi computado e validado
#   6. Verifica que event_id foi registrado
#   7. Verifica que o servidor embutido aceitou o evento (event registered)
#
# Fail-closed: qualquer falha aborta com exit 1.
#
# Uso:
#   ./scripts/test_quickstart.sh
#   ./scripts/test_quickstart.sh --verbose   # exibe output completo
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
TRACKING_DIR="${REPO_ROOT}/tracking"
TMP_BINARY="/tmp/orbit-test-qs-$$"

# ── Etapa 1 — Build ──────────────────────────────────────────────────────────

_header "TEST orbit quickstart — build + fluxo completo"

echo "[build] Compilando orbit CLI..."
if ! (cd "${TRACKING_DIR}" && go build -o "${TMP_BINARY}" ./cmd/orbit/) 2>&1; then
  _abort "go build falhou — verifique erros acima"
fi
echo "[build] ✓  binário em ${TMP_BINARY}"

# Garante remoção do binário temporário ao final.
trap 'rm -f "${TMP_BINARY}"' EXIT

# ── Etapa 2 — Executa quickstart ─────────────────────────────────────────────

echo ""
echo "[run] Executando: orbit quickstart"
echo "──────────────────────────────────────────────────"

# Captura stdout+stderr juntos; redireciona stderr para suprimir apenas o log
# de init do pacote security (informativo, não é erro).
QS_OUTPUT="$("${TMP_BINARY}" quickstart 2>&1)" || {
  echo "${QS_OUTPUT}"
  _abort "orbit quickstart retornou exit code != 0"
}

if [[ "${VERBOSE}" -eq 1 ]]; then
  echo "${QS_OUTPUT}"
fi
echo "──────────────────────────────────────────────────"

# ── Etapa 3 — Asserções sobre o output ───────────────────────────────────────

_header "Verificações"

# 3.1 — Mensagem de conclusão
if echo "${QS_OUTPUT}" | grep -q "Quickstart concluído"; then
  _pass "mensagem de conclusão presente"
else
  _fail "mensagem de conclusão AUSENTE (esperado: 'Quickstart concluído')"
fi

# 3.2 — Progresso 1/3
if echo "${QS_OUTPUT}" | grep -q "\[1/3\]"; then
  _pass "step [1/3] presente"
else
  _fail "step [1/3] AUSENTE"
fi

# 3.3 — Progresso 2/3
if echo "${QS_OUTPUT}" | grep -q "\[2/3\]"; then
  _pass "step [2/3] presente"
else
  _fail "step [2/3] AUSENTE"
fi

# 3.4 — Progresso 3/3
if echo "${QS_OUTPUT}" | grep -q "\[3/3\]"; then
  _pass "step [3/3] presente"
else
  _fail "step [3/3] AUSENTE"
fi

# 3.5 — echo hello executado
if echo "${QS_OUTPUT}" | grep -q "→ hello"; then
  _pass "'echo hello' executado e output capturado"
else
  _fail "'echo hello' output AUSENTE (esperado: '→ hello')"
fi

# 3.6 — Evento registrado com event_id
if echo "${QS_OUTPUT}" | grep -q "evento registrado"; then
  _pass "evento registrado em /track"
else
  _fail "confirmação de registro AUSENTE"
fi

# 3.7 — Proof computado
if echo "${QS_OUTPUT}" | grep -q "proof="; then
  _pass "proof computado e exibido"
else
  _fail "proof AUSENTE no output"
fi

# 3.8 — Proof verificado
if echo "${QS_OUTPUT}" | grep -q "proof válido"; then
  _pass "proof verificado com sucesso"
else
  _fail "verificação de proof AUSENTE"
fi

# 3.9 — Session ID presente
if echo "${QS_OUTPUT}" | grep -qE "session_id=qs-[0-9]+"; then
  _pass "session_id gerado com prefixo 'qs-'"
else
  _fail "session_id AUSENTE ou com formato inesperado"
fi

# 3.10 — Tokens = 42
if echo "${QS_OUTPUT}" | grep -q "tokens=42"; then
  _pass "tokens=42 registrado corretamente"
else
  _fail "tokens=42 AUSENTE no output"
fi

# 3.11 — Servidor embutido (porta efêmera)
if echo "${QS_OUTPUT}" | grep -qE "Servidor\s+: http://127\.0\.0\.1:[0-9]+"; then
  _pass "servidor embutido iniciado em porta efêmera"
else
  _fail "confirmação do servidor embutido AUSENTE"
fi

# 3.12 — Próximo passo sugerido
if echo "${QS_OUTPUT}" | grep -q "orbit stats"; then
  _pass "próximo passo 'orbit stats' sugerido"
else
  _fail "sugestão de próximo passo AUSENTE"
fi

# ── Resultado ────────────────────────────────────────────────────────────────

echo ""
echo "────────────────────────────────────────────────────"
printf '  Resultado: %d ✅ passou   %d ❌ falhou\n' "${PASS}" "${FAIL}"
echo "────────────────────────────────────────────────────"

if [[ "${FAIL}" -gt 0 ]]; then
  echo ""
  echo "❌  test_quickstart.sh FALHOU — ${FAIL} verificação(ões) não passou(aram)"
  exit 1
fi

echo ""
echo "✅  test_quickstart.sh PASSOU — fluxo de onboarding validado"
echo ""
