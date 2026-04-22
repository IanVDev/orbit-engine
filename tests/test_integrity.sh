#!/usr/bin/env bash
# tests/test_integrity.sh — I17/I18/I19: body_hash, chain, merkle.
#
# 4 cenários fail-closed (sem pytest; usa binário orbit real + stdlib):
#
#   [1/4] TestBodyHashPresent              body_hash preenchido em cada log
#   [2/4] TestVerifyDetectsByteTamper      alterar 1 byte do log → verify FAIL
#   [3/4] TestVerifyChainDetectsBreak      remover log do meio → verify --chain FAIL
#   [4/4] TestMerkleRootDeterministic      2 chamadas idênticas → mesmo root (via go test)
#
# Fail-closed: qualquer assertiva falha → exit 1.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-int-XXXXXX)"
SMOKE_TEST_FILE="${REPO_ROOT}/tracking/cmd/orbit/merkle_smoke_test.go"
cleanup() { rm -f "${SMOKE_TEST_FILE}"; rm -rf "${TMP}"; }
trap cleanup EXIT

_fail() { echo "FAIL: $*" >&2; exit 1; }

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
BIN="${TMP}/orbit"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v0.0.0-int -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null

H="${TMP}/home"
export ORBIT_HOME="${H}"
export ORBIT_ANCHOR_PATH="${H}.anchor"

# ── [1/4] body_hash preenchido em cada log ──────────────────────────────
echo "── [1/4] TestBodyHashPresent ──"
"${BIN}" run echo integrity-1 >/dev/null 2>&1 || _fail "run 1 falhou"
LOG=$(ls "${H}/logs/"*.json | head -1)
python3 - "${LOG}" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
if not d.get("body_hash"):
    print("FAIL: body_hash vazio — writer não chama CanonicalHash", file=sys.stderr); sys.exit(1)
if len(d["body_hash"]) != 64:
    print(f"FAIL: body_hash len={len(d['body_hash'])}; esperado 64", file=sys.stderr); sys.exit(1)
PY
echo "    ✓ body_hash presente (64 hex)"

# ── [2/4] 1 byte alterado → verify rejeita ──────────────────────────────
echo "── [2/4] TestVerifyDetectsByteTamper ──"
python3 - "${LOG}" <<'PY'
import json, sys
p = sys.argv[1]
d = json.load(open(p))
if not d["output"]:
    d["output"] = "x"
else:
    first = d["output"][0]
    flipped = first.upper() if first.islower() else (first.lower() if first.isupper() else "X")
    d["output"] = flipped + d["output"][1:]
open(p, "w").write(json.dumps(d, indent=2))
PY
if "${BIN}" verify "${LOG}" >/dev/null 2>&1; then
  _fail "verify aceitou log adulterado (1 byte no output)"
fi
echo "    ✓ verify rejeita log com 1 byte alterado"

# ── [3/4] chain break detection ─────────────────────────────────────────
echo "── [3/4] TestVerifyChainDetectsBreak ──"
rm -rf "${H}" "${ORBIT_ANCHOR_PATH}"
mkdir -p "${H}/logs"
for i in 1 2 3; do
  "${BIN}" run echo "chain-${i}" >/dev/null 2>&1 || _fail "run chain-${i} falhou"
done

"${BIN}" verify --chain >"${TMP}/ch1.out" 2>&1 \
  || { cat "${TMP}/ch1.out"; _fail "--chain falhou em chain íntegra"; }

MIDDLE=$(ls "${H}/logs/"*.json | sort | sed -n '2p')
rm -f "${MIDDLE}"
if "${BIN}" verify --chain >"${TMP}/ch2.out" 2>&1; then
  cat "${TMP}/ch2.out"; _fail "--chain aceitou chain com log do meio removido"
fi
echo "    ✓ --chain detecta remoção de log do meio"

# ── [4/4] merkle root determinístico ────────────────────────────────────
echo "── [4/4] TestMerkleRootDeterministic ──"
cat > "${SMOKE_TEST_FILE}" <<'GO'
package main
import "testing"
func TestMerkleDeterministicSmoke(t *testing.T) {
    leaves := []string{"aa", "bb", "cc", "dd"}
    r1, err := ComputeMerkleRoot(leaves)
    if err != nil { t.Fatal(err) }
    r2, err := ComputeMerkleRoot(leaves)
    if err != nil { t.Fatal(err) }
    if r1 != r2 { t.Fatalf("merkle não determinístico: %s != %s", r1, r2) }
    if len(r1) != 64 { t.Fatalf("root len=%d; want 64", len(r1)) }
    r3, _ := ComputeMerkleRoot([]string{"bb", "aa", "cc", "dd"})
    if r1 == r3 { t.Fatal("merkle ignora ordem dos leaves — root colisão") }
}
GO
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go test -run TestMerkleDeterministicSmoke -count=1 ./cmd/orbit >"${TMP}/mk.out" 2>&1) \
  || { cat "${TMP}/mk.out"; _fail "merkle não determinístico ou ignora ordem"; }
echo "    ✓ merkle root estável + sensível à ordem"

echo ""
echo "PASS: integrity (I17 body_hash + I18 chain + I19 merkle + 1-byte tamper)"
