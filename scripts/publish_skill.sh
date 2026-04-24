#!/usr/bin/env bash
# scripts/publish_skill.sh — coordenador de release orbit-engine → orbit-prompt.
#
# Estratégia (fail-closed em cada passo):
#   P1. Input válido (REPO_VERSION = v<X>.<Y>.<Z>)
#   P2. orbit-engine: working tree limpo, branch main, gate G16 passa
#   P3. Extrai SKILL_VERSION do ZIP via git cat-file (SSOT)
#   P4. orbit-engine: tag prompt-skill-v<SKILL_VERSION> existe em HEAD OU será criada
#   P5. orbit-prompt: working tree limpo, branch main, remote correto
#   P6. orbit-prompt: REPO_VERSION ainda não existe como tag
#   P7. orbit-prompt: CHANGELOG.md tem entrada [REPO_VERSION] no topo (humano preparou)
#   Exec: cria tag engine (se necessário), copia artefato, atualiza README markers,
#         commita + tag orbit-prompt, push nos dois repos.
#
# Uso:
#   make publish-skill REPO_VERSION=v0.3.0
#   make publish-skill REPO_VERSION=v0.3.0 DRY_RUN=1
#   ORBIT_PROMPT_REPO=/path/custom bash scripts/publish_skill.sh ...
#
# Sem dependências externas (bash, git, sed, awk, unzip).
# Não duplica validação do gate G16 — invoca tests/test_skill_version_consistency.sh.

set -euo pipefail

ORBIT_ENGINE="$(cd "$(dirname "$0")/.." && pwd)"
ORBIT_PROMPT="${ORBIT_PROMPT_REPO:-$(dirname "${ORBIT_ENGINE}")/orbit-prompt}"
DRY_RUN="${DRY_RUN:-0}"
REPO_VERSION="${REPO_VERSION:-}"

SKILL_ZIP="orbit-prompt-skill/orbit-prompt.skill"
README_PROMPT="README.md"
CHANGELOG_PROMPT="CHANGELOG.md"
SKILL_TAG_PREFIX="prompt-skill-v"
EXPECTED_REMOTE="IanVDev/orbit-prompt"

fail() { echo "[FAIL] $*" >&2; exit 1; }
pass() { echo "[PASS] $*"; }
info() { echo "[INFO] $*"; }

# ── P1. Input ───────────────────────────────────────────────────────────
[[ -n "${REPO_VERSION}" ]] \
  || fail "REPO_VERSION não definido. Uso: make publish-skill REPO_VERSION=v0.3.0"
[[ "${REPO_VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] \
  || fail "REPO_VERSION deve ser v<X>.<Y>.<Z> — recebi: ${REPO_VERSION}"
REPO_VERSION_NUM="${REPO_VERSION#v}"
pass "input: REPO_VERSION=${REPO_VERSION}"

# ── P2. orbit-engine state ──────────────────────────────────────────────
cd "${ORBIT_ENGINE}"

[[ -z "$(git status --porcelain)" ]] \
  || fail "orbit-engine: working tree não está limpo"
[[ "$(git branch --show-current)" == "main" ]] \
  || fail "orbit-engine: não está na branch main"
pass "orbit-engine: clean + main"

# Gate G16 — fail-closed, não reimplementa lógica
if ! bash tests/test_skill_version_consistency.sh >/dev/null 2>&1; then
  echo "[FAIL] gate G16 não passou — saída:" >&2
  bash tests/test_skill_version_consistency.sh || true
  exit 1
fi
pass "G16 skill version consistency"

# ── P3. Extrai SKILL_VERSION ────────────────────────────────────────────
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

git cat-file -p ":${SKILL_ZIP}" > "${TMP}/skill.zip"
unzip -p "${TMP}/skill.zip" SKILL.md > "${TMP}/SKILL.md" 2>/dev/null \
  || fail "SKILL.md ausente em ${SKILL_ZIP}"

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

[[ "${SKILL_VER}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
  || fail "skill version inválida no frontmatter: '${SKILL_VER}'"
pass "skill version: ${SKILL_VER}"

# ── P4. orbit-engine: tag da skill ──────────────────────────────────────
SKILL_TAG="${SKILL_TAG_PREFIX}${SKILL_VER}"
CREATE_ENGINE_TAG=0

if git rev-parse --verify "refs/tags/${SKILL_TAG}" >/dev/null 2>&1; then
  TAG_COMMIT=$(git rev-parse "${SKILL_TAG}^{}" 2>/dev/null || git rev-parse "${SKILL_TAG}")
  HEAD_COMMIT=$(git rev-parse HEAD)
  [[ "${TAG_COMMIT}" == "${HEAD_COMMIT}" ]] \
    || fail "tag ${SKILL_TAG} aponta ${TAG_COMMIT:0:7}, mas HEAD=${HEAD_COMMIT:0:7}"
  pass "orbit-engine tag ${SKILL_TAG} já existe em HEAD"
else
  CREATE_ENGINE_TAG=1
  info "orbit-engine: tag ${SKILL_TAG} será criada em HEAD"
fi

# ── P5. orbit-prompt state ──────────────────────────────────────────────
[[ -d "${ORBIT_PROMPT}/.git" ]] \
  || fail "orbit-prompt não encontrado em ${ORBIT_PROMPT} (defina ORBIT_PROMPT_REPO)"

cd "${ORBIT_PROMPT}"

REMOTE_URL=$(git remote get-url origin 2>/dev/null || true)
[[ "${REMOTE_URL}" == *"${EXPECTED_REMOTE}"* ]] \
  || fail "orbit-prompt remote inesperado: '${REMOTE_URL}' (esperado conter '${EXPECTED_REMOTE}')"
[[ -z "$(git status --porcelain)" ]] \
  || fail "orbit-prompt: working tree não está limpo"
[[ "$(git branch --show-current)" == "main" ]] \
  || fail "orbit-prompt: não está na branch main"
pass "orbit-prompt: clean + main + remote ok"

# ── P6. Tag de distribuição deve ser nova ───────────────────────────────
if git rev-parse --verify "refs/tags/${REPO_VERSION}" >/dev/null 2>&1; then
  fail "orbit-prompt: tag ${REPO_VERSION} já existe — use versão nova"
fi
pass "orbit-prompt: tag ${REPO_VERSION} disponível"

# ── P7. CHANGELOG preparado ─────────────────────────────────────────────
if ! head -20 "${CHANGELOG_PROMPT}" | grep -qE "^## \[${REPO_VERSION_NUM//./\\.}\]"; then
  cat >&2 <<EOF
[FAIL] CHANGELOG.md não tem entrada [${REPO_VERSION_NUM}] entre as 20 primeiras linhas.

Prepare antes de publicar:
  1. Edite ${ORBIT_PROMPT}/${CHANGELOG_PROMPT}
  2. Adicione no topo (após o primeiro separador ---):

     ## [${REPO_VERSION_NUM}] — $(date +%Y-%m-%d)

     ### Changed
     - <descreva o que mudou>

  3. Re-execute: make publish-skill REPO_VERSION=${REPO_VERSION}
EOF
  exit 1
fi
pass "CHANGELOG tem entrada [${REPO_VERSION_NUM}]"

# ── Plan ────────────────────────────────────────────────────────────────
cd "${ORBIT_ENGINE}"
ENGINE_HEAD=$(git rev-parse --short HEAD)
cd "${ORBIT_PROMPT}"
PROMPT_HEAD=$(git rev-parse --short HEAD)

echo ""
echo "══ publish-skill plan ══"
echo "  orbit-engine:  ${ORBIT_ENGINE} @ ${ENGINE_HEAD}"
echo "  orbit-prompt:  ${ORBIT_PROMPT} @ ${PROMPT_HEAD}"
echo "  skill version: ${SKILL_VER}"
echo "  repo version:  ${REPO_VERSION}"
echo ""
echo "  actions:"
[[ "${CREATE_ENGINE_TAG}" -eq 1 ]] && echo "    - orbit-engine: criar tag ${SKILL_TAG} em HEAD"
echo "    - orbit-prompt: copiar orbit-prompt.skill do orbit-engine"
echo "    - orbit-prompt: atualizar README markers (Repo=${REPO_VERSION}, Skill=v${SKILL_VER})"
echo "    - orbit-prompt: git add + commit + tag ${REPO_VERSION}"
echo "    - push: orbit-engine main --tags"
echo "    - push: orbit-prompt main --tags"
echo ""

if [[ "${DRY_RUN}" == "1" ]]; then
  echo "🟡 DRY_RUN=1 — nada executado"
  exit 0
fi

# ── Execute ─────────────────────────────────────────────────────────────
info "executando"

# orbit-engine: tag se necessário
if [[ "${CREATE_ENGINE_TAG}" -eq 1 ]]; then
  cd "${ORBIT_ENGINE}"
  git tag -a "${SKILL_TAG}" -m "orbit-prompt skill v${SKILL_VER}"
  info "orbit-engine: tag ${SKILL_TAG} criada"
fi

# orbit-prompt: copia artefato
cp "${ORBIT_ENGINE}/${SKILL_ZIP}" "${ORBIT_PROMPT}/orbit-prompt.skill"
info "orbit-prompt: artefato copiado"

# orbit-prompt: atualiza README markers (preservando espaçamento)
cd "${ORBIT_PROMPT}"
sed -i.bak -E "s/^(Repo:[[:space:]]+)v[0-9]+\.[0-9]+\.[0-9]+\$/\1${REPO_VERSION}/" "${README_PROMPT}"
sed -i.bak -E "s/^(Skill:[[:space:]]+)v[0-9]+\.[0-9]+\.[0-9]+\$/\1v${SKILL_VER}/" "${README_PROMPT}"
sed -i.bak -E "s/^(Version:[[:space:]]+)[0-9]+\.[0-9]+\.[0-9]+\$/\1${REPO_VERSION_NUM}/" "${README_PROMPT}"
rm -f "${README_PROMPT}.bak"
info "orbit-prompt: README markers atualizados"

# Sanity: working tree agora tem changes
[[ -n "$(git status --porcelain)" ]] \
  || fail "orbit-prompt: nada para commitar após update — release no-op (artefato/markers idênticos?)"

git add orbit-prompt.skill "${README_PROMPT}" "${CHANGELOG_PROMPT}"
git commit -m "feat: ${REPO_VERSION} — embed skill v${SKILL_VER}"
git tag -a "${REPO_VERSION}" -m "orbit-prompt ${REPO_VERSION} — embed skill v${SKILL_VER}"
info "orbit-prompt: commit + tag ${REPO_VERSION}"

# Push ordenado: engine primeiro (SSOT), prompt depois (consumidor)
cd "${ORBIT_ENGINE}"
git push origin main --tags
info "orbit-engine: push main + tags OK"

cd "${ORBIT_PROMPT}"
git push origin main --tags
info "orbit-prompt: push main + tags OK"

echo ""
echo "🟢 publish-skill: ${REPO_VERSION} (skill v${SKILL_VER})"
echo ""
