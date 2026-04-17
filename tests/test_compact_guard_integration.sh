#!/usr/bin/env bash
# tests/test_compact_guard_integration.sh
#
# Valida a integração automática do compact_guard ao runtime (Python facade
# orchestrator/compact_guard.py + script scripts/orbit_compact_guard.sh).
#
# O fluxo automático — sem intervenção manual — é:
#   1. snapshot(session_id, current_task, objective) antes da chamada
#   2. detect(output) identifica "Compacted"
#   3. rehydrate(session_id) reconstitui contexto
#   4. session_id divergente → fail-closed
#   5. snapshot ausente → fail-closed
#
# Casos:
#   case 1: session_id persiste no snapshot e aparece no rehydrate
#   case 2: rehydrate com --expect-session-id correto → exit 0
#   case 3: rehydrate com --expect-session-id divergente → exit 2 (fail-closed)
#   case 4: facade Python cumpre ciclo completo sem intervenção manual
#   case 5: facade Python fail-closed em session_id divergente
#   case 6: facade Python fail-closed quando snapshot ausente
#   case 7: detect=False quando output não contém "Compacted" (sem rehydrate)

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
GUARD="$REPO_DIR/scripts/orbit_compact_guard.sh"
FACADE="$REPO_DIR/orchestrator/compact_guard.py"

[ -x "$GUARD" ]  || { echo "FAIL: $GUARD nao executavel" >&2; exit 1; }
[ -f "$FACADE" ] || { echo "FAIL: $FACADE ausente" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export ORBIT_HOME="$TMP"
export PYTHONPATH="$REPO_DIR:${PYTHONPATH:-}"
SNAPSHOT="$TMP/compact_snapshot.json"
LOG="$TMP/compact_guard.jsonl"

pass() { echo "PASS  $1"; }
fail_test() { echo "FAIL  $1" >&2; exit 1; }

# ---------------------------------------------------------------------------
# case 1: session_id persiste no snapshot
# ---------------------------------------------------------------------------
"$GUARD" snapshot \
    --session-id   "sess-abc-123" \
    --current-task "compact integration" \
    --objective   "validar session_id" \
    --constraints "fail-closed" \
    --last-output "persistido" >/dev/null 2>&1 \
    || fail_test "case 1: snapshot falhou"

python3 - "$SNAPSHOT" <<'PY' || fail_test "case 1: session_id ausente/errado no snapshot"
import json, sys
s = json.load(open(sys.argv[1]))
assert s.get("session_id") == "sess-abc-123", s
PY
pass "case 1: snapshot inclui session_id"

# ---------------------------------------------------------------------------
# case 2: rehydrate com session_id correto → exit 0
# ---------------------------------------------------------------------------
OUT="$("$GUARD" rehydrate --expect-session-id "sess-abc-123" 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 2: exit $RC (esperado 0) — saida: $OUT"
echo "$OUT" | grep -q "session_id:   sess-abc-123" || fail_test "case 2: session_id ausente no output"
pass "case 2: rehydrate com session_id correto → exit 0"

# ---------------------------------------------------------------------------
# case 3: rehydrate com session_id divergente → fail-closed
# ---------------------------------------------------------------------------
: > "$LOG"
OUT="$("$GUARD" rehydrate --expect-session-id "sess-OUTRO" 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 3: exit $RC (esperado 2)"
echo "$OUT" | grep -q "session_id divergente" || fail_test "case 3: mensagem ausente"
grep -q '"reason":"session_id_mismatch"' "$LOG" || fail_test "case 3: reason session_id_mismatch ausente no log"
pass "case 3: session_id divergente → exit 2 + log session_id_mismatch"

# ---------------------------------------------------------------------------
# case 4: facade Python — ciclo completo automático
# ---------------------------------------------------------------------------
rm -f "$SNAPSHOT" "$LOG"
python3 <<'PY' || fail_test "case 4: ciclo automático falhou"
import os, sys
sys.path.insert(0, os.environ["PYTHONPATH"].split(":")[0])
from orchestrator import compact_guard

SID = "sess-auto-1"

# 1. snapshot antes da chamada
compact_guard.snapshot(
    session_id=SID,
    current_task="rodar ciclo auto",
    objective="validar integração",
    constraints="fail-closed estrito",
    last_output="pre-chamada",
)

# 2. simular output do modelo contendo marcador
output = "Previous turn Compacted by host"

# 3. detect + rehydrate automático
assert compact_guard.detect(output) is True, "detect deveria retornar True"
ctx = compact_guard.rehydrate(session_id=SID)

assert ctx["session_id"]   == SID, ctx
assert ctx["current_task"] == "rodar ciclo auto", ctx
assert ctx["objective"]    == "validar integração", ctx
assert ctx["constraints"]  == "fail-closed estrito", ctx
print("OK")
PY
grep -q '"event":"context_compacted"'  "$LOG" || fail_test "case 4: log context_compacted ausente"
grep -q '"event":"rehydration_status"' "$LOG" || fail_test "case 4: log rehydration_status ausente"
grep -q '"status":"ok"'                "$LOG" || fail_test "case 4: status ok ausente no log"
pass "case 4: facade Python cumpre ciclo completo sem intervenção"

# ---------------------------------------------------------------------------
# case 5: facade — session_id divergente → CompactGuardError
# ---------------------------------------------------------------------------
python3 <<'PY' || fail_test "case 5: mismatch não abortou"
import os, sys
sys.path.insert(0, os.environ["PYTHONPATH"].split(":")[0])
from orchestrator import compact_guard

compact_guard.snapshot(
    session_id="sess-A",
    current_task="x",
    objective="y",
)
try:
    compact_guard.rehydrate(session_id="sess-B")
except compact_guard.CompactGuardError as e:
    assert "divergente" in str(e) or "mismatch" in str(e).lower(), str(e)
    print("OK")
else:
    raise AssertionError("rehydrate não propagou CompactGuardError")
PY
pass "case 5: facade → session_id divergente lança CompactGuardError"

# ---------------------------------------------------------------------------
# case 6: facade — snapshot ausente → CompactGuardError
# ---------------------------------------------------------------------------
rm -f "$SNAPSHOT"
python3 <<'PY' || fail_test "case 6: ausência não abortou"
import os, sys
sys.path.insert(0, os.environ["PYTHONPATH"].split(":")[0])
from orchestrator import compact_guard

try:
    compact_guard.rehydrate(session_id="qualquer")
except compact_guard.CompactGuardError as e:
    assert "fail-closed" in str(e) or "ausente" in str(e), str(e)
    print("OK")
else:
    raise AssertionError("rehydrate não propagou CompactGuardError")
PY
pass "case 6: facade → snapshot ausente lança CompactGuardError"

# ---------------------------------------------------------------------------
# case 7: output sem "Compacted" → detect=False, sem rehydrate
# ---------------------------------------------------------------------------
python3 <<'PY' || fail_test "case 7: detect sem marcador retornou True"
import os, sys
sys.path.insert(0, os.environ["PYTHONPATH"].split(":")[0])
from orchestrator import compact_guard
assert compact_guard.detect("resposta normal do modelo") is False
assert compact_guard.detect("") is False
print("OK")
PY
pass "case 7: output sem marcador → detect=False (sem rehydrate)"

echo ""
echo "OK: integração automática compact_guard em 7 casos"
exit 0
