#!/usr/bin/env bash
# test_loop_closes.sh — teste E2E do loop orbit run → log + snapshot + guidance.
#
# Garante (refinamento do plano):
#   1. orbit run grava JSON em $ORBIT_HOME/logs/
#   2. campo `criticality` tem valor esperado por cenário
#   3. em TEST_FAIL, campo `guidance` contém string no formato "*:<num>"
#   4. em CODE_CHANGE, campo `snapshot_path` aponta para arquivo existente
#
# Fail-closed: qualquer assert que falhar aborta com exit != 0.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$(mktemp -d)"
ORBIT_HOME_DIR="$(mktemp -d)"
export ORBIT_HOME="$ORBIT_HOME_DIR"
# Binário buildado sem ldflags → startup guard aborta. Bypass explícito
# para testes E2E, coerente com o fluxo documentado pelo próprio guard.
export ORBIT_SKIP_GUARD=1

cleanup() {
  rm -rf "$BIN_DIR" "$ORBIT_HOME_DIR"
}
trap cleanup EXIT

echo "== build orbit ==" >&2
( cd "$REPO_ROOT/tracking" && go build -o "$BIN_DIR/orbit" ./cmd/orbit )

ORBIT="$BIN_DIR/orbit"

require_jq() {
  if ! command -v jq >/dev/null 2>&1; then
    echo "FAIL: jq é requerido para este teste" >&2
    exit 1
  fi
}
require_jq

assert_field() {
  local file="$1" field="$2" want="$3"
  local got
  got="$(jq -r "$field" "$file")"
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $file campo $field = '$got', esperado '$want'" >&2
    exit 1
  fi
}

assert_regex() {
  local file="$1" field="$2" pattern="$3"
  local got
  got="$(jq -r "$field" "$file")"
  if [[ ! "$got" =~ $pattern ]]; then
    echo "FAIL: $file campo $field = '$got' não casa com regex '$pattern'" >&2
    exit 1
  fi
}

latest_log() {
  ls -t "$ORBIT_HOME/logs/"*.json 2>/dev/null | head -n 1
}

# ---------------------------------------------------------------------------
# Cenário 1: CODE_CHANGE (git commit em repo git válido)
# ---------------------------------------------------------------------------
echo "== cenário 1: CODE_CHANGE → low + snapshot ==" >&2
TMPREPO="$(mktemp -d)"
(
  cd "$TMPREPO"
  git init -q
  git config user.email t@t
  git config user.name t
  git commit -q --allow-empty -m "initial" || true
)

(
  cd "$TMPREPO"
  "$ORBIT" run git commit --allow-empty -m "loop-test" >/dev/null 2>&1 || true
)

LOG1="$(latest_log)"
if [[ -z "$LOG1" ]]; then
  echo "FAIL: nenhum log criado em $ORBIT_HOME/logs/ após git commit" >&2
  exit 1
fi
echo "log: $LOG1" >&2

assert_field "$LOG1" ".event" "CODE_CHANGE"
assert_field "$LOG1" ".decision" "TRIGGER_SNAPSHOT"
assert_field "$LOG1" ".criticality" "low"

SNAP_PATH="$(jq -r '.snapshot_path' "$LOG1")"
if [[ "$SNAP_PATH" == "" || "$SNAP_PATH" == "null" ]]; then
  echo "FAIL: snapshot_path vazio após CODE_CHANGE" >&2
  exit 1
fi
if [[ ! -f "$SNAP_PATH" ]]; then
  echo "FAIL: snapshot_path $SNAP_PATH não existe em disco" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Cenário 2: TEST_FAIL (go test em código propositalmente quebrado)
# ---------------------------------------------------------------------------
echo "== cenário 2: TEST_FAIL → medium + guidance com file:line ==" >&2
TESTMOD="$(mktemp -d)"
(
  cd "$TESTMOD"
  cat > go.mod <<EOF
module example.com/failtest

go 1.21
EOF
  cat > x_test.go <<'EOF'
package failtest

import "testing"

func TestFails(t *testing.T) {
  t.Errorf("falha proposital")
}
EOF
)

(
  cd "$TESTMOD"
  "$ORBIT" run go test ./... >/dev/null 2>&1 || true
)

# Pegar log mais recente, mas que seja TEST_RUN (não o CODE_CHANGE anterior)
LOG2="$(jq -l --arg e TEST_RUN 'select(.event==$e)' "$ORBIT_HOME/logs/"*.json 2>/dev/null | head -n1 || true)"
# Alternativa simples: último arquivo é o mais recente
LOG2="$(latest_log)"
EVT2="$(jq -r '.event' "$LOG2")"
if [[ "$EVT2" != "TEST_RUN" ]]; then
  echo "FAIL: último log esperado TEST_RUN, got $EVT2 ($LOG2)" >&2
  exit 1
fi
echo "log: $LOG2" >&2

assert_field "$LOG2" ".decision" "TRIGGER_ANALYZE"
assert_field "$LOG2" ".criticality" "medium"
# guidance deve conter "arquivo:número" — regex tolerante
assert_regex  "$LOG2" ".guidance" "^[A-Za-z0-9_./\\\\-]+\.[A-Za-z0-9]+:[0-9]+$"

echo "== OK: loop fechado (CODE_CHANGE + TEST_FAIL validados) ==" >&2
