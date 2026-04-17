#!/usr/bin/env bash
# tests/test_orbit_explain_detects_tampering.sh — teste de integridade.
#
# Garante que scripts/orbit_explain.sh detecta e falha (exit != 0) em:
#   1. ledger saudável               → deve passar (sanity check)
#   2. skill_event_hash adulterado   → deve falhar
#   3. timestamp adulterado          → deve falhar (recompute diverge)
#   4. impact_estimated_tokens mudo  → deve falhar (recompute diverge)
#   5. session_id inexistente        → deve falhar
#   6. ledger ausente                → deve falhar
#   7. linha de ledger corrompida    → deve falhar
#
# Usa ORBIT_HOME temporário e ORBIT_EXPLAIN_LOCAL_ONLY=1 para não depender
# do backend. Fases 2/3 do orbit_explain são cobertas por validação
# manual com backend vivo (fora deste teste unitário).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
EXPLAIN="$REPO_DIR/scripts/orbit_explain.sh"

[ -x "$EXPLAIN" ] || {
    echo "FAIL pré-condição: $EXPLAIN não é executável" >&2
    exit 1
}

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export ORBIT_HOME="$TMP"
export ORBIT_EXPLAIN_LOCAL_ONLY=1
LEDGER="$TMP/client_ledger.jsonl"

# Recompute sha256(sid|ts|tok) exatamente como o orbit_explain faz.
h() {
    printf '%s|%s|%s' "$1" "$2" "$3" | python3 -c '
import hashlib, sys
print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())'
}

# Gera uma linha JSONL válida para o ledger.
mk_entry() {
    local sid="$1" ts="$2" tok="$3"
    local hash
    hash="$(h "$sid" "$ts" "$tok")"
    python3 -c "
import json, sys
entry = {
    'schema_version': 1,
    'written_at': '2026-04-17T10:00:00+00:00',
    'action': 'track',
    'session_id': '$sid',
    'event_type': 'skill_activate',
    'timestamp': '$ts',
    'mode': 'auto',
    'impact_estimated_tokens': $tok,
    'skill_event_hash': '$hash',
    'server_event_id': 'srv' + '$hash'[:61],
    'payload': {'session_id': '$sid', 'timestamp': '$ts'},
}
print(json.dumps(entry, separators=(',', ':'), sort_keys=True))
"
}

# Reseta o ledger para um estado saudável (3 entradas, 2 da sessão alvo).
reset_ledger() {
    mk_entry "sess-ok" "2026-04-17T10:00:00.000Z" 0   >  "$LEDGER"
    mk_entry "sess-ok" "2026-04-17T10:00:05.000Z" 150 >> "$LEDGER"
    mk_entry "outra"   "2026-04-17T10:01:00.000Z" 50  >> "$LEDGER"
}

# Executa o script silenciando stdout; devolve o exit code.
run_explain() {
    "$EXPLAIN" "$1" >/dev/null 2>&1
    echo $?
}

# Falha o teste se a asserção não bater.
assert_exit() {
    local got="$1" want="$2" case_name="$3"
    if [ "$got" = "$want" ]; then
        echo "PASS  $case_name  (exit=$got)"
    else
        echo "FAIL  $case_name  (want exit=$want, got $got)" >&2
        echo "---- saída completa para diagnóstico ----"  >&2
        "$EXPLAIN" "${LAST_SID:-sess-ok}" >&2 || true
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Case 1: ledger saudável → exit 0
# ---------------------------------------------------------------------------
reset_ledger
LAST_SID="sess-ok"
assert_exit "$(run_explain sess-ok)" 0 "case 1: ledger saudável"

# ---------------------------------------------------------------------------
# Case 2: skill_event_hash adulterado → exit 2
# ---------------------------------------------------------------------------
reset_ledger
python3 - "$LEDGER" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
lines = p.read_text().splitlines()
e = json.loads(lines[0])
e["skill_event_hash"] = "deadbeef" * 8   # 64 hex chars, mas errado
lines[0] = json.dumps(e, separators=(",", ":"), sort_keys=True)
p.write_text("\n".join(lines) + "\n")
PY
LAST_SID="sess-ok"
assert_exit "$(run_explain sess-ok)" 2 "case 2: skill_event_hash adulterado"

# ---------------------------------------------------------------------------
# Case 3: timestamp adulterado → recompute diverge → exit 2
# ---------------------------------------------------------------------------
reset_ledger
python3 - "$LEDGER" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
lines = p.read_text().splitlines()
e = json.loads(lines[1])
e["timestamp"] = "1999-01-01T00:00:00.000Z"   # skill_event_hash não atualizado
lines[1] = json.dumps(e, separators=(",", ":"), sort_keys=True)
p.write_text("\n".join(lines) + "\n")
PY
LAST_SID="sess-ok"
assert_exit "$(run_explain sess-ok)" 2 "case 3: timestamp adulterado"

# ---------------------------------------------------------------------------
# Case 4: impact_estimated_tokens adulterado → exit 2
# ---------------------------------------------------------------------------
reset_ledger
python3 - "$LEDGER" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
lines = p.read_text().splitlines()
e = json.loads(lines[0])
e["impact_estimated_tokens"] = 9999  # hash não recalculado
lines[0] = json.dumps(e, separators=(",", ":"), sort_keys=True)
p.write_text("\n".join(lines) + "\n")
PY
LAST_SID="sess-ok"
assert_exit "$(run_explain sess-ok)" 2 "case 4: impact_estimated_tokens adulterado"

# ---------------------------------------------------------------------------
# Case 5: session_id inexistente → exit 2
# ---------------------------------------------------------------------------
reset_ledger
LAST_SID="sess-nao-existe"
assert_exit "$(run_explain sess-nao-existe)" 2 "case 5: session_id inexistente"

# ---------------------------------------------------------------------------
# Case 6: ledger ausente → exit 2
# ---------------------------------------------------------------------------
rm -f "$LEDGER"
LAST_SID="sess-ok"
assert_exit "$(run_explain sess-ok)" 2 "case 6: ledger ausente"

# ---------------------------------------------------------------------------
# Case 7: linha de ledger corrompida (JSON inválido) → exit 2
# ---------------------------------------------------------------------------
reset_ledger
printf 'linha-nao-json\n' >> "$LEDGER"
LAST_SID="sess-ok"
assert_exit "$(run_explain sess-ok)" 2 "case 7: linha corrompida"

echo ""
echo "OK: orbit_explain.sh é fail-closed em todos os 7 casos de quebra"
exit 0
