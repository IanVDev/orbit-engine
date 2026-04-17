#!/usr/bin/env bash
# tests/test_orbit_explain_git_correlation.sh
#
# Garante que scripts/orbit_explain.sh exibe corretamente o bloco
# ARTEFATO CORRELACIONADO (git) a partir dos campos git_head/git_repo
# no ledger:
#
#   case A: HEAD avancou durante a sessao → bloco mostra "HEAD avançou"
#           + comando `git log <first>..<last>` para inspecao externa.
#   case B: HEAD igual nos dois extremos → bloco mostra "nao avancou".
#   case C: git_head/git_repo ausentes (ledger antigo) → bloco mostra
#           "<nao capturado>" — nao quebra nem inventa dados.
#
# Usa ORBIT_EXPLAIN_LOCAL_ONLY=1 para nao depender do backend.

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
    # $1=sid $2=ts $3=tok $4=git_head $5=git_repo
    local sid="$1" ts="$2" tok="$3" head="$4" repo="$5"
    local hash
    hash="$(h "$sid" "$ts" "$tok")"
    python3 -c "
import json
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
    'git_head': '$head',
    'git_repo': '$repo',
    'payload': {'session_id': '$sid'},
}
print(json.dumps(entry, separators=(',', ':'), sort_keys=True))
"
}

pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Case A: HEAD avancou
# ---------------------------------------------------------------------------
mk_entry "sess-A" "2026-04-17T10:00:00.000Z" 0   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" "/tmp/fake-repo"   >  "$LEDGER"
mk_entry "sess-A" "2026-04-17T10:00:05.000Z" 100 "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" "/tmp/fake-repo"   >> "$LEDGER"

OUT="$("$EXPLAIN" sess-A 2>&1)"
echo "$OUT" | grep -q "HEAD no 1º evento   aaaaaaaaaaaa" || fail "case A: HEAD 1º evento ausente"
echo "$OUT" | grep -q "HEAD no último evento  bbbbbbbbbbbb" || fail "case A: HEAD último evento ausente"
echo "$OUT" | grep -q "HEAD avançou durante a sessão" || fail "case A: mensagem avançou ausente"
echo "$OUT" | grep -qE "cd /tmp/fake-repo" || fail "case A: 'cd <repo>' ausente"
echo "$OUT" | grep -qE "git log --oneline aaaa.*\.\.bbbb" || fail "case A: comando git log ausente"
echo "$OUT" | grep -qE "git diff +aaaa.* bbbb" || fail "case A: comando git diff ausente"
pass "case A: HEAD avançou → mostra cd + git log + git diff"

# ---------------------------------------------------------------------------
# Case B: HEAD igual
# ---------------------------------------------------------------------------
mk_entry "sess-B" "2026-04-17T10:00:00.000Z" 0   "cccccccccccccccccccccccccccccccccccccccc" "/tmp/other-repo"  >  "$LEDGER"
mk_entry "sess-B" "2026-04-17T10:00:05.000Z" 50  "cccccccccccccccccccccccccccccccccccccccc" "/tmp/other-repo"  >> "$LEDGER"

OUT="$("$EXPLAIN" sess-B 2>&1)"
echo "$OUT" | grep -q "HEAD não avançou" || fail "case B: mensagem não avançou ausente"
echo "$OUT" | grep -qE "git log --oneline" && fail "case B: não deveria sugerir git log"
pass "case B: HEAD igual → não sugere git log"

# ---------------------------------------------------------------------------
# Case C: git ausente (ledger antigo, sem campos)
# ---------------------------------------------------------------------------
# Entry sem git_head/git_repo — usa mk_entry com "" nos dois.
mk_entry "sess-C" "2026-04-17T10:00:00.000Z" 0   "" ""  >  "$LEDGER"

OUT="$("$EXPLAIN" sess-C 2>&1)"
echo "$OUT" | grep -q "ARTEFATO CORRELACIONADO" || fail "case C: bloco ausente"
echo "$OUT" | grep -q "<não capturado" || fail "case C: marcador <não capturado ausente"
# Nao pode inventar comando git log
echo "$OUT" | grep -qE "git log --oneline" && fail "case C: não deveria sugerir git log"
pass "case C: git ausente → bloco explícito, nenhum dado inventado"

echo ""
echo "OK: correlação git exibida de forma honesta em todos os 3 casos"
exit 0
