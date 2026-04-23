#!/usr/bin/env bash
# scripts/release_gate.sh — Release Gate Soberano.
#
# Contrato: um release só existe se puder ser consumido da forma que
# o README documenta. Este gate valida a distribuição pública ponta a
# ponta, fail-closed. Sem isso, "prod-ready local" é teatro.
#
# 5 verificações sequenciais (cada uma pode abortar o gate):
#   [1/5] Tag existe no remoto git (git ls-remote --tags)
#   [2/5] Asset binário existe no GitHub Releases (HEAD 200)
#   [3/5] Asset .sha256 existe no GitHub Releases (HEAD 200)
#   [4/5] Download do binário + verificação do sha256 (sha256sum -c)
#   [5/5] Binário reporta exatamente a versão esperada (orbit version)
#
# Uso:
#   scripts/release_gate.sh --version v0.1.1
#   scripts/release_gate.sh --version v0.1.1 --repo IanVDev/orbit-engine
#   scripts/release_gate.sh --version v0.1.1 --platform linux-amd64
#
# Overrides (CI + teste):
#   RELEASE_GATE_BASE_URL  override do prefixo GitHub Releases (default: github.com)
#   RELEASE_GATE_REMOTE    override do `git remote` para o ls-remote (default: origin)
#
# Fail-closed:
#   Qualquer passo != OK → exit 1. Stdout determinístico (PASS/FAIL por linha).
#   Sem sudo, sem fallback silencioso, sem retry cego.
#
# Dependências: git, curl, sha256sum (coreutils). Sem python, sem jq.

set -euo pipefail

# ── Args ─────────────────────────────────────────────────────────────────
VERSION=""
REPO="IanVDev/orbit-engine"
PLATFORM="linux-amd64"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)  VERSION="$2"; shift 2 ;;
    --repo)     REPO="$2"; shift 2 ;;
    --platform) PLATFORM="$2"; shift 2 ;;
    --help|-h)
      sed -n '2,33p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "FAIL: argumento desconhecido: $1" >&2; exit 1 ;;
  esac
done

[[ -n "${VERSION}" ]] || { echo "FAIL: --version é obrigatório" >&2; exit 1; }

BASE_URL="${RELEASE_GATE_BASE_URL:-https://github.com}"
REMOTE="${RELEASE_GATE_REMOTE:-origin}"
BIN_NAME="orbit-${VERSION}-${PLATFORM}"
BIN_URL="${BASE_URL}/${REPO}/releases/download/${VERSION}/${BIN_NAME}"
SHA_URL="${BIN_URL}.sha256"
TMP="$(mktemp -d -t orbit-relgate-XXXXXX)"
trap 'rm -rf "${TMP}"' EXIT

_step() { printf '[%s/5] %s\n' "$1" "$2"; }
_pass() { printf '      ✓  %s\n' "$1"; }
_fail() { printf '\n❌  FAIL: %s\n' "$1" >&2; exit 1; }

echo ""
echo "══ Release Gate Soberano — ${VERSION} ══"
echo "    repo:     ${REPO}"
echo "    platform: ${PLATFORM}"
echo "    base:     ${BASE_URL}"
echo ""

# ── [1/5] Tag existe no remoto ────────────────────────────────────────────
_step 1 "verificando tag remota ${VERSION} em ${REMOTE}..."
REMOTE_TAGS="$(git ls-remote --tags "${REMOTE}" 2>&1)" \
  || _fail "git ls-remote --tags ${REMOTE} falhou — remote inacessível"

if ! echo "${REMOTE_TAGS}" | grep -qE "refs/tags/${VERSION}\$"; then
  _fail "tag ${VERSION} NÃO existe em ${REMOTE} — rode: git push ${REMOTE} ${VERSION}"
fi
_pass "tag ${VERSION} presente em ${REMOTE}"

# ── [2/5] Asset binário existe no GitHub Releases ─────────────────────────
_step 2 "HEAD ${BIN_URL}..."
BIN_STATUS="$(curl -sfL -o /dev/null -w '%{http_code}' -I --max-time 10 "${BIN_URL}" || echo 000)"
if [[ "${BIN_STATUS}" != "200" ]]; then
  _fail "binário indisponível (HTTP ${BIN_STATUS}) — release.yml rodou? ver ${BASE_URL}/${REPO}/actions"
fi
_pass "binário disponível (HTTP 200)"

# ── [3/5] Asset sha256 existe ────────────────────────────────────────────
_step 3 "HEAD ${SHA_URL}..."
SHA_STATUS="$(curl -sfL -o /dev/null -w '%{http_code}' -I --max-time 10 "${SHA_URL}" || echo 000)"
if [[ "${SHA_STATUS}" != "200" ]]; then
  _fail ".sha256 indisponível (HTTP ${SHA_STATUS}) — release sem proof de integridade"
fi
_pass ".sha256 disponível (HTTP 200)"

# ── [4/5] Download + sha256 check ────────────────────────────────────────
_step 4 "download + sha256sum -c..."
curl -fsSL --max-time 60 "${BIN_URL}" -o "${TMP}/${BIN_NAME}" \
  || _fail "download do binário falhou"
curl -fsSL --max-time 10 "${SHA_URL}" -o "${TMP}/${BIN_NAME}.sha256" \
  || _fail "download do .sha256 falhou"

(cd "${TMP}" && sha256sum -c "${BIN_NAME}.sha256" >/dev/null 2>&1) \
  || _fail "sha256 não confere — binário adulterado ou upload corrompido"
_pass "sha256 confere"

# ── [5/5] Binário reporta a versão esperada ───────────────────────────────
_step 5 "executando binário + validando versão..."
chmod +x "${TMP}/${BIN_NAME}"

# ORBIT_SKIP_GUARD=1 — o binário tem startup-guard de PATH/integridade que é
# irrelevante para esta validação (não estamos no ambiente de instalação do
# usuário). O gate quer saber: o binário reporta a versão certa?
REPORTED="$(ORBIT_SKIP_GUARD=1 ORBIT_SKIP_GUARD_IN_CI=1 "${TMP}/${BIN_NAME}" version 2>/dev/null || true)"
if [[ -z "${REPORTED}" ]]; then
  _fail "binário não produziu saída de 'version'"
fi

# Contrato do version: "orbit version <VERSION> (commit=... build=...)"
if ! echo "${REPORTED}" | grep -qE "^orbit version ${VERSION} \(commit=[^ ]+ build=[^)]+\)\$"; then
  _fail "version mismatch — esperado '${VERSION}', binário reportou: ${REPORTED}"
fi
_pass "versão reportada: ${REPORTED}"

echo ""
echo "🟢 RELEASE GATE: PASS — ${VERSION} é release público válido"
echo "   ${BIN_URL}"
echo ""
