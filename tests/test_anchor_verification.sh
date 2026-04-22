#!/usr/bin/env bash
# tests/test_anchor_verification.sh — I20 ANCHOR_VERIFICATION fail-closed.
#
# Usa mock HTTP local de AURYA (python3 -m http.server custom) para servir
# /proofstream/submit determinístico. Sem pytest, sem rede externa.
#
# 4 cenários fail-closed:
#   [1/4] TestValidAnchorPasses            anchor íntegro → verify --chain OK
#   [2/4] TestAlteredLeafFails             1 byte numa leaf → FAIL "folha X divergente"
#   [3/4] TestAlteredSignatureFails        1 bit no app_signature → FAIL "assinatura"
#   [4/4] TestReplayIsRejected             reaplicar receipt antigo → FAIL "replay"

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-av-XXXXXX)"
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
  go build -ldflags "-X main.Version=v0.0.0-av -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null

# ── Mock AURYA: servidor HTTP que responde 200 com NodeTimestamp monotônico ─
cat > "${TMP}/aurya_stub.py" <<'PY'
import http.server, json, sys, time
class H(http.server.BaseHTTPRequestHandler):
    counter = 0
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        _body = self.rfile.read(n) if n else b""
        H.counter += 1
        # NodeTimestamp monotônico via contador embutido (evita colisão em ms).
        ts = time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime()) + f".{H.counter:09d}Z"
        resp = {
            "ok": True,
            "hash": "stub-hash-%d" % H.counter,
            "node_timestamp": ts,
            "node_signature": "stub-sig-%d" % H.counter,
        }
        data = json.dumps(resp).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
    def log_message(self, *a, **k): pass
if __name__ == "__main__":
    http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY

PORT=$((40000 + RANDOM % 5000))
python3 "${TMP}/aurya_stub.py" "${PORT}" >/dev/null 2>&1 &
MOCK_PID=$!
for _ in 1 2 3 4 5; do
  curl -sf -X POST "http://127.0.0.1:${PORT}/proofstream/submit" \
    -d '{}' >/dev/null 2>&1 && break
  sleep 0.3
done

H="${TMP}/home"
export ORBIT_HOME="${H}"
export ORBIT_ANCHOR_PATH="${H}.anchor"
AURYA="http://127.0.0.1:${PORT}"

create_logs() {
  for i in 1 2 3; do
    "${BIN}" run echo "av-${1}-${i}" >/dev/null 2>&1 || _fail "run ${1}-${i} falhou"
  done
}

# ── [1/4] anchor íntegro → verify --chain OK ────────────────────────────
echo "── [1/4] TestValidAnchorPasses ──"
create_logs "valid"
"${BIN}" anchor --host "${AURYA}" >/dev/null 2>&1 || _fail "anchor inicial falhou"
"${BIN}" verify --chain >"${TMP}/v1.out" 2>&1 \
  || { cat "${TMP}/v1.out"; _fail "verify --chain falhou em anchor íntegro"; }
grep -q "anchor ok" "${TMP}/v1.out" \
  || { cat "${TMP}/v1.out"; _fail "mensagem de anchor ok ausente"; }
echo "    ✓ anchor íntegro passa em verify --chain"

# Helper: encontra o receipt mais recente.
LATEST_RECEIPT() { ls "${H}/anchors/"*.json 2>/dev/null | sort | tail -1; }

# Helpers: isolam o vetor testado em cada cenário.
# Cada cenário precisa resetar .anchor-last-ts para testar O CHECK específico
# (senão o monotonic check mascara a falha do vetor alvo).
reset_monotonic() { rm -f "${H}/.anchor-last-ts"; }

# ── [2/4] 1 byte alterado numa leaf → verify FAIL ───────────────────────
# Vetor-alvo: full-match elemento-por-elemento (ou signature secundário).
echo "── [2/4] TestAlteredLeafFails ──"
RECEIPT=$(LATEST_RECEIPT)
[[ -n "${RECEIPT}" ]] || _fail "receipt não encontrado pós-[1]"
cp "${RECEIPT}" "${RECEIPT}.bak"

python3 - "${RECEIPT}" <<'PY'
import json, sys
p = sys.argv[1]
d = json.load(open(p))
h = d["leaf_hashes"][0]
last = h[-1]
d["leaf_hashes"][0] = h[:-1] + ("f" if last != "f" else "e")
open(p, "w").write(json.dumps(d, indent=2))
PY
reset_monotonic
if "${BIN}" verify --chain >"${TMP}/v2.out" 2>&1; then
  mv "${RECEIPT}.bak" "${RECEIPT}"
  _fail "verify aceitou receipt com leaf alterada (checks full-match+signature falharam)"
fi
# Alteração em leaf_hashes invalida a signature (corpo canônico mudou).
# O primeiro check que detecta é `verifyAnchorSignature` — isso prova que
# ele está ativo. Se sig for desabilitada, cai no full-match em seguida.
grep -qE "assinatura|folha" "${TMP}/v2.out" \
  || { cat "${TMP}/v2.out"; mv "${RECEIPT}.bak" "${RECEIPT}"; _fail "msg ausente"; }
mv "${RECEIPT}.bak" "${RECEIPT}"
echo "    ✓ leaf alterada → verify FAIL"

# ── [3/4] assinatura adulterada → verify FAIL ───────────────────────────
# Vetor-alvo: verifyAnchorSignature (Ed25519).
echo "── [3/4] TestAlteredSignatureFails ──"
cp "${RECEIPT}" "${RECEIPT}.bak"
python3 - "${RECEIPT}" <<'PY'
import json, sys
p = sys.argv[1]
d = json.load(open(p))
s = d["app_signature"]
last = s[-1]
d["app_signature"] = s[:-1] + ("f" if last != "f" else "e")
open(p, "w").write(json.dumps(d, indent=2))
PY
reset_monotonic
if "${BIN}" verify --chain >"${TMP}/v3.out" 2>&1; then
  mv "${RECEIPT}.bak" "${RECEIPT}"
  _fail "verify aceitou receipt com app_signature alterada"
fi
grep -qE "assinatura" "${TMP}/v3.out" \
  || { cat "${TMP}/v3.out"; mv "${RECEIPT}.bak" "${RECEIPT}"; _fail "msg de signature ausente"; }
mv "${RECEIPT}.bak" "${RECEIPT}"
echo "    ✓ signature alterada → verify FAIL (assinatura)"

# ── [4/4] replay: receipt antigo (mesmas leaves) reintroduzido → FAIL ──
# Vetor-alvo ISOLADO: verifyAnchorMonotonic.
# Para isolar: NÃO rodar log extra. Re-ancorar o MESMO estado gera receipt
# com mesmas leaves mas NodeTimestamp maior. Replay do antigo passa em:
#   - signature: sim (receipt antigo é self-assinado e corpo não foi tocado)
#   - leaf_count: sim (3 == 3, LeafHashes inalterado)
#   - full match: sim (leaves iguais, mesmo conteúdo)
# Único check que DEVE pegar = monotonic.
echo "── [4/4] TestReplayIsRejected ──"
OLD_RECEIPT=$(LATEST_RECEIPT)
cp "${OLD_RECEIPT}" "${TMP}/saved_old.json"

# Re-anchor do MESMO estado (sem novos logs) — apenas NodeTimestamp muda.
"${BIN}" anchor --host "${AURYA}" >/dev/null 2>&1 || _fail "anchor #2 falhou"
NEW_RECEIPT=$(LATEST_RECEIPT)
[[ "${NEW_RECEIPT}" != "${OLD_RECEIPT}" ]] || _fail "novo anchor não gerou receipt distinto"
# Consome o novo (avança .anchor-last-ts para ts_novo).
"${BIN}" verify --chain >/dev/null 2>&1 || _fail "verify --chain pós-anchor novo falhou"

# REPLAY: apaga novo, reintroduz velho (mesmas leaves, ts menor).
rm "${NEW_RECEIPT}"
cp "${TMP}/saved_old.json" "${OLD_RECEIPT}"
if "${BIN}" verify --chain >"${TMP}/v4.out" 2>&1; then
  _fail "verify aceitou replay de receipt antigo (monotonic check falhou)"
fi
grep -qE "replay|NodeTimestamp" "${TMP}/v4.out" \
  || { cat "${TMP}/v4.out"; _fail "msg de replay ausente"; }
echo "    ✓ replay de receipt antigo → verify FAIL (monotonic)"

echo ""
echo "PASS: anchor verification I20 (sig + full-match + leaf_count + anti-replay)"
