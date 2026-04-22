#!/usr/bin/env bash
# tests/test_trusted_signer.sh — I21 TRUSTED_ANCHOR_SIGNER fail-closed.
#
# Valida que receipts só são aceitos quando assinados com a chave trusted
# hardcoded (trustedAuryaPubKey). Qualquer outra chave → FAIL.
#
# 3 cenários:
#   [1/3] TestTrustedSignerPasses    signer default (dev-key) → PASS + trusted=true
#   [2/3] TestUntrustedSignerFails   signer diferente → FAIL "não assinado por trusted"
#   [3/3] TestLogsSignerFingerprint  verify --chain imprime signer=... trusted=true

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-ts-XXXXXX)"
PORT=""
MOCK_PID=""

cleanup() {
  [[ -n "${MOCK_PID}" ]] && kill "${MOCK_PID}" 2>/dev/null || true
  rm -rf "${TMP}"
}
trap cleanup EXIT

_fail() { echo "FAIL: $*" >&2; exit 1; }

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
BIN="${TMP}/orbit"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v0.0.0-ts -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null

# ── AURYA stub (mesmo padrão de test_anchor_verification.sh) ────────────
cat > "${TMP}/aurya_stub.py" <<'PY'
import http.server, json, sys, time
class H(http.server.BaseHTTPRequestHandler):
    counter = 0
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0); _ = self.rfile.read(n) if n else b""
        H.counter += 1
        ts = time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime()) + f".{H.counter:09d}Z"
        data = json.dumps({"ok": True, "hash": f"h{H.counter}", "node_timestamp": ts, "node_signature": f"s{H.counter}"}).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data))); self.end_headers(); self.wfile.write(data)
    def log_message(self, *a, **k): pass
if __name__ == "__main__":
    http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY

PORT=$((45000 + RANDOM % 5000))
python3 "${TMP}/aurya_stub.py" "${PORT}" >/dev/null 2>&1 &
MOCK_PID=$!
for _ in 1 2 3 4 5; do
  curl -sf -X POST "http://127.0.0.1:${PORT}/proofstream/submit" -d '{}' >/dev/null 2>&1 && break
  sleep 0.3
done

AURYA="http://127.0.0.1:${PORT}"

# ── [1/3] signer default (trusted) → PASS ────────────────────────────────
echo "── [1/3] TestTrustedSignerPasses ──"
H1="${TMP}/h1"
export ORBIT_HOME="${H1}"
export ORBIT_ANCHOR_PATH="${H1}.anchor"
unset ORBIT_SIGNER_PRIVKEY
"${BIN}" run echo trusted-1 >/dev/null 2>&1 || _fail "run falhou"
"${BIN}" run echo trusted-2 >/dev/null 2>&1 || _fail "run falhou"
"${BIN}" anchor --host "${AURYA}" >/dev/null 2>&1 || _fail "anchor default falhou"
"${BIN}" verify --chain >"${TMP}/v1.out" 2>&1 \
  || { cat "${TMP}/v1.out"; _fail "verify --chain com signer default falhou"; }
grep -q "trusted=true" "${TMP}/v1.out" \
  || { cat "${TMP}/v1.out"; _fail "output não mostra trusted=true"; }
echo "    ✓ signer default passa + output mostra trusted=true"

# ── [2/3] signer DIFERENTE → FAIL ───────────────────────────────────────
echo "── [2/3] TestUntrustedSignerFails ──"
H2="${TMP}/h2"
export ORBIT_HOME="${H2}"
export ORBIT_ANCHOR_PATH="${H2}.anchor"

# Gera keypair novo (diferente da default).
ALT_PRIV="$(python3 -c '
import os, hashlib
# Gera 64 bytes seed + derivation Ed25519 é complexo em stdlib. Usa Go via
# exec para manter "sem deps externas" — ed25519 dev não está em cpython 3.11 stdlib.
' 2>/dev/null || true)"

# Alternativa: escreve um gerador Go inline, compila, executa, captura priv hex.
cat > "${TMP}/kg.go" <<'GO'
package main
import ("crypto/ed25519"; "crypto/rand"; "encoding/hex"; "fmt")
func main() {
  _, priv, _ := ed25519.GenerateKey(rand.Reader)
  fmt.Print(hex.EncodeToString(priv))
}
GO
ALT_PRIV="$(cd ${TMP} && GOTOOLCHAIN=${GOTOOLCHAIN:-local} go run kg.go)"
[[ ${#ALT_PRIV} -eq 128 ]] || _fail "gerador produziu priv de tamanho ${#ALT_PRIV}"

# Anchor com signer alternativo
"${BIN}" run echo untrusted-1 >/dev/null 2>&1 || _fail "run untrusted falhou"
ORBIT_SIGNER_PRIVKEY="${ALT_PRIV}" "${BIN}" anchor --host "${AURYA}" >/dev/null 2>&1 \
  || _fail "anchor com signer alternativo deveria ter SIDO GERADO (não testamos recusa no writer)"

# Verify DEVE rejeitar — AppPub do receipt difere de trustedAuryaPubKey.
unset ORBIT_SIGNER_PRIVKEY
if "${BIN}" verify --chain >"${TMP}/v2.out" 2>&1; then
  cat "${TMP}/v2.out"
  _fail "verify aceitou receipt assinado com key diferente da trusted"
fi
grep -qE "não assinado por trusted signer|CRITICAL" "${TMP}/v2.out" \
  || { cat "${TMP}/v2.out"; _fail "msg de untrusted signer ausente"; }
echo "    ✓ receipt com signer não-trusted → FAIL com CRITICAL"

# ── [3/3] verify imprime signer fingerprint + trusted flag ──────────────
echo "── [3/3] TestLogsSignerFingerprint ──"
# Reusa setup [1/3] — reverifica.
export ORBIT_HOME="${H1}"
export ORBIT_ANCHOR_PATH="${H1}.anchor"
# Reset monotonic pra permitir re-verify.
rm -f "${H1}/.anchor-last-ts"
"${BIN}" verify --chain >"${TMP}/v3.out" 2>&1 || _fail "re-verify falhou"
grep -qE "signer=[a-f0-9]{16}\.\.\. trusted=true" "${TMP}/v3.out" \
  || { cat "${TMP}/v3.out"; _fail "formato 'signer=<fp>... trusted=true' ausente"; }
echo "    ✓ verify output: 'signer=<16 hex>... trusted=true'"

echo ""
echo "PASS: trusted anchor signer I21 (default PASS + alt FAIL + log fingerprint)"
