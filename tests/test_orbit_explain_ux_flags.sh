#!/usr/bin/env bash
# tests/test_orbit_explain_ux_flags.sh
#
# Cobre o que orbit_explain oferece como ponto de partida de investigacao:
#   case 1: --help imprime uso e sai 0
#   case 2: sem args imprime uso e sai 2 (fricao tradicional de CLI)
#   case 3: --list sem filtro lista todas as sessoes
#   case 4: --list --since <ISO> filtra por ultimo_ts >= since
#   case 5: --list --since --> corta sessoes antigas
#   case 6: --list --since sem valor falha (fail-closed)
#   case 7: --list com flag desconhecida falha

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

h() {
    printf '%s|%s|%s' "$1" "$2" "$3" | python3 -c '
import hashlib, sys
print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())'
}

mk_entry() {
    local sid="$1" ts="$2" tok="$3"
    local hash
    hash="$(h "$sid" "$ts" "$tok")"
    python3 -c "
import json
e = {
    'schema_version': 1,'written_at':'$ts','action':'track',
    'session_id':'$sid','event_type':'skill_activate','timestamp':'$ts',
    'mode':'auto','impact_estimated_tokens':$tok,
    'skill_event_hash':'$hash','server_event_id':'','git_head':'','git_repo':'',
    'payload':{},
}
print(json.dumps(e, separators=(',',':'), sort_keys=True))
"
}

pass() { echo "PASS  $1"; }
fail_test() { echo "FAIL  $1" >&2; exit 1; }

# Preenche 3 sessoes em janelas temporais distintas
mk_entry "sess-antiga"  "2026-01-01T12:00:00.000Z" 0   >  "$LEDGER"
mk_entry "sess-abril"   "2026-04-10T08:00:00.000Z" 100 >> "$LEDGER"
mk_entry "sess-hoje"    "2026-04-17T14:00:00.000Z" 200 >> "$LEDGER"

# ---------------------------------------------------------------------------
# case 1: --help sai 0 e imprime uso
# ---------------------------------------------------------------------------
OUT="$("$EXPLAIN" --help 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 1: --help retornou $RC (esperado 0)"
echo "$OUT" | grep -q "uso:" || fail_test "case 1: --help sem secao 'uso:'"
echo "$OUT" | grep -q -- "--since" || fail_test "case 1: --help nao menciona --since"
pass "case 1: --help imprime uso e sai 0"

# ---------------------------------------------------------------------------
# case 2: sem args imprime uso e sai 2
# ---------------------------------------------------------------------------
OUT="$("$EXPLAIN" 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 2: sem args retornou $RC (esperado 2)"
echo "$OUT" | grep -q "uso:" || fail_test "case 2: sem args sem 'uso:'"
pass "case 2: sem args imprime uso e sai 2"

# ---------------------------------------------------------------------------
# case 3: --list lista todas as 3 sessoes
# ---------------------------------------------------------------------------
OUT="$("$EXPLAIN" --list 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 3: --list retornou $RC"
for sid in sess-antiga sess-abril sess-hoje; do
    echo "$OUT" | grep -q "$sid" || fail_test "case 3: --list nao mostrou $sid"
done
echo "$OUT" | grep -q "total: 3 sessões" || fail_test "case 3: total incorreto"
pass "case 3: --list sem filtro mostra as 3 sessões"

# ---------------------------------------------------------------------------
# case 4: --list --since 2026-04-01 mostra abril + hoje, corta antiga
# ---------------------------------------------------------------------------
OUT="$("$EXPLAIN" --list --since "2026-04-01T00:00:00Z" 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 4: --since retornou $RC"
echo "$OUT" | grep -q "sess-antiga" && fail_test "case 4: deveria cortar sess-antiga"
echo "$OUT" | grep -q "sess-abril" || fail_test "case 4: deveria manter sess-abril"
echo "$OUT" | grep -q "sess-hoje"  || fail_test "case 4: deveria manter sess-hoje"
echo "$OUT" | grep -q "total: 2 sessões" || fail_test "case 4: total incorreto (esperado 2)"
pass "case 4: --since 2026-04-01 → 2 sessões"

# ---------------------------------------------------------------------------
# case 5: --since no futuro → zero sessoes, exit 0 com mensagem explicita
# ---------------------------------------------------------------------------
OUT="$("$EXPLAIN" --list --since "2099-01-01T00:00:00Z" 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 5: --since futuro retornou $RC"
echo "$OUT" | grep -q "nenhuma sessão" || fail_test "case 5: sem mensagem 'nenhuma sessão'"
pass "case 5: --since futuro → zero sessões, mensagem clara"

# ---------------------------------------------------------------------------
# case 6: --since sem valor → fail-closed (exit 2)
# ---------------------------------------------------------------------------
OUT="$("$EXPLAIN" --list --since 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 6: --since vazio retornou $RC (esperado 2)"
pass "case 6: --since sem valor → exit 2"

# ---------------------------------------------------------------------------
# case 7: --list <flag-desconhecida> → fail-closed
# ---------------------------------------------------------------------------
OUT="$("$EXPLAIN" --list --banana 2026 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 7: flag desconhecida retornou $RC (esperado 2)"
pass "case 7: --list --banana → exit 2"

echo ""
echo "OK: orbit_explain UX flags comportam conforme esperado"
exit 0
