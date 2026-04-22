#!/usr/bin/env bash
# tests/test_release_gate.sh — anti-regressão do Release Gate Soberano.
#
# Estratégia determinística sem rede:
#   - RELEASE_GATE_REMOTE aponta para o próprio repo (tem tag v0.1.1 local).
#   - RELEASE_GATE_BASE_URL aponta para servidor HTTP Python local que serve
#     os assets como o GitHub Releases serviria.
#
# 6 cenários (1 happy path + 5 fail-closed):
#   1. Caminho feliz: tag + binário + .sha256 corretos         → exit 0
#   2. Tag ausente no remoto                                   → exit 1
#   3. Binário ausente (404)                                   → exit 1
#   4. sha256 ausente (404)                                    → exit 1
#   5. sha256 adulterado                                       → exit 1
#   6. Version mismatch (binário reporta outra tag)            → exit 1
#
# Deps: git, curl, sha256sum, python3, go. Sem rede externa.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-relgate-test-XXXXXX)"
PORT=""
MOCK_PID=""

cleanup() {
  [[ -n "${MOCK_PID}" ]] && kill "${MOCK_PID}" 2>/dev/null || true
  rm -rf "${TMP}"
}
trap cleanup EXIT

_fail() { echo "FAIL: $*" >&2; exit 1; }

# Pré-requisito: a tag v0.1.1 precisa existir no repo (foi criada no ciclo
# anterior pelo `make tag-release`). Fail-closed: se não existir, o teste
# aborta em vez de dar falso positivo.
git -C "${REPO_ROOT}" rev-parse -q --verify refs/tags/v0.1.1 >/dev/null \
  || _fail "tag v0.1.1 não existe local — rode: make tag-release VERSION=v0.1.1"

# ── Setup: árvore HTTP estilo GitHub Releases ───────────────────────────
ASSETS_DIR="${TMP}/www/IanVDev/orbit-engine/releases/download/v0.1.1"
mkdir -p "${ASSETS_DIR}"

# Build binário com versão correta.
COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
BIN_PATH="${ASSETS_DIR}/orbit-v0.1.1-linux-amd64"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v0.1.1 -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN_PATH}" ./cmd/orbit) >/dev/null
(cd "${ASSETS_DIR}" && sha256sum "$(basename "${BIN_PATH}")" > "$(basename "${BIN_PATH}").sha256")

# Servidor HTTP mock.
PORT=$((20000 + RANDOM % 10000))
(cd "${TMP}/www" && python3 -m http.server "${PORT}" >/dev/null 2>&1) &
MOCK_PID=$!
for _ in 1 2 3 4 5; do
  curl -sf "http://127.0.0.1:${PORT}/" >/dev/null 2>&1 && break
  sleep 0.3
done

export RELEASE_GATE_BASE_URL="http://127.0.0.1:${PORT}"
export RELEASE_GATE_REMOTE="${REPO_ROOT}"   # repo local como remote (tem v0.1.1)

run_gate() {
  bash "${REPO_ROOT}/scripts/release_gate.sh" \
    --repo IanVDev/orbit-engine --platform linux-amd64 "$@"
}

# ── [1/6] caminho feliz ──────────────────────────────────────────────────
echo "── [1/6] caminho feliz ──"
run_gate --version v0.1.1 >"${TMP}/s1.out" 2>&1 \
  || { cat "${TMP}/s1.out"; _fail "cenário 1 deveria passar"; }
grep -q "RELEASE GATE: PASS" "${TMP}/s1.out" || _fail "cenário 1: PASS não impresso"
echo "    ✓ PASS"

# ── [2/6] tag ausente (v9.9.9 não existe no repo) ────────────────────────
echo "── [2/6] tag ausente ──"
if run_gate --version v9.9.9 >"${TMP}/s2.out" 2>&1; then
  _fail "cenário 2 deveria falhar"
fi
grep -q "tag v9.9.9 NÃO existe" "${TMP}/s2.out" \
  || { cat "${TMP}/s2.out"; _fail "cenário 2: mensagem errada"; }
echo "    ✓ FAIL corretamente"

# ── [3/6] binário ausente (404) ──────────────────────────────────────────
echo "── [3/6] binário ausente ──"
mv "${BIN_PATH}" "${BIN_PATH}.bak"
if run_gate --version v0.1.1 >"${TMP}/s3.out" 2>&1; then
  mv "${BIN_PATH}.bak" "${BIN_PATH}"; _fail "cenário 3 deveria falhar"
fi
grep -q "binário indisponível" "${TMP}/s3.out" \
  || { cat "${TMP}/s3.out"; mv "${BIN_PATH}.bak" "${BIN_PATH}"; _fail "cenário 3: msg errada"; }
mv "${BIN_PATH}.bak" "${BIN_PATH}"
echo "    ✓ FAIL corretamente"

# ── [4/6] sha256 ausente (404) ───────────────────────────────────────────
echo "── [4/6] sha256 ausente ──"
mv "${BIN_PATH}.sha256" "${BIN_PATH}.sha256.bak"
if run_gate --version v0.1.1 >"${TMP}/s4.out" 2>&1; then
  mv "${BIN_PATH}.sha256.bak" "${BIN_PATH}.sha256"; _fail "cenário 4 deveria falhar"
fi
grep -q ".sha256 indisponível" "${TMP}/s4.out" \
  || { cat "${TMP}/s4.out"; mv "${BIN_PATH}.sha256.bak" "${BIN_PATH}.sha256"; _fail "cenário 4: msg errada"; }
mv "${BIN_PATH}.sha256.bak" "${BIN_PATH}.sha256"
echo "    ✓ FAIL corretamente"

# ── [5/6] sha256 adulterado ──────────────────────────────────────────────
echo "── [5/6] sha256 adulterado ──"
SHA_ORIG="$(cat "${BIN_PATH}.sha256")"
echo "0000000000000000000000000000000000000000000000000000000000000000  $(basename "${BIN_PATH}")" \
  > "${BIN_PATH}.sha256"
if run_gate --version v0.1.1 >"${TMP}/s5.out" 2>&1; then
  echo "${SHA_ORIG}" > "${BIN_PATH}.sha256"; _fail "cenário 5 deveria falhar"
fi
grep -q "sha256 não confere" "${TMP}/s5.out" \
  || { cat "${TMP}/s5.out"; echo "${SHA_ORIG}" > "${BIN_PATH}.sha256"; _fail "cenário 5: msg errada"; }
echo "${SHA_ORIG}" > "${BIN_PATH}.sha256"
echo "    ✓ FAIL corretamente"

# ── [6/6] version mismatch ───────────────────────────────────────────────
echo "── [6/6] version mismatch ──"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v9.9.9 -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN_PATH}" ./cmd/orbit) >/dev/null
(cd "${ASSETS_DIR}" && sha256sum "$(basename "${BIN_PATH}")" > "$(basename "${BIN_PATH}").sha256")
if run_gate --version v0.1.1 >"${TMP}/s6.out" 2>&1; then
  _fail "cenário 6 deveria falhar"
fi
grep -q "version mismatch" "${TMP}/s6.out" \
  || { cat "${TMP}/s6.out"; _fail "cenário 6: msg errada"; }
echo "    ✓ FAIL corretamente"

echo ""
echo "PASS: release_gate (6 cenários — 1 happy + 5 fail-closed)"
