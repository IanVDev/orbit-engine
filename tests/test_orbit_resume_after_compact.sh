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
OUT="$(unset ORBIT_AURYA_ENABLED; "$EXPLAIN" --list --ignore-intent 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 10: exit $RC (esperado 0)"
echo "$OUT" | grep -q "execution override manual" || fail_test "case 10: aviso de bypass (execution override) ausente"
echo "$OUT" | grep -q "orbit: modo local" || fail_test "case 10: mensagem de modo local ausente (default deve ser opt-out)"
echo "$OUT" | grep -q "registro remoto iniciado" && fail_test "case 10: registro remoto nao deveria aparecer sem flag"
echo "$OUT" | grep -qi "aurya" && fail_test "case 10: menção a AURYA não pode aparecer no terminal"
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

# ---------------------------------------------------------------------------
# case 13: ORBIT_AURYA_ENABLED=1 troca a mensagem de gating, preserva exit 0
# e mantém o contrato interno do log (event=intent_ignored).
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
mk_intent "sess-intent-aurya" "task com flag AURYA ligada"
OUT="$(ORBIT_AURYA_ENABLED=1 ORBIT_AURYA_URL="http://127.0.0.1:1/unreachable" \
    "$EXPLAIN" --list --ignore-intent 2>/dev/null)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 13: exit $RC (esperado 0)"
echo "$OUT" | grep -q "orbit: registro remoto iniciado" || fail_test "case 13: mensagem de registro remoto ausente com flag ligada"
echo "$OUT" | grep -q "orbit: modo local" && fail_test "case 13: modo local nao deveria aparecer com flag ligada"
echo "$OUT" | grep -qi "aurya" && fail_test "case 13: menção a AURYA não pode aparecer no terminal"
python3 - "$OVERRIDES" <<'PY' || fail_test "case 13: contrato interno alterado (event != intent_ignored)"
import json, sys
e = [json.loads(l) for l in open(sys.argv[1]) if l.strip()][-1]
assert e.get("event") == "intent_ignored", f"event errado: {e}"
PY
rm -f "$INTENT"
pass "case 13: ORBIT_AURYA_ENABLED=1 → mensagem AURYA, contrato interno preservado"

# ---------------------------------------------------------------------------
# Mock curl: casos 14–16 simulam respostas AURYA sem tocar na rede.
# Troca apenas curl no PATH; python3/date/etc continuam resolvendo normal.
# ---------------------------------------------------------------------------
MOCK_BIN="$TMP/bin"
mkdir -p "$MOCK_BIN"
make_mock_curl() {
    cat > "$MOCK_BIN/curl" <<MOCK
#!/usr/bin/env bash
cat <<'BODY'
$1
BODY
MOCK
    chmod +x "$MOCK_BIN/curl"
}

# Aguarda até 1.5s pela escrita assíncrona no arquivo de stderr.
wait_for_stderr() {
    local file="$1" pattern="$2" i
    for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
        grep -q "$pattern" "$file" 2>/dev/null && return 0
        sleep 0.1
    done
    return 1
}

# ---------------------------------------------------------------------------
# case 14: resposta AURYA com event_hash → mensagem em stderr após async
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
mk_intent "sess-hash-ok" "task com hash retornado"
make_mock_curl '{"event_hash":"mock-abc-123","status":"ok"}'
ERR14="$TMP/case14.err"
PATH="$MOCK_BIN:$PATH" ORBIT_AURYA_ENABLED=1 \
    "$EXPLAIN" --list --ignore-intent >/dev/null 2>"$ERR14"
RC=$?
[ "$RC" = "0" ] || fail_test "case 14: exit $RC (esperado 0)"
wait_for_stderr "$ERR14" "ref: mock-abc-123" \
    || fail_test "case 14: hash remoto não apareceu em stderr (conteúdo: $(cat "$ERR14"))"
grep -q "orbit: override registrado remotamente (ref: mock-abc-123) - by Orbit" "$ERR14" \
    || fail_test "case 14: mensagem nao bate formato exato (ou assinatura ausente)"
grep -qi "aurya" "$ERR14" && fail_test "case 14: stderr nao pode mencionar AURYA"
rm -f "$INTENT"
pass "case 14: event_hash retornado → mensagem assinada em stderr (async não bloqueia)"

# ---------------------------------------------------------------------------
# case 15: resposta sem event_hash → nada em stderr (silencio total default)
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
mk_intent "sess-hash-none" "task sem hash"
make_mock_curl '{"status":"error","message":"invalid payload"}'
ERR15="$TMP/case15.err"
PATH="$MOCK_BIN:$PATH" ORBIT_AURYA_ENABLED=1 \
    "$EXPLAIN" --list --ignore-intent >/dev/null 2>"$ERR15"
RC=$?
[ "$RC" = "0" ] || fail_test "case 15: exit $RC (esperado 0)"
sleep 0.3  # dar tempo pro background escrever se fosse o caso
grep -qi "aurya"            "$ERR15" && fail_test "case 15: menção a AURYA não pode aparecer"
grep -q "registrado remotamente" "$ERR15" && fail_test "case 15: nao deveria confirmar registro sem hash"
grep -q "falha"             "$ERR15" && fail_test "case 15: verbose desligado nao deveria exibir falha"
rm -f "$INTENT"
pass "case 15: resposta sem hash → stderr silencioso por padrão"

# ---------------------------------------------------------------------------
# case 16: ORBIT_VERBOSE=1 + resposta sem hash → mensagem de falha em stderr
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
mk_intent "sess-hash-verbose" "task verbose"
make_mock_curl '{"status":"error"}'
ERR16="$TMP/case16.err"
PATH="$MOCK_BIN:$PATH" ORBIT_AURYA_ENABLED=1 ORBIT_VERBOSE=1 \
    "$EXPLAIN" --list --ignore-intent >/dev/null 2>"$ERR16"
RC=$?
[ "$RC" = "0" ] || fail_test "case 16: exit $RC (esperado 0)"
wait_for_stderr "$ERR16" "falha no registro remoto" \
    || fail_test "case 16: verbose ligado, mensagem de falha ausente (conteúdo: $(cat "$ERR16"))"
grep -qi "aurya" "$ERR16" && fail_test "case 16: falha verbose não pode mencionar AURYA"
grep -q "by Orbit" "$ERR16" && fail_test "case 16: falha verbose é diagnóstico, não leva assinatura"
rm -f "$INTENT"
pass "case 16: ORBIT_VERBOSE=1 → mensagem de falha (sem AURYA, sem assinatura) em stderr"

echo ""
echo "OK: orbit_explain enforcement + rastreabilidade de bypass em 16 casos"
exit 0
