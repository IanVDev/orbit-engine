#!/usr/bin/env bash
# tests/test_install_remote.sh — anti-regressão do install one-liner.
#
# Estratégia: servidor HTTP Python simulando GitHub Releases + versão
# explícita via --version (pula resolução `latest`, que redireciona no
# GitHub real).
#
# 6 cenários (1 happy path + 5 fail-closed):
#   1. Happy path: binário + sha256 OK, version confere          → exit 0
#   2. Binário 404                                                → exit 1
#   3. sha256 404                                                 → exit 1
#   4. sha256 adulterado                                          → exit 1
#   5. Binário reporta versão diferente (smoke test detecta)      → exit 1
#   6. Prefix sem permissão de escrita                            → exit 1
#
# Sem rede externa: tudo em mock HTTP local + repo local.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-install-test-XXXXXX)"
PORT=""
MOCK_PID=""

cleanup() {
  [[ -n "${MOCK_PID}" ]] && kill "${MOCK_PID}" 2>/dev/null || true
  rm -rf "${TMP}"
}
trap cleanup EXIT

_fail() { echo "FAIL: $*" >&2; exit 1; }

# ── Setup árvore estilo GitHub Releases ─────────────────────────────────
ASSETS_DIR="${TMP}/www/IanVDev/orbit-engine/releases/download/v0.1.4"
mkdir -p "${ASSETS_DIR}"

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
BIN_NAME="orbit-v0.1.4-linux-amd64"
BIN="${ASSETS_DIR}/${BIN_NAME}"

(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v0.1.4 -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null
(cd "${ASSETS_DIR}" && sha256sum "${BIN_NAME}" > "${BIN_NAME}.sha256")

PORT=$((30000 + RANDOM % 5000))
(cd "${TMP}/www" && python3 -m http.server "${PORT}" >/dev/null 2>&1) &
MOCK_PID=$!
for _ in 1 2 3 4 5; do
  curl -sf "http://127.0.0.1:${PORT}/" >/dev/null 2>&1 && break
  sleep 0.3
done

export ORBIT_INSTALL_BASE_URL="http://127.0.0.1:${PORT}"

# Hack: `uname -m` no ambiente → amd64 (caso o teste rode em arm, ajusta target).
UNAME_ARCH="$(uname -m)"
if [[ "${UNAME_ARCH}" != "x86_64" && "${UNAME_ARCH}" != "amd64" ]]; then
  echo "SKIP: teste foi escrito para x86_64 (host atual: ${UNAME_ARCH})"
  exit 0
fi

PREFIX="${TMP}/bin"

run_install() {
  bash "${REPO_ROOT}/scripts/install_remote.sh" \
    --version v0.1.4 --prefix "${PREFIX}" --repo IanVDev/orbit-engine "$@"
}

# ── [1/6] Happy path ─────────────────────────────────────────────────────
echo "── [1/6] happy path ──"
run_install >"${TMP}/s1.out" 2>&1 \
  || { cat "${TMP}/s1.out"; _fail "cenário 1 deveria passar"; }
grep -q "orbit instalado com sucesso" "${TMP}/s1.out" \
  || { cat "${TMP}/s1.out"; _fail "cenário 1: mensagem de sucesso ausente"; }
[[ -x "${PREFIX}/orbit" ]] || _fail "cenário 1: binário não foi instalado"
ORBIT_SKIP_GUARD=1 "${PREFIX}/orbit" version | grep -q "v0.1.4" \
  || _fail "cenário 1: binário instalado não reporta v0.1.4"
rm -f "${PREFIX}/orbit"
echo "    ✓ PASS — binário instalado e reporta v0.1.4"

# ── [2/6] binário 404 ────────────────────────────────────────────────────
echo "── [2/6] binário 404 ──"
mv "${BIN}" "${BIN}.bak"
if run_install >"${TMP}/s2.out" 2>&1; then
  mv "${BIN}.bak" "${BIN}"; _fail "cenário 2 deveria falhar"
fi
grep -q "download do binário falhou" "${TMP}/s2.out" \
  || { cat "${TMP}/s2.out"; mv "${BIN}.bak" "${BIN}"; _fail "cenário 2: msg errada"; }
grep -q "AÇÃO" "${TMP}/s2.out" \
  || { cat "${TMP}/s2.out"; mv "${BIN}.bak" "${BIN}"; _fail "cenário 2: AÇÃO ausente na UX"; }
mv "${BIN}.bak" "${BIN}"
echo "    ✓ FAIL com CAUSA + AÇÃO"

# ── [3/6] sha256 404 ─────────────────────────────────────────────────────
echo "── [3/6] sha256 404 ──"
mv "${BIN}.sha256" "${BIN}.sha256.bak"
if run_install >"${TMP}/s3.out" 2>&1; then
  mv "${BIN}.sha256.bak" "${BIN}.sha256"; _fail "cenário 3 deveria falhar"
fi
grep -q "download do .sha256 falhou" "${TMP}/s3.out" \
  || { cat "${TMP}/s3.out"; mv "${BIN}.sha256.bak" "${BIN}.sha256"; _fail "cenário 3: msg errada"; }
mv "${BIN}.sha256.bak" "${BIN}.sha256"
echo "    ✓ FAIL com CAUSA + AÇÃO"

# ── [4/6] sha256 adulterado ──────────────────────────────────────────────
echo "── [4/6] sha256 adulterado ──"
SHA_ORIG="$(cat "${BIN}.sha256")"
echo "0000000000000000000000000000000000000000000000000000000000000000  ${BIN_NAME}" \
  > "${BIN}.sha256"
if run_install >"${TMP}/s4.out" 2>&1; then
  echo "${SHA_ORIG}" > "${BIN}.sha256"; _fail "cenário 4 deveria falhar"
fi
grep -q "sha256 mismatch" "${TMP}/s4.out" \
  || { cat "${TMP}/s4.out"; echo "${SHA_ORIG}" > "${BIN}.sha256"; _fail "cenário 4: msg errada"; }
echo "${SHA_ORIG}" > "${BIN}.sha256"
echo "    ✓ FAIL: integridade detectada"

# ── [5/6] binário reporta versão diferente ──────────────────────────────
echo "── [5/6] version mismatch no smoke ──"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v9.9.9 -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null
(cd "${ASSETS_DIR}" && sha256sum "${BIN_NAME}" > "${BIN_NAME}.sha256")
if run_install >"${TMP}/s5.out" 2>&1; then
  _fail "cenário 5 deveria falhar"
fi
grep -q "smoke test falhou" "${TMP}/s5.out" \
  || { cat "${TMP}/s5.out"; _fail "cenário 5: msg errada"; }
echo "    ✓ FAIL: smoke pega version mismatch"

# Rebuild correto para próximo cenário.
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v0.1.4 -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null
(cd "${ASSETS_DIR}" && sha256sum "${BIN_NAME}" > "${BIN_NAME}.sha256")

# ── [6/6] prefix sem permissão ───────────────────────────────────────────
echo "── [6/6] prefix sem permissão ──"
if [[ "$(id -u)" == "0" ]]; then
  echo "    ⚠  SKIP — teste roda como root, chmod 555 é ignorado (sandbox)."
  echo ""
  echo "PASS: install_remote (5 cenários executados, 1 skipped como root)"
  exit 0
fi
RO_DIR="${TMP}/readonly"
mkdir -p "${RO_DIR}"
chmod 555 "${RO_DIR}"
if bash "${REPO_ROOT}/scripts/install_remote.sh" \
     --version v0.1.4 --prefix "${RO_DIR}/bin" --repo IanVDev/orbit-engine \
     >"${TMP}/s6.out" 2>&1; then
  chmod 755 "${RO_DIR}"; _fail "cenário 6 deveria falhar"
fi
grep -q "não consegui escrever" "${TMP}/s6.out" \
  || { cat "${TMP}/s6.out"; chmod 755 "${RO_DIR}"; _fail "cenário 6: msg errada"; }
chmod 755 "${RO_DIR}"
echo "    ✓ FAIL com sugestão de --prefix"

echo ""
echo "PASS: install_remote (6 cenários — 1 happy + 5 fail-closed com CAUSA + AÇÃO)"
