#!/usr/bin/env bash
# tests/test_rollback.sh — valida que orbit_rollback.sh restaura o .bak.
#
# Ciclo:
#   1. Builda binário "OLD" com Version=v0.0.0-old
#   2. Copia como .bak (simula update que deixou backup)
#   3. Builda binário "NEW" com Version=v0.0.0-new e sobrescreve
#   4. Confirma que NEW está ativo
#   5. Chama scripts/orbit_rollback.sh
#   6. Confirma que OLD está ativo
#
# Fail-closed: qualquer divergência → exit 1.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-rb-XXXXXX)"
trap 'rm -rf "${TMP}"' EXIT

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo rb)"
DEST="${TMP}/orbit"

build() {
  local version="$1" out="$2"
  (cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
    go build \
      -ldflags "-X main.Version=${version} -X main.Commit=${COMMIT} -X main.BuildTime=now" \
      -o "${out}" ./cmd/orbit) >/dev/null
}

_fail() { echo "FAIL: $*" >&2; exit 1; }

echo "── [1/5] build OLD (v0.0.0-old) ──"
build "v0.0.0-old" "${DEST}"
cp "${DEST}" "${DEST}.bak"
"${DEST}.bak" version 2>/dev/null | grep -q "v0.0.0-old" \
  || _fail ".bak não reporta v0.0.0-old"

echo "── [2/5] build NEW (v0.0.0-new) overwrite DEST ──"
build "v0.0.0-new" "${DEST}"
"${DEST}" version 2>/dev/null | grep -q "v0.0.0-new" \
  || _fail "DEST não reporta v0.0.0-new após overwrite"

echo "── [3/5] execute rollback ──"
bash "${REPO_ROOT}/scripts/orbit_rollback.sh" --dest "${DEST}" >/dev/null \
  || _fail "orbit_rollback.sh retornou erro"

echo "── [4/5] verify OLD restored ──"
"${DEST}" version 2>/dev/null | grep -q "v0.0.0-old" \
  || _fail "após rollback, DEST deveria reportar v0.0.0-old"

echo "── [5/5] verify fail-closed on missing backup ──"
rm -f "${DEST}.bak"
if bash "${REPO_ROOT}/scripts/orbit_rollback.sh" --dest "${DEST}" >/dev/null 2>&1; then
  _fail "rollback DEVE falhar quando .bak não existe"
fi

echo ""
echo "PASS: rollback (5 asserts)"
