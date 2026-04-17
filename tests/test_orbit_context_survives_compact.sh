#!/usr/bin/env bash
# tests/test_orbit_context_survives_compact.sh
#
# Valida o controle determinístico de compact (scripts/orbit_compact_guard.sh):
#   case 1: snapshot persiste todos os campos obrigatórios
#   case 2: detect identifica "Compacted" e loga context_compacted
#   case 3: texto sem "Compacted" → exit 1 e nenhum log
#   case 4: rehydrate com snapshot válido → exit 0, output contém campos e log ok
#   case 5: rehydrate sem snapshot → exit 2 (fail-closed) com reason=snapshot_missing
#   case 6: rehydrate com snapshot corrompido → exit 2 com reason=snapshot_corrupt
#   case 7: ciclo end-to-end: snapshot → detect("Compacted") → rehydrate reconstitui estado
#
# Usa ORBIT_HOME temporário para isolamento total.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
GUARD="$REPO_DIR/scripts/orbit_compact_guard.sh"

[ -x "$GUARD" ] || { echo "FAIL: $GUARD nao executavel" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export ORBIT_HOME="$TMP"
SNAPSHOT="$TMP/compact_snapshot.json"
LOG="$TMP/compact_guard.jsonl"

pass() { echo "PASS  $1"; }
fail_test() { echo "FAIL  $1" >&2; exit 1; }

# ---------------------------------------------------------------------------
# case 1: snapshot persiste todos os campos
# ---------------------------------------------------------------------------
"$GUARD" snapshot \
    --current-task "Implementar compact guard" \
    --objective   "Rehidratar contexto após compact" \
    --constraints "fail-closed, minimal, determinístico" \
    --last-output "snapshot inicial" >/dev/null 2>&1 \
    || fail_test "case 1: comando snapshot retornou erro"

[ -f "$SNAPSHOT" ] || fail_test "case 1: snapshot não foi criado"
python3 - "$SNAPSHOT" <<'PY' || fail_test "case 1: conteúdo do snapshot inválido"
import json, sys
s = json.load(open(sys.argv[1]))
assert s["current_task"] == "Implementar compact guard", s
assert s["objective"]    == "Rehidratar contexto após compact", s
assert s["constraints"]  == "fail-closed, minimal, determinístico", s
assert s["last_output"]  == "snapshot inicial", s
assert "written_at" in s and s["written_at"], s
assert s["schema_version"] == 1, s
PY
pass "case 1: snapshot persiste current_task, objective, constraints, last_output"

# ---------------------------------------------------------------------------
# case 2: detect identifica "Compacted" e loga
# ---------------------------------------------------------------------------
: > "$LOG"
"$GUARD" detect "Context Compacted by host" >/dev/null 2>&1
RC=$?
[ "$RC" = "0" ] || fail_test "case 2: exit $RC (esperado 0)"
[ -f "$LOG" ] || fail_test "case 2: log não gravado"
grep -q '"event":"context_compacted"' "$LOG" || fail_test "case 2: evento context_compacted ausente"
grep -q '"has_snapshot":true' "$LOG" || fail_test "case 2: has_snapshot deveria ser true (snapshot do case 1)"
pass "case 2: detect('Compacted...') → exit 0 + log context_compacted"

# ---------------------------------------------------------------------------
# case 3: texto sem "Compacted" → exit 1 sem log
# ---------------------------------------------------------------------------
: > "$LOG"
"$GUARD" detect "mensagem qualquer sem marcador" >/dev/null 2>&1
RC=$?
[ "$RC" = "1" ] || fail_test "case 3: exit $RC (esperado 1)"
[ -s "$LOG" ] && fail_test "case 3: log não deveria ter conteúdo"
pass "case 3: texto sem 'Compacted' → exit 1, sem log"

# ---------------------------------------------------------------------------
# case 4: rehydrate com snapshot válido
# ---------------------------------------------------------------------------
: > "$LOG"
OUT="$("$GUARD" rehydrate 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 4: exit $RC (esperado 0)"
echo "$OUT" | grep -q "REHYDRATED CONTEXT"       || fail_test "case 4: header REHYDRATED CONTEXT ausente"
echo "$OUT" | grep -q "Implementar compact guard" || fail_test "case 4: current_task ausente no output"
echo "$OUT" | grep -q "Rehidratar contexto"       || fail_test "case 4: objective ausente no output"
echo "$OUT" | grep -q "fail-closed, minimal"      || fail_test "case 4: constraints ausentes no output"
grep -q '"event":"rehydration_status"' "$LOG"     || fail_test "case 4: evento rehydration_status ausente"
grep -q '"status":"ok"' "$LOG"                    || fail_test "case 4: status ok ausente"
pass "case 4: rehydrate com snapshot válido → exit 0, output + log ok"

# ---------------------------------------------------------------------------
# case 5: rehydrate sem snapshot → fail-closed (exit 2)
# ---------------------------------------------------------------------------
rm -f "$SNAPSHOT"
: > "$LOG"
OUT="$("$GUARD" rehydrate 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 5: exit $RC (esperado 2)"
echo "$OUT" | grep -q "fail-closed"             || fail_test "case 5: mensagem fail-closed ausente"
grep -q '"status":"fail_closed"' "$LOG"         || fail_test "case 5: status fail_closed ausente no log"
grep -q '"reason":"snapshot_missing"' "$LOG"    || fail_test "case 5: reason snapshot_missing ausente"
pass "case 5: rehydrate sem snapshot → exit 2 (fail-closed) + log reason=snapshot_missing"

# ---------------------------------------------------------------------------
# case 6: snapshot corrompido → fail-closed
# ---------------------------------------------------------------------------
echo '{nao-e-json-valido' > "$SNAPSHOT"
: > "$LOG"
OUT="$("$GUARD" rehydrate 2>&1)"; RC=$?
[ "$RC" = "2" ] || fail_test "case 6: exit $RC (esperado 2)"
grep -q '"reason":"snapshot_corrupt"' "$LOG" || fail_test "case 6: reason snapshot_corrupt ausente"
pass "case 6: snapshot corrompido → exit 2 (fail-closed) + log reason=snapshot_corrupt"

# ---------------------------------------------------------------------------
# case 7: ciclo end-to-end
#   snapshot(estado) → detect("Compacted") → rehydrate reconstitui estado
# ---------------------------------------------------------------------------
rm -f "$SNAPSHOT" "$LOG"

"$GUARD" snapshot \
    --current-task "Escrever testes de compact" \
    --objective   "Garantir que contexto sobrevive ao ciclo compact" \
    --constraints "sem features extras, fail-closed" \
    --last-output "6 casos validados" >/dev/null 2>&1 \
    || fail_test "case 7: snapshot falhou"

"$GUARD" detect "Previous context automatically Compacted due to token budget" >/dev/null 2>&1
RC=$?
[ "$RC" = "0" ] || fail_test "case 7: detect não reconheceu Compacted (exit $RC)"

OUT="$("$GUARD" rehydrate 2>&1)"; RC=$?
[ "$RC" = "0" ] || fail_test "case 7: rehydrate falhou (exit $RC)"
echo "$OUT" | grep -q "Escrever testes de compact"         || fail_test "case 7: current_task perdida"
echo "$OUT" | grep -q "contexto sobrevive ao ciclo compact" || fail_test "case 7: objective perdida"
echo "$OUT" | grep -q "sem features extras"                 || fail_test "case 7: constraints perdidas"
echo "$OUT" | grep -q "6 casos validados"                   || fail_test "case 7: last_output perdido"

# Log deve conter os dois eventos em ordem
grep -q '"event":"context_compacted"'  "$LOG" || fail_test "case 7: evento context_compacted ausente no log"
grep -q '"event":"rehydration_status"' "$LOG" || fail_test "case 7: evento rehydration_status ausente no log"
grep -q '"status":"ok"'                "$LOG" || fail_test "case 7: status ok ausente no log"
pass "case 7: ciclo end-to-end preserva todos os campos do contexto"

echo ""
echo "OK: orbit_compact_guard determinístico + fail-closed em 7 casos"
exit 0
