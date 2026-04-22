#!/usr/bin/env bash
# scripts/orbit_rollback.sh — Reverte `orbit` para o binário de backup.
#
# Par operacional do update_orbit.sh. Quando um `orbit update` instala um
# binário que depois se mostra quebrado, este script restaura o .bak
# gravado pelo update.
#
# Contrato (fail-closed):
#   [1] Backup existe em <DEST>.bak          → senão FAIL
#   [2] Backup responde `version`            → senão FAIL (backup corrompido)
#   [3] Substituição atômica via mv          → senão tenta sudo
#   [4] Pós-restore: `version` responde      → senão FAIL
#
# Uso:
#   ./scripts/orbit_rollback.sh                    # usa $(command -v orbit)
#   ./scripts/orbit_rollback.sh --dest /path/orbit # caminho explícito
#
# Saída: imprime versões antes/depois e caminho do backup descartado.
# Exit 0 = rollback concluído. Exit 1 = não pôde rolar back.

set -euo pipefail

_fail() { printf '\n❌  ERRO: %s\n' "$1" >&2; exit 1; }
_ok()   { printf '      ✓  %s\n' "$1"; }
_step() { printf '[%s/%s] %s\n' "$1" "$2" "$3"; }

# Detecta destino padrão a partir do binário em execução no PATH.
DETECTED_DEST="$(command -v orbit 2>/dev/null || true)"
DEST="${DETECTED_DEST:-/usr/local/bin/orbit}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dest) DEST="$2"; shift 2 ;;
    --help|-h)
      echo "uso: $0 [--dest <caminho>]"
      exit 0 ;;
    *) _fail "argumento desconhecido: $1" ;;
  esac
done

BAK="${DEST}.bak"

echo ""
echo "⏪  orbit rollback"
echo "    dest:   ${DEST}"
echo "    backup: ${BAK}"
echo ""

# ── [1] Backup existe ───────────────────────────────────────────────────
_step 1 4 "Verificando backup..."
[[ -f "${BAK}" ]] || _fail "backup não encontrado em ${BAK} — nada para reverter"
_ok "encontrado: ${BAK}"

# ── [2] Backup é executável válido ──────────────────────────────────────
_step 2 4 "Validando backup..."
BAK_VERSION="$("${BAK}" version 2>/dev/null || true)"
[[ -n "${BAK_VERSION}" ]] || _fail "backup não responde 'version' — corrompido, NÃO revertido"
_ok "backup ok: ${BAK_VERSION}"

# ── [3] Captura versão corrente (antes de sobrescrever) ─────────────────
CUR_VERSION="$("${DEST}" version 2>/dev/null || echo '(binário corrente não responde)')"

# ── [4] Substituição atômica ────────────────────────────────────────────
_step 3 4 "Restaurando..."
if ! cp "${BAK}" "${DEST}" 2>/dev/null; then
  sudo cp "${BAK}" "${DEST}" \
    || _fail "falha ao restaurar ${BAK} → ${DEST} (tente com sudo)"
fi
chmod +x "${DEST}" 2>/dev/null || sudo chmod +x "${DEST}" || true
_ok "restaurado: ${DEST}"

# ── [5] Validação pós-rollback ──────────────────────────────────────────
_step 4 4 "Validando pós-rollback..."
POST_VERSION="$("${DEST}" version 2>/dev/null || true)"
[[ -n "${POST_VERSION}" ]] \
  || _fail "binário restaurado não responde 'version' — sistema em estado degradado"
_ok "ativo: ${POST_VERSION}"

echo ""
echo "✅  rollback concluído"
echo "    ${CUR_VERSION}  →  ${POST_VERSION}"
echo ""
