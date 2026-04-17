#!/usr/bin/env bash
# tests/test_orbit_signature_scope.sh
#
# Anti-regressão: a assinatura "- by Orbit" só pode aparecer quando o
# comando representa ação real do Orbit (bypass manual, proteção de
# continuidade, retomada, verificação de integridade, confirmação remota).
#
# Regras validadas:
#   - help/uso              → sem assinatura
#   - --list limpo          → sem assinatura
#   - <session_id> rotineiro→ sem assinatura
#   - --ignore-intent ativo → assinatura no override
#   - intent bloqueando     → assinatura no header de proteção
#   - --resume com intent   → assinatura no header de retomada
#   - --verify-intent-log   → assinatura no header de verificação
#   - Nenhum fluxo deve mencionar "AURYA" no terminal

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
EXPLAIN="$REPO_DIR/scripts/orbit_explain.sh"

[ -x "$EXPLAIN" ] || { echo "FAIL: $EXPLAIN nao executavel" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export ORBIT_HOME="$TMP"
export ORBIT_EXPLAIN_LOCAL_ONLY=1
unset ORBIT_AURYA_ENABLED ORBIT_VERBOSE

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

count_sig() { echo "$1" | grep -c "by Orbit" || true; }

pass()      { echo "PASS  $1"; }
fail_test() { echo "FAIL  $1" >&2; exit 1; }

# Ledger com uma sessão válida para os fluxos de --list e <session_id>.
mk_entry "sess-quiet" "2026-04-17T10:00:00.000Z" 100 > "$LEDGER"

# ---------------------------------------------------------------------------
# Fluxos silenciosos: assinatura NÃO pode aparecer.
# ---------------------------------------------------------------------------

OUT="$("$EXPLAIN" --help 2>&1)"
[ "$(count_sig "$OUT")" = "0" ] || fail_test "help não pode carregar assinatura"
echo "$OUT" | grep -qi "aurya" && fail_test "help não pode mencionar AURYA"
pass "help  → zero assinaturas, zero AURYA"

OUT="$("$EXPLAIN" --list 2>&1)"
[ "$(count_sig "$OUT")" = "0" ] || fail_test "--list limpo não pode carregar assinatura"
echo "$OUT" | grep -qi "aurya" && fail_test "--list não pode mencionar AURYA"
pass "--list limpo → zero assinaturas, zero AURYA"

OUT="$("$EXPLAIN" sess-quiet 2>&1)"
[ "$(count_sig "$OUT")" = "0" ] \
    || fail_test "verify rotineiro de <session_id> não pode carregar assinatura (saída: $OUT)"
echo "$OUT" | grep -qi "aurya" && fail_test "<session_id> não pode mencionar AURYA"
pass "<session_id> rotineiro → zero assinaturas, zero AURYA"

# ---------------------------------------------------------------------------
# Fluxos de ação real: exatamente UMA assinatura por fluxo.
# ---------------------------------------------------------------------------

# Bypass manual
mk_intent "sess-quiet" "task qualquer"
OUT="$("$EXPLAIN" --list --ignore-intent 2>&1)"
SIG_COUNT="$(count_sig "$OUT")"
[ "$SIG_COUNT" = "1" ] \
    || fail_test "--ignore-intent esperava 1 assinatura, obteve $SIG_COUNT"
echo "$OUT" | grep -q "execution override manual.* - by Orbit" \
    || fail_test "assinatura ausente na linha de override"
echo "$OUT" | grep -qi "aurya" && fail_test "bypass não pode mencionar AURYA"
rm -f "$INTENT" "$OVERRIDES"
pass "--ignore-intent → assinatura única na linha de override"

# Proteção de continuidade (intent bloqueia execução)
mk_intent "sess-quiet" "task para bloqueio"
OUT="$("$EXPLAIN" --list 2>&1)"; RC=$?
[ "$RC" = "3" ] || fail_test "bloqueio deveria exitar 3, obteve $RC"
SIG_COUNT="$(count_sig "$OUT")"
[ "$SIG_COUNT" = "1" ] \
    || fail_test "bloqueio esperava 1 assinatura, obteve $SIG_COUNT"
echo "$OUT" | grep -q "proteção de continuidade acionada - by Orbit" \
    || fail_test "header de proteção ausente ou sem assinatura"
echo "$OUT" | grep -q "EXECUÇÃO INTERROMPIDA .* - by Orbit" \
    && fail_test "linha EXECUÇÃO INTERROMPIDA não pode ser assinada (header já assina)"
rm -f "$INTENT"
pass "bloqueio de intent → assinatura única no header de proteção"

# Retomada
mk_intent "sess-quiet" "task para retomar"
OUT="$("$EXPLAIN" --resume 2>&1)"
SIG_COUNT="$(count_sig "$OUT")"
[ "$SIG_COUNT" = "1" ] \
    || fail_test "--resume esperava 1 assinatura, obteve $SIG_COUNT"
echo "$OUT" | grep -q "retomada de continuidade acionada - by Orbit" \
    || fail_test "header de retomada ausente ou sem assinatura"
echo "$OUT" | grep -q "RETOMADA DE TASK INTERROMPIDA.* - by Orbit" \
    && fail_test "banner RETOMADA não pode ser assinado (header já assina)"
rm -f "$INTENT"
pass "--resume com intent → assinatura única no header de retomada"

# Verificação de integridade
: > "$OVERRIDES"  # log vazio, integro
OUT="$("$EXPLAIN" --verify-intent-log 2>&1)"
SIG_COUNT="$(count_sig "$OUT")"
[ "$SIG_COUNT" = "1" ] \
    || fail_test "--verify-intent-log esperava 1 assinatura, obteve $SIG_COUNT"
echo "$OUT" | grep -q "verificação de integridade acionada - by Orbit" \
    || fail_test "header de verificação ausente ou sem assinatura"
echo "$OUT" | grep -qi "aurya" && fail_test "verify não pode mencionar AURYA"
pass "--verify-intent-log → assinatura única no header de verificação"

echo ""
echo "OK: assinatura '- by Orbit' está no escopo correto em 7 fluxos"
exit 0
