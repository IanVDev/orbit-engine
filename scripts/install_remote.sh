#!/usr/bin/env bash
# scripts/install_remote.sh — one-liner para instalar orbit a partir de
# GitHub Releases. Projetado para ser servido via:
#
#   curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash
#
# Ou direto do release:
#
#   curl -fsSL https://github.com/IanVDev/orbit-engine/releases/latest/download/install.sh | bash
#
# Diferente de scripts/install.sh (que compila via `go build` — exige Go),
# este script não exige nada além de: bash, curl, sha256sum, uname, install.
#
# Fluxo fail-closed em 5 passos:
#   [1/5] Detectar plataforma (linux/darwin × amd64/arm64)
#   [2/5] Resolver versão (latest ou --version vX.Y.Z)
#   [3/5] Baixar binário + .sha256 para /tmp
#   [4/5] sha256sum -c (integridade — REJEITA se não confere)
#   [5/5] Mover para ~/.orbit/bin/orbit (ou --prefix) + chmod +x + smoke test
#
# UX fail-closed: toda falha imprime CAUSA + AÇÃO corretiva (não só erro).
# Nunca deixa estado incoerente — arquivos temp são limpos no trap EXIT.

set -euo pipefail

# ── Configuração ────────────────────────────────────────────────────────
REPO="${ORBIT_REPO:-IanVDev/orbit-engine}"
VERSION=""                        # vazio = latest
PREFIX="${HOME}/.orbit/bin"       # default: user-local, sem sudo
BASE_URL="${ORBIT_INSTALL_BASE_URL:-https://github.com}"

TMP="$(mktemp -d -t orbit-install-XXXXXX)"
trap 'rm -rf "${TMP}"' EXIT

# ── Parse args ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --prefix)  PREFIX="$2"; shift 2 ;;
    --repo)    REPO="$2"; shift 2 ;;
    --help|-h)
      cat <<'HELP'
orbit install — baixa e instala o binário orbit.

Uso:
  curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash
  bash install_remote.sh [--version vX.Y.Z] [--prefix /path] [--repo owner/name]

Defaults:
  --version latest (última release publicada)
  --prefix  ~/.orbit/bin  (sem sudo)
  --repo    IanVDev/orbit-engine
HELP
      exit 0 ;;
    *)
      echo ""
      echo "ERRO: argumento desconhecido: $1" >&2
      echo "AÇÃO: rode com --help para ver as opções." >&2
      exit 1 ;;
  esac
done

# ── UX helpers (mensagens de erro estruturadas) ─────────────────────────
_die() {
  local cause="$1" action="$2"
  printf '\n\033[1;31m❌  ERRO\033[0m: %s\n' "${cause}" >&2
  printf '\033[1;33m   CAUSA\033[0m: %s\n' "${cause}" >&2
  printf '\033[1;32m   AÇÃO\033[0m: %s\n\n' "${action}" >&2
  exit 1
}

_step() { printf '[%s/5] %s\n' "$1" "$2"; }
_ok()   { printf '      \033[0;32m✓\033[0m  %s\n' "$1"; }

echo ""
echo "🛰  orbit install"
echo ""

# ── [1/5] Detectar plataforma ───────────────────────────────────────────
_step 1 "detectando plataforma..."
OS_RAW="$(uname -s)"
case "${OS_RAW}" in
  Linux)  OS="linux"  ;;
  Darwin) OS="darwin" ;;
  *) _die "SO não suportado: ${OS_RAW}" "plataformas suportadas: Linux, macOS. Use WSL no Windows." ;;
esac

ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) _die "arquitetura não suportada: ${ARCH_RAW}" "plataformas: amd64, arm64." ;;
esac

PLATFORM="${OS}-${ARCH}"
_ok "${PLATFORM}"

# ── [2/5] Resolver versão ────────────────────────────────────────────────
_step 2 "resolvendo versão..."
if [[ -z "${VERSION}" ]]; then
  # latest → redireciona; HEAD para capturar Location.
  LATEST_URL="${BASE_URL}/${REPO}/releases/latest"
  VERSION="$(curl -sIL -o /dev/null -w '%{url_effective}' --max-time 10 "${LATEST_URL}" 2>/dev/null \
    | sed -E 's|.*/tag/||; s|/$||' || true)"
  if [[ -z "${VERSION}" || "${VERSION}" == *"/"* ]]; then
    _die "não foi possível resolver a última versão em ${REPO}" \
         "verifique conexão, ou passe --version vX.Y.Z explicitamente."
  fi
fi
_ok "${VERSION}"

# ── [3/5] Download ───────────────────────────────────────────────────────
BIN_NAME="orbit-${VERSION}-${PLATFORM}"
BIN_URL="${BASE_URL}/${REPO}/releases/download/${VERSION}/${BIN_NAME}"
SHA_URL="${BIN_URL}.sha256"

_step 3 "baixando ${BIN_NAME}..."
HTTP_STATUS="$(curl -sL -o "${TMP}/${BIN_NAME}" -w '%{http_code}' --max-time 60 "${BIN_URL}" || echo 000)"
if [[ "${HTTP_STATUS}" != "200" ]]; then
  _die "download do binário falhou (HTTP ${HTTP_STATUS})" \
       "verifique se ${VERSION} existe em ${BASE_URL}/${REPO}/releases — ou passe --version outra."
fi

HTTP_STATUS="$(curl -sL -o "${TMP}/${BIN_NAME}.sha256" -w '%{http_code}' --max-time 30 "${SHA_URL}" || echo 000)"
if [[ "${HTTP_STATUS}" != "200" ]]; then
  _die "download do .sha256 falhou (HTTP ${HTTP_STATUS})" \
       "a release ${VERSION} não publicou .sha256 — instalação abortada por segurança."
fi
_ok "binário + .sha256 baixados"

# ── [4/5] Verificação de integridade ────────────────────────────────────
_step 4 "verificando sha256..."
if ! (cd "${TMP}" && sha256sum -c "${BIN_NAME}.sha256" >/dev/null 2>&1); then
  EXPECTED="$(awk '{print $1}' "${TMP}/${BIN_NAME}.sha256" 2>/dev/null || echo '?')"
  ACTUAL="$(sha256sum "${TMP}/${BIN_NAME}" 2>/dev/null | awk '{print $1}' || echo '?')"
  _die "sha256 mismatch — binário corrompido ou adulterado" \
       "esperado: ${EXPECTED} — obtido: ${ACTUAL}. Tente novamente; persiste → reporte no repo."
fi
_ok "integridade confirmada"

# ── [5/5] Instalar + smoke test ─────────────────────────────────────────
_step 5 "instalando em ${PREFIX}/orbit..."
mkdir -p "${PREFIX}"
install -m 0755 "${TMP}/${BIN_NAME}" "${PREFIX}/orbit" 2>/dev/null \
  || _die "não consegui escrever em ${PREFIX}" \
          "rode com --prefix em caminho com permissão, ex: --prefix ~/.local/bin"

SMOKE_OUT="$(ORBIT_SKIP_GUARD=1 "${PREFIX}/orbit" version 2>/dev/null || true)"
if [[ "${SMOKE_OUT}" != "orbit version ${VERSION}"* ]]; then
  _die "smoke test falhou — binário instalado não reporta ${VERSION}" \
       "output: ${SMOKE_OUT:-<vazio>}. Delete ${PREFIX}/orbit e reinstale."
fi
_ok "${SMOKE_OUT}"

# ── Próximos passos ─────────────────────────────────────────────────────
echo ""
echo -e "\033[1;32m✅  orbit instalado com sucesso\033[0m"
echo ""
echo "   Próximo passo (10s):"
echo "     ${PREFIX}/orbit quickstart"
echo ""
if ! echo "${PATH}" | tr ':' '\n' | grep -qx "${PREFIX}"; then
  echo "   💡  Adicione ao PATH para chamar 'orbit' direto:"
  echo "       echo 'export PATH=\"${PREFIX}:\$PATH\"' >> ~/.bashrc"
  echo ""
fi
