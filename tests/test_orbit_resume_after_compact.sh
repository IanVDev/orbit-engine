#!/usr/bin/env bash
# tests/test_orbit_resume_after_compact.sh
#
# Garante que orbit_explain.sh persiste, detecta e bloqueia com intent ativo:
#   case 1:  --resume sem intent → exit 2 (fail-closed, mensagem explícita)
#   case 2:  --resume com intent válido → exit 0, exibe session_id e descrição
#   case 3:  --list com intent → INTERROMPIDA + exit 3, sem Status: OK
#   case 4:  --list sem intent → sem banner, exit 0 (sem falso positivo)
#   case 5:  <session_id> com intent → INTERROMPIDA + exit 3, sem Status: OK
#   case 6:  intent com JSON malformado → --list fail-closed (exit 2)
#   case 7:  intent com JSON malformado → --resume fail-closed (exit 2)
#   case 8:  --list --ignore-intent com intent → exit 0, tabela normal
#   case 9:  <session_id> --ignore-intent com intent → exit 0, Status: OK presente
#   case 10: --ignore-intent com intent → log gravado em intent_overrides.jsonl
#   case 11: --ignore-intent sem intent → log NÃO gravado
#   case 12: bypass não altera exit code nem tabela vs execução normal sem intent
#
# Usa ORBIT_HOME temporário e ORBIT_EXPLAIN_LOCAL_ONLY=1.

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
intent = {
    'schema_version': 1,
    'session_id': '$sid',
    'description': '$desc',
    'written_at': '2026-04-17T10:00:00.000Z',
    'status': 'active',
}
print(json.dumps(intent, indent=2))
" > "$INTENT"
}

pass() { echo "PASS  $1"; }
fail_test() { echo "FAIL  $1" >&2; exit 1; }

# Ledger com uma entrada válida para sess-intent-1
mk_entry "sess-intent-1" "2026-04-17T10:00:00.000Z" 100 > "$LEDGER"

# ---------------------------------------------------------------------------
# case 1: --resume sem intent → exit 2 com mensagem explicita
# ---------------------------------------------------------------------------
rm -f "$INTENT"
OUT="$("$EXPLAIN" --resume 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 1: --resume sem intent retornou $RC (esperado 2)"
echo "$OUT" | grep -qiE "nenhum intent|nada a retomar" || fail_test "case 1: mensagem explicita ausente"
pass "case 1: --resume sem intent → exit 2 com mensagem"

# ---------------------------------------------------------------------------
# case 2: --resume com intent válido → exit 0, exibe task
# ---------------------------------------------------------------------------
mk_intent "sess-intent-1" "implementar validação de schema no endpoint /track"
OUT="$("$EXPLAIN" --resume 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 2: --resume com intent retornou $RC (esperado 0)"
echo "$OUT" | grep -q "sess-intent-1" || fail_test "case 2: session_id ausente no output"
echo "$OUT" | grep -q "implementar validação" || fail_test "case 2: descrição ausente no output"
pass "case 2: --resume com intent válido → exit 0 com task exibida"

# ---------------------------------------------------------------------------
# case 3: --list com intent → INTERROMPIDA + exit 3, sem Status: OK
# ---------------------------------------------------------------------------
mk_intent "sess-intent-1" "tarefa pendente de resumo"
OUT="$("$EXPLAIN" --list 2>&1)"; RC=$?
[ "$RC" = "3" ] || fail_test "case 3: --list com intent retornou $RC (esperado 3)"
echo "$OUT" | grep -q "INTENT PENDENTE"  || fail_test "case 3: banner INTENT PENDENTE ausente"
echo "$OUT" | grep -q "tarefa pendente"  || fail_test "case 3: descrição ausente no banner"
echo "$OUT" | grep -q "INTERROMPIDA"     || fail_test "case 3: mensagem INTERROMPIDA ausente"
echo "$OUT" | grep -q "Status: OK"       && fail_test "case 3: Status: OK nao devia aparecer"
pass "case 3: --list com intent → INTERROMPIDA (exit 3), sem Status: OK"

# ---------------------------------------------------------------------------
# case 4: --list sem intent → sem banner (sem falso positivo)
# ---------------------------------------------------------------------------
rm -f "$INTENT"
OUT="$("$EXPLAIN" --list 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 4: --list sem intent retornou $RC (esperado 0)"
echo "$OUT" | grep -q "INTENT PENDENTE" && fail_test "case 4: banner INTENT PENDENTE nao devia aparecer"
pass "case 4: --list sem intent → sem banner"

# ---------------------------------------------------------------------------
# case 5: <session_id> com intent → INTERROMPIDA + exit 3, sem Status: OK
# ---------------------------------------------------------------------------
mk_intent "sess-intent-1" "task no detalhe de sessao"
OUT="$("$EXPLAIN" sess-intent-1 2>&1)"; RC=$?
[ "$RC" = "3" ] || fail_test "case 5: <session_id> com intent retornou $RC (esperado 3)"
echo "$OUT" | grep -q "INTENT PENDENTE" || fail_test "case 5: banner ausente no output de detalhe"
echo "$OUT" | grep -q "INTERROMPIDA"    || fail_test "case 5: mensagem INTERROMPIDA ausente"
echo "$OUT" | grep -q "Status: OK"      && fail_test "case 5: Status: OK nao devia aparecer"
rm -f "$INTENT"
pass "case 5: <session_id> com intent → INTERROMPIDA (exit 3), sem Status: OK"

# ---------------------------------------------------------------------------
# case 6: intent com JSON malformado → --list fail-closed (exit 2)
# ---------------------------------------------------------------------------
echo '{isso nao e json valido' > "$INTENT"
OUT="$("$EXPLAIN" --list 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 6: intent malformado → --list retornou $RC (esperado 2)"
pass "case 6: intent malformado → --list exit 2"

# ---------------------------------------------------------------------------
# case 7: intent com JSON malformado → --resume fail-closed (exit 2)
# ---------------------------------------------------------------------------
echo '{isso nao e json valido' > "$INTENT"
OUT="$("$EXPLAIN" --resume 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 7: intent malformado → --resume retornou $RC (esperado 2)"
pass "case 7: intent malformado → --resume exit 2"

# ---------------------------------------------------------------------------
# case 8: --list --ignore-intent com intent → exit 0, tabela normal
# ---------------------------------------------------------------------------
mk_intent "sess-intent-1" "task para ignorar no list"
OUT="$("$EXPLAIN" --list --ignore-intent 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 8: --ignore-intent retornou $RC (esperado 0)"
echo "$OUT" | grep -q "sess-intent-1" || fail_test "case 8: tabela ausente com --ignore-intent"
echo "$OUT" | grep -q "INTERROMPIDA"  && fail_test "case 8: INTERROMPIDA nao devia aparecer"
rm -f "$INTENT"
pass "case 8: --list --ignore-intent → exit 0, tabela normal"

# ---------------------------------------------------------------------------
# case 9: <session_id> --ignore-intent com intent → exit 0, Status: OK presente
# ---------------------------------------------------------------------------
mk_intent "sess-intent-1" "task para ignorar no detalhe"
OUT="$("$EXPLAIN" --ignore-intent sess-intent-1 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 9: --ignore-intent+<session_id> retornou $RC (esperado 0)"
echo "$OUT" | grep -q "Status: OK"   || fail_test "case 9: Status: OK ausente com --ignore-intent"
echo "$OUT" | grep -q "INTERROMPIDA" && fail_test "case 9: INTERROMPIDA nao devia aparecer"
rm -f "$INTENT"
pass "case 9: <session_id> --ignore-intent → exit 0, Status: OK presente"

# ---------------------------------------------------------------------------
# case 10: --ignore-intent com intent existente → log gravado + aviso no output
# ---------------------------------------------------------------------------
OVERRIDES="$TMP/intent_overrides.jsonl"
rm -f "$OVERRIDES"
mk_intent "sess-intent-1" "task para rastrear bypass"
OUT="$("$EXPLAIN" --list --ignore-intent 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 10: exit $RC (esperado 0)"
echo "$OUT" | grep -q "intent ignorado manualmente" || fail_test "case 10: aviso de bypass ausente no output"
[ -f "$OVERRIDES" ] || fail_test "case 10: intent_overrides.jsonl nao criado"
python3 - "$OVERRIDES" <<'PY' || fail_test "case 10: log estruturado invalido"
import json, sys
entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
assert entries, "nenhuma entrada no log"
e = entries[-1]
assert e.get("event") == "intent_ignored", f"event errado: {e}"
assert e.get("reason") == "manual_override", f"reason errado: {e}"
assert "timestamp" in e, "timestamp ausente"
assert e.get("session_id") == "sess-intent-1", f"session_id errado: {e}"
PY
rm -f "$INTENT"
pass "case 10: --ignore-intent + intent → log gravado, aviso no output"

# ---------------------------------------------------------------------------
# case 11: --ignore-intent sem intent existente → log NÃO gravado
# ---------------------------------------------------------------------------
rm -f "$INTENT" "$OVERRIDES"
"$EXPLAIN" --list --ignore-intent >/dev/null 2>&1
[ ! -f "$OVERRIDES" ] || fail_test "case 11: log nao devia ser criado sem intent ativo"
pass "case 11: --ignore-intent sem intent → log não gerado"

# ---------------------------------------------------------------------------
# case 12: bypass não altera exit code nem tabela vs execução normal sem intent
# ---------------------------------------------------------------------------
mk_intent "sess-intent-1" "task qualquer"
OUT_BYPASS="$("$EXPLAIN" --list --ignore-intent 2>&1)"; RC_BYPASS=$?
rm -f "$INTENT"
OUT_CLEAN="$("$EXPLAIN" --list 2>&1)"; RC_CLEAN=$?
[ "$RC_BYPASS" = "0" ] || fail_test "case 12: --ignore-intent exit $RC_BYPASS (esperado 0)"
[ "$RC_CLEAN"  = "0" ] || fail_test "case 12: sem intent exit $RC_CLEAN (esperado 0)"
echo "$OUT_BYPASS" | grep -q "sess-intent-1" || fail_test "case 12: tabela ausente com --ignore-intent"
echo "$OUT_CLEAN"  | grep -q "sess-intent-1" || fail_test "case 12: tabela ausente sem intent"
pass "case 12: bypass mantém exit 0 e tabela idêntica à execução limpa"

echo ""
echo "OK: orbit_explain enforcement + rastreabilidade de bypass em 12 casos"
exit 0
