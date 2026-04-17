#!/usr/bin/env bash
# tests/test_orbit_intent_overrides_integrity.sh
#
# Valida integridade com hash encadeado em ~/.orbit/intent_overrides.jsonl:
#   case 1: entrada única — campos "hash" e "prev_hash" presentes e válidos
#   case 2: duas entradas — cadeia encadeada (E2.prev_hash == E1.hash)
#   case 3: tampering simples em E1 (session_id alterado) → hash diverge
#   case 4: tampering sofisticado em E1 (hash atualizado) → chain E2 quebra
#   case 5: entrada legada (sem hash) → próxima entrada usa prev_hash=""
#
# Verificação via Python inline — sem dependência de ferramenta extra.

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

# Aciona _log_intent_override produzindo uma entrada no OVERRIDES.
trigger_bypass() {
    local sid="$1" desc="$2"
    mk_intent "$sid" "$desc"
    "$EXPLAIN" --list --ignore-intent >/dev/null 2>&1
    rm -f "$INTENT"
}

# Verifica cadeia completa de OVERRIDES.
# Sai 0 se íntegra, 2 se adulterada (hash diverge ou chain quebra).
verify_chain() {
    local log="$1"
    LOG_PATH="$log" python3 <<'PY'
import hashlib, json, os, sys
log_path = os.environ["LOG_PATH"]
with open(log_path, encoding="utf-8") as f:
    entries = [json.loads(l) for l in f if l.strip()]
checked = 0
for i, entry in enumerate(entries):
    stored_hash = entry.get("hash", "")
    if not stored_hash:
        continue  # entrada legada sem hash — pulada
    prev_hash = entry.get("prev_hash", "")
    entry_for_hash = {k: v for k, v in entry.items() if k != "hash"}
    entry_json     = json.dumps(entry_for_hash, separators=(",", ":"), sort_keys=True)
    recomputed     = hashlib.sha256((prev_hash + entry_json).encode()).hexdigest()
    if recomputed != stored_hash:
        print(f"INTEGRITY FAIL entrada {i+1}: hash diverge", file=sys.stderr)
        sys.exit(2)
    if i > 0:
        prev_entry_hash = entries[i-1].get("hash", "")
        if prev_hash != prev_entry_hash:
            print(f"CHAIN BROKEN entrada {i+1}: prev_hash nao corresponde", file=sys.stderr)
            sys.exit(2)
    checked += 1
print(f"OK: {checked} entrada(s) verificada(s)")
PY
}

pass()      { echo "PASS  $1"; }
fail_test() { echo "FAIL  $1" >&2; exit 1; }

mk_entry "sess-A" "2026-04-17T10:00:00.000Z" 100 > "$LEDGER"

# ---------------------------------------------------------------------------
# case 1: entrada única — hash e prev_hash presentes e válidos
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
trigger_bypass "sess-A" "task A"
[ -f "$OVERRIDES" ] || fail_test "case 1: OVERRIDES nao criado"
LOG_PATH="$OVERRIDES" python3 <<'PY' || fail_test "case 1: campos hash/prev_hash ausentes ou invalidos"
import hashlib, json, os
with open(os.environ["LOG_PATH"], encoding="utf-8") as f:
    e = json.loads(f.readline())
assert "hash"      in e, "campo hash ausente"
assert "prev_hash" in e, "campo prev_hash ausente"
assert e["prev_hash"] == "", f"primeira entrada deve ter prev_hash='' mas tem '{e['prev_hash']}'"
entry_for_hash = {k: v for k, v in e.items() if k != "hash"}
recomp = hashlib.sha256(
    (e["prev_hash"] + json.dumps(entry_for_hash, separators=(",",":"), sort_keys=True)).encode()
).hexdigest()
assert recomp == e["hash"], f"hash invalido: {recomp[:16]}... != {e['hash'][:16]}..."
PY
pass "case 1: entrada única — hash e prev_hash presentes e válidos"

# ---------------------------------------------------------------------------
# case 2: duas entradas — cadeia encadeada (E2.prev_hash == E1.hash)
# ---------------------------------------------------------------------------
trigger_bypass "sess-A" "task B"
verify_chain "$OVERRIDES" || fail_test "case 2: cadeia invalida"
LOG_PATH="$OVERRIDES" python3 <<'PY' || fail_test "case 2: E2.prev_hash != E1.hash"
import json, os
with open(os.environ["LOG_PATH"], encoding="utf-8") as f:
    entries = [json.loads(l) for l in f if l.strip()]
assert len(entries) == 2, f"esperado 2 entradas, encontrado {len(entries)}"
assert entries[1]["prev_hash"] == entries[0]["hash"], \
    f"E2.prev_hash={entries[1]['prev_hash'][:16]}... != E1.hash={entries[0]['hash'][:16]}..."
PY
pass "case 2: cadeia válida — E2.prev_hash == E1.hash"

# ---------------------------------------------------------------------------
# case 3: tampering simples em E1 (session_id alterado, hash não atualizado)
# ---------------------------------------------------------------------------
LOG_PATH="$OVERRIDES" python3 <<'PY'
import json, os
path  = os.environ["LOG_PATH"]
lines = open(path, encoding="utf-8").readlines()
e1 = json.loads(lines[0])
e1["session_id"] = "TAMPERED"       # adulterar sem cobrir o rastro
lines[0] = json.dumps(e1, separators=(",", ":"), sort_keys=True) + "\n"
open(path, "w").writelines(lines)
PY
verify_chain "$OVERRIDES" 2>/dev/null && fail_test "case 3: tampering simples nao detectado"
pass "case 3: tampering simples (session_id alterado) → hash diverge detectado"

# ---------------------------------------------------------------------------
# case 4: tampering sofisticado em E1 (hash atualizado), E2.prev_hash intocado
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
trigger_bypass "sess-A" "task C"
trigger_bypass "sess-A" "task D"
LOG_PATH="$OVERRIDES" python3 <<'PY'
import hashlib, json, os
path  = os.environ["LOG_PATH"]
lines = open(path, encoding="utf-8").readlines()
e1 = json.loads(lines[0])
e1["session_id"] = "SOPHISTICATED-TAMPER"
# Recalcular hash de E1 — tentativa de cobrir o rastro
entry_for_hash = {k: v for k, v in e1.items() if k != "hash"}
e1["hash"] = hashlib.sha256(
    (e1["prev_hash"] + json.dumps(entry_for_hash, separators=(",",":"), sort_keys=True)).encode()
).hexdigest()
lines[0] = json.dumps(e1, separators=(",", ":"), sort_keys=True) + "\n"
# E2.prev_hash NÃO atualizado — aponta para o hash antigo de E1
open(path, "w").writelines(lines)
PY
verify_chain "$OVERRIDES" 2>/dev/null && fail_test "case 4: tampering sofisticado nao detectado"
pass "case 4: tampering sofisticado (hash E1 atualizado) → chain E2 quebra detectada"

# ---------------------------------------------------------------------------
# case 5: entrada legada (sem hash) → próxima entrada usa prev_hash=""
# ---------------------------------------------------------------------------
rm -f "$OVERRIDES"
printf '{"event":"intent_ignored","timestamp":"2026-01-01T00:00:00.000Z","session_id":"legacy","reason":"manual_override"}\n' \
    > "$OVERRIDES"
trigger_bypass "sess-A" "task E"
LOG_PATH="$OVERRIDES" python3 <<'PY' || fail_test "case 5: prev_hash nao eh string vazia"
import json, os
with open(os.environ["LOG_PATH"], encoding="utf-8") as f:
    entries = [json.loads(l) for l in f if l.strip()]
assert len(entries) == 2, f"esperado 2 entradas, encontrado {len(entries)}"
assert "hash" not in entries[0], "entrada legada nao devia ter campo hash"
assert entries[1].get("prev_hash") == "", \
    f"esperado prev_hash='' mas obteve '{entries[1].get('prev_hash')}'"
assert "hash" in entries[1], "nova entrada deve ter campo hash"
PY
pass "case 5: entrada legada (sem hash) → próxima entrada usa prev_hash=''"

echo ""
echo "OK: integridade com hash encadeado verificada em todos os 5 casos"
exit 0
