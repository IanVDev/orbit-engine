#!/usr/bin/env bash
#
# build_orbit.sh — ALT INSTALL PATH (/usr/local/bin)
#
# ⚠️  Este script NÃO é o caminho canônico de instalação.
#     Use scripts/install.sh para instalar em ~/.orbit/bin/orbit (padrão).
#     Este script instala em /usr/local/bin/orbit (system-wide, requer sudo).
#
#     Motivo de existir: máquinas compartilhadas ou CI que não usa ~/.orbit.
#     Se ambos os paths estiverem presentes, `orbit doctor` emitirá WARNING.
#
# O módulo Go vive em tracking/ (não na raiz), então este script:
#   1) entra em tracking/
#   2) builda ./cmd/orbit com ldflags (Commit + BuildTime)
#   3) instala em /usr/local/bin/orbit
#   4) valida com `orbit version`
#   5) roda `orbit context-pack`
#
# Fail-closed: qualquer passo que falhar aborta o script inteiro.

set -euo pipefail

# --- resolve paths -----------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MODULE_DIR="${REPO_ROOT}/tracking"
INSTALL_PATH="${ORBIT_INSTALL_PATH:-/usr/local/bin/orbit}"

if [[ ! -f "${MODULE_DIR}/go.mod" ]]; then
  echo "❌  go.mod não encontrado em ${MODULE_DIR}" >&2
  exit 1
fi

# --- build metadata ----------------------------------------------------------
COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "▶  commit    = ${COMMIT}"
echo "▶  buildTime = ${BUILD_TIME}"
echo "▶  module    = ${MODULE_DIR}"
echo "▶  install   = ${INSTALL_PATH}"

# --- build -------------------------------------------------------------------
TMP_BIN="$(mktemp -t orbit.XXXXXX)"
trap 'rm -f "${TMP_BIN}"' EXIT

LDFLAGS="-s -w \
  -X main.Commit=${COMMIT} \
  -X main.BuildTime=${BUILD_TIME}"

echo "▶  building ./cmd/orbit …"
(
  cd "${MODULE_DIR}"
  CGO_ENABLED=0 go build -trimpath -ldflags "${LDFLAGS}" -o "${TMP_BIN}" ./cmd/orbit
)

# --- install -----------------------------------------------------------------
if [[ -w "$(dirname "${INSTALL_PATH}")" ]]; then
  install -m 0755 "${TMP_BIN}" "${INSTALL_PATH}"
else
  echo "▶  instalando em ${INSTALL_PATH} (sudo) …"
  sudo install -m 0755 "${TMP_BIN}" "${INSTALL_PATH}"
fi

# --- validate ----------------------------------------------------------------
echo "▶  orbit version:"
"${INSTALL_PATH}" version

echo "▶  orbit context-pack:"
"${INSTALL_PATH}" context-pack

echo "✅  build + install OK"
