#!/usr/bin/env bash
# scripts/update_orbit.sh — Atualiza o binário orbit via GitHub Releases.
#
# Uso:
#   ./scripts/update_orbit.sh
#   ./scripts/update_orbit.sh --dest /usr/local/bin/orbit
#   ./scripts/update_orbit.sh --repo IanVDev/orbit-engine
#
# Fluxo (fail-closed):
#   [1] Detecta plataforma e caminho de destino
#   [2] Baixa binário latest do GitHub Releases
#   [3] Valida com `<tmp> version`
#   [4] Cria backup: <dest>.bak
#   [5] Substitui atomicamente via mv
#
# Qualquer erro aborta com exit 1 — o binário instalado nunca é tocado
# antes da validação passar.

set -euo pipefail

# ── Funções utilitárias ──────────────────────────────────────────────────────

_step() { printf '[%s/%s] %s\n' "$1" "$2" "$3"; }
_ok()   { printf '      ✓  %s\n' "$1"; }
_info() { printf '      ℹ  %s\n' "$1"; }
_fail() { printf '\n❌  ERRO: %s\n' "$1" >&2; exit 1; }

# ── Constantes e argumentos ──────────────────────────────────────────────────

REPO="IanVDev/orbit-engine"
BINARY_NAME="orbit"

# Detecta destino padrão a partir do binário em execução no PATH
DETECTED_DEST="$(command -v "${BINARY_NAME}" 2>/dev/null || true)"
DEST="${DETECTED_DEST:-/usr/local/bin/${BINARY_NAME}}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dest)   DEST="$2"; shift 2 ;;
    --repo)   REPO="$2"; shift 2 ;;
    --help|-h)
      echo "uso: $0 [--dest <caminho>] [--repo <owner/repo>]"
      exit 0
      ;;
    *) _fail "argumento desconhecido: $1" ;;
  esac
done

# ── Detecta plataforma ───────────────────────────────────────────────────────

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) _fail "arquitetura não suportada: ${ARCH}" ;;
esac

RELEASE_URL="https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}-${OS}-${ARCH}"
TMP_BINARY="$(mktemp "/tmp/${BINARY_NAME}-update-XXXXXX")"
BACKUP_PATH="${DEST}.bak"

echo ""
echo "🔄  orbit update"
echo ""

# ── [1] Validar destino ──────────────────────────────────────────────────────

_step 1 5 "Verificando instalação atual..."

if [[ ! -f "${DEST}" ]]; then
  _fail "binário não encontrado em ${DEST} — instale primeiro com scripts/install.sh"
fi

CURRENT_VERSION="$("${DEST}" version 2>/dev/null || true)"
if [[ -z "${CURRENT_VERSION}" ]]; then
  _fail "falha ao executar '${DEST} version' — binário atual inválido"
fi

_ok "versão atual: ${CURRENT_VERSION}"

# ── [2] Download ─────────────────────────────────────────────────────────────

_step 2 5 "Baixando latest de github.com/${REPO}..."

if ! curl -fsSL --max-time 30 "${RELEASE_URL}" -o "${TMP_BINARY}"; then
  rm -f "${TMP_BINARY}"
  _fail "falha no download de ${RELEASE_URL}"
fi

chmod +x "${TMP_BINARY}"
_ok "download concluído: ${TMP_BINARY}"

# ── [3] Validar novo binário ─────────────────────────────────────────────────

_step 3 5 "Validando novo binário..."

NEW_VERSION="$("${TMP_BINARY}" version 2>/dev/null || true)"
if [[ -z "${NEW_VERSION}" ]]; then
  rm -f "${TMP_BINARY}"
  _fail "novo binário falhou em 'version' — abortando sem alterar instalação"
fi

_ok "nova versão: ${NEW_VERSION}"

if [[ "${CURRENT_VERSION}" == "${NEW_VERSION}" ]]; then
  _info "versão idêntica — nenhuma atualização necessária"
  rm -f "${TMP_BINARY}"
  echo ""
  echo "✅  orbit já está na versão mais recente."
  echo ""
  exit 0
fi

# ── [4] Backup ───────────────────────────────────────────────────────────────

_step 4 5 "Criando backup..."

if ! cp "${DEST}" "${BACKUP_PATH}" 2>/dev/null; then
  # pode precisar de sudo se DEST for system path
  if ! sudo cp "${DEST}" "${BACKUP_PATH}"; then
    rm -f "${TMP_BINARY}"
    _fail "falha ao criar backup em ${BACKUP_PATH}"
  fi
fi

_ok "backup: ${BACKUP_PATH}"

# ── [5] Substituição atômica ─────────────────────────────────────────────────

_step 5 5 "Instalando..."

INSTALL_OK=false
if mv "${TMP_BINARY}" "${DEST}" 2>/dev/null; then
  INSTALL_OK=true
elif sudo mv "${TMP_BINARY}" "${DEST}" 2>/dev/null; then
  INSTALL_OK=true
fi

if [[ "${INSTALL_OK}" != "true" ]]; then
  rm -f "${TMP_BINARY}"
  _fail "falha ao mover binário para ${DEST} — backup disponível em ${BACKUP_PATH}"
fi

chmod +x "${DEST}" 2>/dev/null || sudo chmod +x "${DEST}" || true
_ok "instalado em ${DEST}"

# ── Confirmação final ────────────────────────────────────────────────────────

INSTALLED_VERSION="$("${DEST}" version 2>/dev/null || true)"
if [[ -z "${INSTALLED_VERSION}" ]]; then
  _fail "binário instalado falhou em 'version' pós-instalação — restaure: cp ${BACKUP_PATH} ${DEST}"
fi

echo ""
echo "✅  orbit atualizado com sucesso!"
echo "    ${CURRENT_VERSION}  →  ${INSTALLED_VERSION}"
echo "    backup: ${BACKUP_PATH}"
echo ""
