#!/usr/bin/env bash
# tests/test_orbit_intent_overrides_verify.sh
#
# Valida o comportamento de orbit_explain.sh --verify-intent-log:
#   case 1: arquivo não existe → exit 2 (fail-closed)
#   case 2: arquivo vazio → exit 0, mensagem "ÍNTEGRO"
#   case 3: cadeia válida (3 entradas) → exit 0, "ÍNTEGRO"
#   case 4: tampering simples em E1 (session_id, hash intocado) → exit 2, "CHAIN BROKEN"
#   case 5: tampering sofisticado em E1 (hash E1 atualizado) → exit 2, "CHAIN BROKEN" em E2
#   case 6: entrada legada seguida de entrada válida → exit 0, legada pulada

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
EXPLAIN="$REPO_DIR/scripts/orbit_explain.sh"

[ -x "$EXPLAIN" ] || { echo "FAIL: $EXPLAIN nao executavel" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export ORBIT_HOME="$TMP"
export ORBIT_EXPLAIN_LOCAL_ONLY=1
LEDGER="$TMP/client_ledger.jsonl"
INTENT="$TMP/active_task.intent"
OVERRIDES="$TMP/intent_overrides.jsonl"

h() {
    printf '%s|%s|%s' "$1" "$2" "$3" | python3 -c '
import hashlib, sys
print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())'
}

mk_entry() {
    local sid="$1" ts="$2" tok="$3"
    local hash; hash="$(h "$sid" "$ts" "$tok")"
    python3 -c "
import json
e = {
    'schema_version': 1, 'written_at': '$ts', 'action': 'track',
    'session_id': '$sid', 'event_type': 'skill_activate', 'timestamp': '$ts',
    'mode': 'auto', 'impact_estimated_tokens': $tok,
    'skill_event_hash': '$hash', 'server_event_id': '', 'git_head': '', 'git_repo': '',
    'payload': {},
}
print(json.dumps(e, separators=(',', ':'), sort_keys=True))
"
}

mk_intent() {
    local sid="$1" desc="$2"
    python3 -c "
import json
print(json.dumps({
    'schema_version': 1, 'session_id': '$sid',
    'description': '$desc', 'written_at': '2026-04-17T10:00:00.000Z', 'status': 'active',
}, indent=2))
" > "$INTENT"
}

trigger_bypass() {
    local sid="$1" desc="$2"
    mk_intent "$sid" "$desc"
    "$EXPLAIN" --list --ignore-intent >/dev/null 2>&1
    rm -f "$INTENT"
}

pass()      { echo "PASS  $1"; }
fail_test() { echo "FAIL  $1" >&2; exit 1; }

mk_entry "sess-A" "2026-04-17T10:00:00.000Z" 100 > "$LEDGER"

# ---------------------------------------------------------------------------
# case 1: arquivo não existe → exit 2 (fail-closed)
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
OUT="$("$EXPLAIN" --verify-intent-log 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 1: exit $RC (esperado 2)"
echo "$OUT" | grep -qi "não encontrado\|nao encontrado" || fail_test "case 1: mensagem de arquivo ausente não exibida"
pass "case 1: arquivo ausente → exit 2 com mensagem"

# ---------------------------------------------------------------------------
# case 2: arquivo vazio → exit 0, "ÍNTEGRO"
# ---------------------------------------------------------------------------
: > "$OVERRIDES"
OUT="$("$EXPLAIN" --verify-intent-log 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 2: exit $RC (esperado 0)"
echo "$OUT" | grep -qiE "ÍNTEGRO|ntegro" || fail_test "case 2: status ÍNTEGRO ausente"
pass "case 2: arquivo vazio → exit 0, ÍNTEGRO"

# ---------------------------------------------------------------------------
# case 3: cadeia válida (3 entradas) → exit 0, "ÍNTEGRO", 3 verificadas
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
trigger_bypass "sess-A" "task 1"
trigger_bypass "sess-A" "task 2"
trigger_bypass "sess-A" "task 3"
OUT="$("$EXPLAIN" --verify-intent-log 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 3: exit $RC (esperado 0)"
echo "$OUT" | grep -qiE "ÍNTEGRO|ntegro"  || fail_test "case 3: status ÍNTEGRO ausente"
echo "$OUT" | grep -q "verificadas: 3"    || fail_test "case 3: contagem de verificadas incorreta"
echo "$OUT" | grep -q "CHAIN BROKEN"      && fail_test "case 3: CHAIN BROKEN nao devia aparecer"
pass "case 3: cadeia válida (3 entradas) → exit 0, ÍNTEGRO"

# ---------------------------------------------------------------------------
# case 4: tampering simples em E1 (session_id alterado, hash não atualizado)
# ---------------------------------------------------------------------------
LOG_PATH="$OVERRIDES" python3 <<'PY'
import json, os
path  = os.environ["LOG_PATH"]
lines = open(path, encoding="utf-8").readlines()
e1 = json.loads(lines[0])
e1["session_id"] = "TAMPERED-SIMPLE"
lines[0] = json.dumps(e1, separators=(",", ":"), sort_keys=True) + "\n"
open(path, "w").writelines(lines)
PY
OUT="$("$EXPLAIN" --verify-intent-log 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 4: exit $RC (esperado 2)"
echo "$OUT" | grep -q "CHAIN BROKEN"     || fail_test "case 4: CHAIN BROKEN ausente"
echo "$OUT" | grep -q "CORROMPIDO"       || fail_test "case 4: Status CORROMPIDO ausente"
echo "$OUT" | grep -q "entrada 1"        || fail_test "case 4: numero de entrada errado"
pass "case 4: tampering simples → exit 2, CHAIN BROKEN na entrada 1"

# ---------------------------------------------------------------------------
# case 5: tampering sofisticado em E1 (hash E1 atualizado, E2.prev_hash intocado)
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
trigger_bypass "sess-A" "task X"
trigger_bypass "sess-A" "task Y"
LOG_PATH="$OVERRIDES" python3 <<'PY'
import hashlib, json, os
path  = os.environ["LOG_PATH"]
lines = open(path, encoding="utf-8").readlines()
e1 = json.loads(lines[0])
# Adulterar E1 e recalcular seu hash (cobrindo rastro em E1)
e1["session_id"] = "SOPHISTICATED-TAMPER"
entry_for_hash = {k: v for k, v in e1.items() if k != "hash"}
e1["hash"] = hashlib.sha256(
    (e1["prev_hash"] + json.dumps(entry_for_hash, separators=(",",":"), sort_keys=True)).encode()
).hexdigest()
lines[0] = json.dumps(e1, separators=(",", ":"), sort_keys=True) + "\n"
# E2.prev_hash NÃO atualizado — aponta para hash antigo de E1
open(path, "w").writelines(lines)
PY
OUT="$("$EXPLAIN" --verify-intent-log 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 5: exit $RC (esperado 2)"
echo "$OUT" | grep -q "CHAIN BROKEN"  || fail_test "case 5: CHAIN BROKEN ausente"
echo "$OUT" | grep -q "CORROMPIDO"    || fail_test "case 5: Status CORROMPIDO ausente"
echo "$OUT" | grep -q "entrada 2"     || fail_test "case 5: deveria falhar na entrada 2"
pass "case 5: tampering sofisticado → exit 2, CHAIN BROKEN na entrada 2"

# ---------------------------------------------------------------------------
# case 6: entrada legada (sem hash) seguida de entrada válida → exit 0, legada pulada
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
printf '{"event":"intent_ignored","timestamp":"2026-01-01T00:00:00.000Z","session_id":"legacy","reason":"manual_override"}\n' \
    > "$OVERRIDES"
trigger_bypass "sess-A" "task after legacy"
OUT="$("$EXPLAIN" --verify-intent-log 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 6: exit $RC (esperado 0)"
echo "$OUT" | grep -qiE "ÍNTEGRO|ntegro" || fail_test "case 6: status ÍNTEGRO ausente"
echo "$OUT" | grep -q "legadas"          || fail_test "case 6: contagem de legadas ausente"
echo "$OUT" | grep -q "verificadas: 1"   || fail_test "case 6: deve verificar 1 entrada"
pass "case 6: entrada legada pulada, nova entrada verificada → exit 0, ÍNTEGRO"

echo ""
echo "OK: orbit_explain --verify-intent-log funciona em todos os 6 casos"
exit 0
