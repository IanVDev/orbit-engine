#!/usr/bin/env bash
# tests/test_git_verify_merge.sh — teste anti-regressão de `orbit git verify-merge`.
#
# Cenários testados (fail-closed em qualquer falha):
#   1. Commit com 1 parent (regular) → deve falhar com exit 1
#   2. Merge commit real (2 parents) → deve passar com exit 0
#   3. Subcomando desconhecido       → deve falhar com exit 1
#
# Requisitos: git, orbit no PATH.
# Offline-safe: sem rede necessária.

set -euo pipefail

ORBIT="${ORBIT:-orbit}"
export ORBIT_SKIP_GUARD=1
PASS=0
FAIL=0

pass() { echo "[PASS] $1"; PASS=$((PASS + 1)); }
fail() { echo "[FAIL] $1" >&2; FAIL=$((FAIL + 1)); }

# Verifica dependências antes de criar estado.
if ! command -v git >/dev/null 2>&1; then
  echo "[SKIP] git não encontrado"
  exit 0
fi
if ! command -v "${ORBIT}" >/dev/null 2>&1; then
  echo "[SKIP] orbit não encontrado em PATH"
  exit 0
fi

# ── Setup: repo temporário isolado ──────────────────────────────────────────
REPO=$(mktemp -d)
trap 'rm -rf "${REPO}"' EXIT

cd "${REPO}"
git init -q -b main
git config user.email "test@test"
git config user.name "test"
git config commit.gpgsign false

# Commit inicial em main.
git commit --allow-empty -q -m "initial"

# ── Cenário 1: commit regular (1 parent) deve falhar ────────────────────────
# Captura output separado do exit code (pipefail causaria falso negativo).
OUT1=$("${ORBIT}" git verify-merge 2>&1 || true)
if echo "${OUT1}" | grep -q "NOT_A_MERGE"; then
  pass "commit regular detectado como NOT_A_MERGE"
else
  fail "commit regular devia produzir NOT_A_MERGE"
fi

if ! "${ORBIT}" git verify-merge >/dev/null 2>&1; then
  pass "commit regular retorna exit 1"
else
  fail "commit regular devia retornar exit 1, retornou exit 0"
fi

# ── Cenário 2: merge commit real (2 parents) deve passar ────────────────────
git checkout -q -b feature
git commit --allow-empty -q -m "feature work"
git checkout -q main
git merge --no-ff -q -m "merge feature" feature

if "${ORBIT}" git verify-merge 2>&1 | grep -q "MERGE_VALID"; then
  pass "merge commit detectado como MERGE_VALID"
else
  fail "merge commit devia produzir MERGE_VALID"
fi

if "${ORBIT}" git verify-merge >/dev/null 2>&1; then
  pass "merge commit retorna exit 0"
else
  fail "merge commit devia retornar exit 0, retornou exit 1"
fi

# ── Cenário 3: --ref aponta para commit anterior ao merge (1 parent) ────────
if ! "${ORBIT}" git verify-merge --ref HEAD~1 >/dev/null 2>&1; then
  pass "--ref HEAD~1 (commit regular) retorna exit 1"
else
  fail "--ref HEAD~1 devia retornar exit 1"
fi

# ── Cenário 4: subcomando desconhecido ──────────────────────────────────────
if ! "${ORBIT}" git unknown-cmd >/dev/null 2>&1; then
  pass "subcomando desconhecido retorna exit 1"
else
  fail "subcomando desconhecido devia retornar exit 1"
fi

# ── Cenário 5: ref inválida ──────────────────────────────────────────────────
if ! "${ORBIT}" git verify-merge --ref "nonexistent-ref-xyz" >/dev/null 2>&1; then
  pass "ref inválida retorna exit 1"
else
  fail "ref inválida devia retornar exit 1"
fi

# ── Relatório ────────────────────────────────────────────────────────────────
echo ""
echo "git verify-merge: ${PASS} passed, ${FAIL} failed"

if [[ "${FAIL}" -gt 0 ]]; then
  exit 1
fi
