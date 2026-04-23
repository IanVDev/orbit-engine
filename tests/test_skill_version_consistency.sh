#!/usr/bin/env bash
# tests/test_skill_version_consistency.sh — consistência de versão da skill.
#
# SSOT: frontmatter `version:` dentro de SKILL.md (dentro do ZIP
# orbit-prompt-skill/orbit-prompt.skill). Derivados: marcador HTML no
# README externo e tag git prompt-skill-v<X.Y.Z> apontando para HEAD.
#
# Estratégia:
#   1. Extrai SKILL.md do ZIP via `git cat-file -p :<path>` (index staged).
#   2. Parseia `version: X.Y.Z` do frontmatter YAML.
#   3. Lê marcador `<!-- SKILL_VERSION: X.Y.Z -->` do README (via index).
#   4. Se existe tag `prompt-skill-v*` apontando para HEAD, compara com SSOT.
#   5. Divergência em qualquer par → FAIL.
#
# Fail-closed: toda inconsistência encerra com exit 1 e motivo explícito.
# Sem dependências externas (bash + grep + awk + sed + unzip + git).
#
# Uso: bash tests/test_skill_version_consistency.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${REPO_ROOT}"

SKILL_ZIP_PATH="orbit-prompt-skill/orbit-prompt.skill"
README_PATH="orbit-prompt-skill/PROMPT-SKILL-README.md"
TAG_PREFIX="prompt-skill-v"

# ── 1. Paths presentes no index ──────────────────────────────────────────
if ! git cat-file -e ":${SKILL_ZIP_PATH}" 2>/dev/null; then
  echo "FAIL: ${SKILL_ZIP_PATH} não está no git index (stage o arquivo)" >&2
  exit 1
fi
if ! git cat-file -e ":${README_PATH}" 2>/dev/null; then
  echo "FAIL: ${README_PATH} não está no git index (stage o arquivo)" >&2
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

# ── 2. Extrai SKILL.md do ZIP staged ─────────────────────────────────────
git cat-file -p ":${SKILL_ZIP_PATH}" > "${TMP}/skill.zip"

if ! unzip -p "${TMP}/skill.zip" SKILL.md > "${TMP}/SKILL.md" 2>/dev/null; then
  echo "FAIL: SKILL.md não encontrado dentro de ${SKILL_ZIP_PATH}" >&2
  exit 1
fi

# Parseia version do frontmatter YAML (entre os dois '---' do topo).
# Aceita version: 1.2.3, version: "1.2.3", version: '1.2.3'.
SKILL_VER=$(awk '
  /^---[[:space:]]*$/ { fm++; if (fm == 2) exit; next }
  fm == 1 && /^version:[[:space:]]/ {
    v = $0
    sub(/^version:[[:space:]]+/, "", v)
    gsub(/[[:space:]"'"'"']+/, "", v)
    print v
    exit
  }
' "${TMP}/SKILL.md")

if [[ -z "${SKILL_VER}" ]]; then
  echo "FAIL: 'version:' ausente no frontmatter de SKILL.md" >&2
  exit 1
fi

if [[ ! "${SKILL_VER}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "FAIL: SKILL.md version não é semver X.Y.Z: '${SKILL_VER}'" >&2
  exit 1
fi

# ── 3. Marcador HTML no README (via index) ───────────────────────────────
README_VER=$(git cat-file -p ":${README_PATH}" | sed -n \
  's/^<!--[[:space:]]*SKILL_VERSION:[[:space:]]*\([0-9][0-9.]*\)[[:space:]]*-->.*/\1/p' \
  | head -1)

if [[ -z "${README_VER}" ]]; then
  echo "FAIL: marcador '<!-- SKILL_VERSION: X.Y.Z -->' ausente em ${README_PATH}" >&2
  exit 1
fi

if [[ ! "${README_VER}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "FAIL: README marker não é semver X.Y.Z: '${README_VER}'" >&2
  exit 1
fi

# ── 4. Comparação SKILL ↔ README ────────────────────────────────────────
if [[ "${SKILL_VER}" != "${README_VER}" ]]; then
  echo "FAIL: versões divergem — SKILL.md=${SKILL_VER}  README=${README_VER}" >&2
  echo "      SSOT é SKILL.md. Atualize o marcador do README para ${SKILL_VER}." >&2
  exit 1
fi

# ── 5. Tag git apontando para HEAD (opcional) ────────────────────────────
TAG=$(git tag --points-at HEAD --list "${TAG_PREFIX}*" | head -1 || true)

if [[ -n "${TAG}" ]]; then
  TAG_VER="${TAG#${TAG_PREFIX}}"
  if [[ ! "${TAG_VER}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "FAIL: tag ${TAG} não bate com ${TAG_PREFIX}X.Y.Z" >&2
    exit 1
  fi
  if [[ "${TAG_VER}" != "${SKILL_VER}" ]]; then
    echo "FAIL: tag ${TAG} (${TAG_VER}) ≠ SKILL.md (${SKILL_VER})" >&2
    exit 1
  fi
  echo "PASS: skill version ${SKILL_VER} — SKILL = README = tag ${TAG}"
  exit 0
fi

echo "PASS: skill version ${SKILL_VER} — SKILL = README (sem tag em HEAD)"
