#!/usr/bin/env bash
# tests/test_orbit_run_parsing.sh
#
# Anti-regressão do parsing de `orbit run`:
#   Forma canônica (POSIX): cada argumento é um token próprio.
#     orbit run echo hello world   → exec("echo", ["hello","world"])  → exit 0
#
#   Forma não-canônica (single token com espaços): trata como nome único
#   de binário; falha previsivelmente com "executable not found".
#     orbit run "echo hello"       → exec("echo hello", [])          → exit != 0
#
# Este teste TRAVA o contrato POSIX. Qualquer "fix" futuro que introduza
# shell-split automático (via `sh -c` ou tokenização interna) quebra este
# teste — intencionalmente. Mudança de semântica exige nova decisão de
# produto, não commit silencioso.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TRACKING_DIR="$REPO_DIR/tracking"

command -v go >/dev/null 2>&1 || { echo "SKIP: go não instalado"; exit 0; }
[ -d "$TRACKING_DIR" ] || { echo "FAIL: $TRACKING_DIR ausente" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
BIN="$TMP/orbit"

# Build isolado — sem -ldflags, então startup-guard dispararia.
# ORBIT_SKIP_GUARD=1 é o bypass documentado e já previsto no código.
(
    cd "$TRACKING_DIR"
    go build -o "$BIN" ./cmd/orbit
) || { echo "FAIL: build falhou" >&2; exit 1; }

export ORBIT_SKIP_GUARD=1
# Ambiente limpo: ledger dedicado pra não poluir ~/.orbit do dev.
export ORBIT_HOME="$TMP/home"
mkdir -p "$ORBIT_HOME"

pass()      { echo "PASS  $1"; }
fail_test() { echo "FAIL  $1" >&2; exit 1; }

# ---------------------------------------------------------------------------
# case 1: forma canônica — cada arg é token próprio → execução bem-sucedida
# ---------------------------------------------------------------------------
OUT="$("$BIN" run echo hello world 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 1: exit $RC (esperado 0). saída: $OUT"
echo "$OUT" | grep -q "hello world" \
    || fail_test "case 1: output 'hello world' ausente. saída: $OUT"
echo "$OUT" | grep -qi "not found\|executable file" \
    && fail_test "case 1: forma canônica nao pode gerar 'not found'. saída: $OUT"
pass "case 1: 'orbit run echo hello world' → exit 0, output contém 'hello world'"

# ---------------------------------------------------------------------------
# case 2: forma não-canônica (single token) — falha previsível
# ---------------------------------------------------------------------------
OUT="$("$BIN" run "echo hello" 2>&1)"; RC=$?
[ "$RC" != "0" ] \
    || fail_test "case 2: exit 0 (esperado != 0) — shell-split indevido introduzido? saída: $OUT"
echo "$OUT" | grep -q "executable file not found\|not found in \$PATH" \
    || fail_test "case 2: erro esperado 'executable file not found' ausente. saída: $OUT"
pass "case 2: 'orbit run \"echo hello\"' → exit != 0, erro POSIX esperado (not found)"

# ---------------------------------------------------------------------------
# case 3: flag --json antes do comando não é absorvida como argumento do cmd
# ---------------------------------------------------------------------------
OUT="$("$BIN" run --json echo isolated 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 3: --json+echo exit $RC (esperado 0). saída: $OUT"
echo "$OUT" | grep -Eq '"command":\s*"echo"' \
    || fail_test "case 3: JSON nao reporta command=echo. saída: $OUT"
echo "$OUT" | grep -q '"isolated"' \
    || fail_test "case 3: JSON nao contem arg 'isolated'. saída: $OUT"
pass "case 3: '--json echo isolated' → --json consumido pelo parser, echo/isolated intactos"

echo ""
echo "OK: contrato POSIX de 'orbit run' preservado em 3 casos"
exit 0
