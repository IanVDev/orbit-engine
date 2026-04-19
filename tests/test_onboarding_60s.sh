#!/usr/bin/env bash
# tests/test_onboarding_60s.sh — guardião do quickstart prometido no README.
#
# Se este teste quebrar, a promessa de 60s do README não é mais verdadeira:
# o usuário que copiar/colar do README terá uma experiência diferente da
# documentada. Roda contra binário recém-buildado em ORBIT_HOME isolado.
#
# A sequência canônica testada espelha o bloco "Install (CLI binary)" +
# primeiro `orbit run` + `orbit verify` + `orbit diagnose`.
#
# Não cronometra os 60s — "60s" é promessa de copy, não de teste. O que
# este teste protege é o CONTRATO de comandos: cada passo deve sair com
# exit 0 e produzir o efeito documentado.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$(mktemp -t orbit-onboard-XXXXXX)"
ORBIT_HOME="$(mktemp -d -t orbit-home-XXXXXX)"
export ORBIT_HOME ORBIT_SKIP_GUARD=1

cleanup() { rm -f "$BIN"; rm -rf "$ORBIT_HOME"; }
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# ── 1. Build local (proxy do binário do GitHub Release) ─────────────────
echo "[1/5] build do binário..."
go -C "$REPO_ROOT/tracking" build -o "$BIN" ./cmd/orbit \
  || fail "go build falhou"

# ── 2. orbit version produz output não-vazio ─────────────────────────────
echo "[2/5] orbit version..."
"$BIN" version | grep -qE 'orbit version' \
  || fail "orbit version sem output esperado"

# ── 3. orbit run echo gera log em \$ORBIT_HOME/logs/ ─────────────────────
echo "[3/5] orbit run echo..."
"$BIN" run -- echo "hello orbit" > /dev/null 2>&1 \
  || fail "orbit run retornou erro inesperado"

LOG_COUNT="$(find "$ORBIT_HOME/logs" -name '*.json' 2>/dev/null | wc -l)"
[ "$LOG_COUNT" -ge 1 ] \
  || fail "nenhum log foi gravado em $ORBIT_HOME/logs/"

LOG="$(find "$ORBIT_HOME/logs" -name '*.json' | head -1)"

# ── 4. orbit verify do log gerado retorna sucesso ────────────────────────
echo "[4/5] orbit verify..."
"$BIN" verify "$LOG" 2>&1 | grep -qE 'proof confere|✅' \
  || fail "verify não confirma proof do log recém-criado"

# ── 5. orbit diagnose roda sem crash ─────────────────────────────────────
echo "[5/5] orbit diagnose..."
"$BIN" diagnose > /dev/null 2>&1 \
  || fail "diagnose retornou erro"

echo "PASS: onboarding íntegro (build → version → run → verify → diagnose)"
