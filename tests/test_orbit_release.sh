#!/usr/bin/env bash
# tests/test_orbit_release.sh — anti-regressão do subcomando `orbit release`.
#
# Estratégia: cria um repo git sintético em /tmp com "origin" local para
# testar o fluxo sem sair do sandbox. Valida 7 cenários:
#   1. VERSION malformada (v1)                          → exit 1
#   2. Não está na branch main                          → exit 1
#   3. Working tree sujo                                → exit 1
#   4. HEAD diverge de origin/main                      → exit 1
#   5. Tag já existe localmente                         → exit 1
#   6. Tag já existe em origin                          → exit 1
#   7. Happy path: cria tag + push com sucesso (skip-gate) → exit 0
#
# Fail-closed: qualquer cenário com resultado inesperado aborta com exit 1.
# Usa ORBIT_SKIP_GUARD=1 para bypass do startup-guard do binário.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-relcmd-XXXXXX)"
trap 'rm -rf "${TMP}"' EXIT

_fail() { echo "FAIL: $*" >&2; exit 1; }

# ── Build binário com version stamp ──────────────────────────────────────
COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
BIN="${TMP}/orbit"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v0.0.0-test -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null

# ── Setup: repo + origin bare local ──────────────────────────────────────
# Desabilita signing no setup sintético (não é commit do repo real).
git -c commit.gpgsign=false -c tag.gpgsign=false init --bare "${TMP}/origin.git" >/dev/null
git -c commit.gpgsign=false init --quiet "${TMP}/repo"

cd "${TMP}/repo"
git config user.email "test@orbit"
git config user.name  "test"
git config commit.gpgsign false
git config tag.gpgsign false
git commit --allow-empty -m "init" --quiet
git branch -M main
git remote add origin "${TMP}/origin.git"
git push origin main --quiet

# Helper: roda `orbit release` com SKIP_GUARD.
run_release() {
  ORBIT_SKIP_GUARD=1 "${BIN}" release "$@" 2>&1
}

# ── [1/7] VERSION malformada ─────────────────────────────────────────────
echo "── [1/7] VERSION malformada ──"
if out=$(run_release --skip-gate "v1"); then
  _fail "cenário 1 deveria falhar (VERSION malformada)"
fi
echo "${out}" | grep -q "VERSION inválida" || { echo "${out}"; _fail "cenário 1: msg errada"; }
echo "    ✓ FAIL corretamente"

# ── [2/7] branch != main ─────────────────────────────────────────────────
echo "── [2/7] branch != main ──"
git checkout -b feat/wrong --quiet
if out=$(run_release --skip-gate "v0.1.2"); then
  git checkout main --quiet
  _fail "cenário 2 deveria falhar (branch != main)"
fi
echo "${out}" | grep -q "só pode ser feito a partir de main" \
  || { echo "${out}"; git checkout main --quiet; _fail "cenário 2: msg errada"; }
git checkout main --quiet
git branch -D feat/wrong --quiet 2>/dev/null || true
echo "    ✓ FAIL corretamente"

# ── [3/7] working tree sujo ──────────────────────────────────────────────
echo "── [3/7] working tree sujo ──"
echo "dirty" > dirty.txt
if out=$(run_release --skip-gate "v0.1.2"); then
  _fail "cenário 3 deveria falhar (tree sujo)"
fi
echo "${out}" | grep -q "working tree sujo" \
  || { echo "${out}"; _fail "cenário 3: msg errada"; }
rm dirty.txt
echo "    ✓ FAIL corretamente"

# ── [4/7] HEAD diverge de origin/main ────────────────────────────────────
echo "── [4/7] HEAD diverge de origin/main ──"
git commit --allow-empty -m "ahead" --quiet
if out=$(run_release --skip-gate "v0.1.2"); then
  git reset --hard HEAD~1 --quiet
  _fail "cenário 4 deveria falhar (HEAD ahead)"
fi
echo "${out}" | grep -q "diverge de origin/main" \
  || { echo "${out}"; git reset --hard HEAD~1 --quiet; _fail "cenário 4: msg errada"; }
git reset --hard HEAD~1 --quiet
echo "    ✓ FAIL corretamente"

# ── [5/7] tag já existe localmente ───────────────────────────────────────
echo "── [5/7] tag já existe localmente ──"
git tag v0.1.2
if out=$(run_release --skip-gate "v0.1.2"); then
  git tag -d v0.1.2 >/dev/null
  _fail "cenário 5 deveria falhar (tag local existe)"
fi
echo "${out}" | grep -q "já existe localmente" \
  || { echo "${out}"; git tag -d v0.1.2 >/dev/null; _fail "cenário 5: msg errada"; }
git tag -d v0.1.2 >/dev/null
echo "    ✓ FAIL corretamente"

# ── [6/7] tag já existe em origin ────────────────────────────────────────
echo "── [6/7] tag já existe em origin ──"
git tag v0.1.3
git push origin v0.1.3 --quiet
git tag -d v0.1.3 >/dev/null
if out=$(run_release --skip-gate "v0.1.3"); then
  _fail "cenário 6 deveria falhar (tag em origin)"
fi
echo "${out}" | grep -q "já existe em origin" \
  || { echo "${out}"; _fail "cenário 6: msg errada"; }
echo "    ✓ FAIL corretamente"

# ── [7/7] happy path ─────────────────────────────────────────────────────
echo "── [7/7] happy path ──"
out=$(run_release --skip-gate "v0.1.4") \
  || { echo "${out}"; _fail "cenário 7 (happy) deveria passar"; }
echo "${out}" | grep -q "RELEASE: v0.1.4 publicado em origin" \
  || { echo "${out}"; _fail "cenário 7: msg de sucesso ausente"; }
# Confirma tag no bare origin.
git ls-remote --tags origin 2>/dev/null | grep -q "refs/tags/v0.1.4" \
  || _fail "cenário 7: tag não chegou em origin"
echo "    ✓ PASS — tag v0.1.4 criada e pushada"

echo ""
echo "PASS: orbit release (7 cenários — 6 fail-closed + 1 happy)"
