#!/usr/bin/env bash
# scripts/install.sh — Instalador local do orbit-engine CLI.
#
# Compila o binário `orbit` a partir do código-fonte e instala em
# ~/.orbit/bin, sem depender de nenhum domínio ou serviço externo.
#
# Uso:
#   ./scripts/install.sh
#   ./scripts/install.sh --prefix /usr/local/bin   # instala em caminho customizado
#
# Requisitos:
#   - Go instalado e no PATH (go build)
#   - Acesso de escrita ao diretório de destino
#
# Comportamento (fail-closed):
#   - Qualquer erro aborta com exit 1.
#   - O binário só substitui o anterior se o build for bem-sucedido.
#   - Instalação atômica: arquivo temporário → mv (nunca deixa binário corrompido).
#
# Saída esperada:
#   [1/3] Compilando orbit CLI...          ✓  build ok
#   [2/3] Instalando em ~/.orbit/bin/...   ✓  instalado
#   [3/3] Configurando PATH...             ✓  PATH ok
#   ✅  orbit instalado com sucesso!
set -euo pipefail

# ── Funções utilitárias ──────────────────────────────────────────────────────

_step() { printf '[%s/%s] %s\n' "$1" "$2" "$3"; }
_ok()   { printf '      ✓  %s\n' "$1"; }
_info() { printf '      ℹ  %s\n' "$1"; }
_fail() { printf '\n❌  ERRO: %s\n' "$1" >&2; exit 1; }

# ── Constantes ───────────────────────────────────────────────────────────────

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY_NAME="orbit"
DEFAULT_PREFIX="${HOME}/.orbit/bin"
TRACKING_PKG="./cmd/orbit"

# ── Argumentos ───────────────────────────────────────────────────────────────

PREFIX="${DEFAULT_PREFIX}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    --help|-h)
      echo "uso: $0 [--prefix <diretório>]"
      echo "  --prefix   Diretório de instalação (default: ${DEFAULT_PREFIX})"
      exit 0
      ;;
    *) _fail "argumento desconhecido: $1" ;;
  esac
done

INSTALL_PATH="${PREFIX}/${BINARY_NAME}"
TMP_BINARY="/tmp/orbit-install-$$"

echo ""
echo "📦  orbit-engine — instalador local"
echo "────────────────────────────────────────────"

# ── Etapa 1/3 — Compilar ─────────────────────────────────────────────────────

_step 1 3 "Compilando orbit CLI..."

# Verifica pré-requisito: Go instalado.
if ! command -v go &>/dev/null; then
  _fail "Go não encontrado no PATH. Instale Go em https://go.dev/dl/"
fi

GO_VERSION="$(go version 2>&1 | awk '{print $3}')"
_info "usando ${GO_VERSION}"

TRACKING_DIR="${REPO_ROOT}/tracking"
if [[ ! -d "${TRACKING_DIR}" ]]; then
  _fail "diretório tracking/ não encontrado em ${REPO_ROOT}"
fi

# Determina o commit atual para injetar no binário.
COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
BUILD_VERSION="$(git -C "${REPO_ROOT}" describe --tags --always 2>/dev/null || echo 'dev')"

(
  cd "${TRACKING_DIR}"
  go build \
    -ldflags "-X main.Version=${BUILD_VERSION} -X main.Commit=${COMMIT}" \
    -o "${TMP_BINARY}" \
    ${TRACKING_PKG}
) || _fail "go build falhou — verifique os logs acima"

_ok "build ok  (version=${BUILD_VERSION} commit=${COMMIT})"

# ── Etapa 2/3 — Instalar ────────────────────────────────────────────────────

_step 2 3 "Instalando em ${INSTALL_PATH}..."

mkdir -p "${PREFIX}"

# Instalação atômica: mv é atômico no mesmo filesystem.
mv "${TMP_BINARY}" "${INSTALL_PATH}"
chmod +x "${INSTALL_PATH}"

# Smoke test: o binário deve responder ao subcomando version.
INSTALLED_VERSION="$("${INSTALL_PATH}" version 2>/dev/null || echo 'erro')"
if [[ "${INSTALLED_VERSION}" == *"erro"* ]]; then
  _fail "binário instalado mas não responde a 'orbit version'"
fi

_ok "instalado  (${INSTALLED_VERSION})"

# ── Etapa 3/3 — Configurar PATH ─────────────────────────────────────────────

_step 3 3 "Configurando PATH..."

PATH_EXPORT='export PATH="${HOME}/.orbit/bin:${PATH}"'

# Detecta o arquivo de configuração do shell do usuário.
SHELL_RC=""
if [[ -f "${HOME}/.zshrc" ]]; then
  SHELL_RC="${HOME}/.zshrc"
elif [[ -f "${HOME}/.bashrc" ]]; then
  SHELL_RC="${HOME}/.bashrc"
elif [[ -f "${HOME}/.bash_profile" ]]; then
  SHELL_RC="${HOME}/.bash_profile"
fi

if [[ "${PREFIX}" != "${DEFAULT_PREFIX}" ]]; then
  # Caminho customizado: não modifica o shell rc.
  _info "prefixo customizado — adicione ${PREFIX} ao seu PATH manualmente"
elif [[ -n "${SHELL_RC}" ]]; then
  if grep -q ".orbit/bin" "${SHELL_RC}" 2>/dev/null; then
    _ok "PATH já configurado em ${SHELL_RC}"
  else
    {
      echo ""
      echo "# orbit-engine CLI — adicionado por install.sh"
      echo "${PATH_EXPORT}"
    } >> "${SHELL_RC}"
    _ok "PATH adicionado em ${SHELL_RC}"
  fi
else
  _info "shell rc não encontrado — adicione manualmente ao seu perfil:"
  _info "  ${PATH_EXPORT}"
fi

# ── Sumário ──────────────────────────────────────────────────────────────────

echo ""
echo "✅  orbit instalado com sucesso!"
echo "────────────────────────────────────────────"
echo "   Binário  : ${INSTALL_PATH}"
echo "   Versão   : ${INSTALLED_VERSION}"
echo "   Commit   : ${COMMIT}"
echo ""
echo "   Para usar agora (sem reiniciar o shell):"
echo "   export PATH=\"\${HOME}/.orbit/bin:\${PATH}\""
echo ""
echo "   Primeiros passos:"
echo "   orbit quickstart   # fluxo completo do zero"
echo "   orbit stats        # métricas de uso"
echo ""
