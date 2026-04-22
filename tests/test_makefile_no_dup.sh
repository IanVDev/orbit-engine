#!/usr/bin/env bash
# tests/test_makefile_no_dup.sh — trava contra warnings de "overriding recipe".
#
# Um alvo duplicado no Makefile faz `make` escolher o último silenciosamente.
# Isso já aconteceu (orbit-check SSH sobrescreveu orbit-check produção — ver
# histórico). Este teste garante que `make` nunca emite o warning.
#
# Fail-closed: qualquer warning → exit 1.
#
# Nota: use `make -n <alvo>` em um alvo que existe no repo para forçar
# o parse completo do Makefile sem executar recipes.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${REPO_ROOT}"

# Alvos que existem no Makefile — escolher um barato.
OUT="$(make -n gate-cli 2>&1 || true)"

if echo "${OUT}" | grep -qE "warning: (overriding|ignoring old) recipe"; then
  echo "FAIL: Makefile tem alvos duplicados — ver warnings:" >&2
  echo "${OUT}" | grep -E "warning:" >&2
  exit 1
fi

echo "PASS: Makefile sem alvos duplicados"
