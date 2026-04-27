#!/usr/bin/env bash
# tests/test_public_hygiene.sh — higiene pública do README.
#
# Verifica:
#   1. Nenhum nome antigo (motor-orbital, token-optimizer) no README.
#   2. Nenhum badge shields.io dinâmico de release (github/v/release) enquanto
#      não houver release público confirmado no código.
#   3. Todo badge shields.io aponta para o repo correto (IanVDev/orbit-engine).
#   4. Nenhum badge referencia outro caminho de repo.
#
# Fail-closed: qualquer violação → exit 1.
# Portabilidade: bash 3.2 (macOS) e bash 5 (Linux/CI).
# Uso: bash tests/test_public_hygiene.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
README="${REPO_ROOT}/README.md"
CORRECT_REPO="IanVDev/orbit-engine"

[[ -f "${README}" ]] || { echo "FAIL: ${README} não existe" >&2; exit 1; }

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "OK: $*"; }

# ── Check 1: nomes antigos ───────────────────────────────────────────────────
OLD_NAMES="motor-orbital motor_orbital token-optimizer token_optimizer"
for name in ${OLD_NAMES}; do
  if grep -qi "${name}" "${README}"; then
    fail "nome antigo '${name}' encontrado no README"
  fi
done
ok "Check 1: nenhum nome antigo no README"

# ── Check 2: badge dinâmico de release ausente ──────────────────────────────
# github/v/release é dinâmico e quebra se o repo for privado ou sem release.
# Remover esta linha quando existir release público confirmado.
if grep -q "shields.io/github/v/release" "${README}"; then
  fail "badge dinâmico de release (shields.io/github/v/release) encontrado no README — remova ou substitua por badge estático enquanto não houver release público"
fi
ok "Check 2: sem badge dinâmico de release"

# ── Check 3: badges shields.io apontam para repo correto ────────────────────
WRONG_REPO=$(
  grep -o 'shields\.io/github/[^)]*' "${README}" \
    | grep -v "${CORRECT_REPO}" || true
)
if [[ -n "${WRONG_REPO}" ]]; then
  echo "FAIL: badge shields.io aponta para repo errado:" >&2
  echo "${WRONG_REPO}" >&2
  exit 1
fi
ok "Check 3: todos os badges shields.io apontam para ${CORRECT_REPO}"

# ── Check 4: license badge não usa endpoint dinâmico do GitHub ──────────────
# shields.io/github/license depende da API do GitHub — quebra com repo privado.
# Use shields.io/badge/license-MIT em vez disso.
if grep -q "shields.io/github/license" "${README}"; then
  fail "badge dinâmico de license (shields.io/github/license) encontrado — substitua por badge estático: https://img.shields.io/badge/license-MIT-blue.svg"
fi
ok "Check 4: license badge é estático (não depende da API GitHub)"

echo ""
echo "OK: higiene pública — todos os 4 checks passaram"
