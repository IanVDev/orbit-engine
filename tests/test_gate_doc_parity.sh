#!/usr/bin/env bash
# tests/test_gate_doc_parity.sh — paridade entre scripts/gate_cli.sh e
# docs/CLI_RELEASE_GATE.md.
#
# Justificativa:
#   Adicionar um novo gate ao gate_cli.sh sem atualizar o doc (já aconteceu:
#   doc dizia "8 gates", script tinha 9). Este guard previne divergência
#   silenciosa — qualquer gate novo exige edição simultânea dos dois.
#
# Estratégia:
#   Conta invocações `run G<N>_...` em scripts/gate_cli.sh e linhas `| G<N>` na
#   tabela de gates de docs/CLI_RELEASE_GATE.md. Exige igualdade.
#
# Fail-closed: divergência → exit 1.
# Uso: bash tests/test_gate_doc_parity.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="${REPO_ROOT}/scripts/gate_cli.sh"
DOC="${REPO_ROOT}/docs/CLI_RELEASE_GATE.md"

[[ -f "${SCRIPT}" ]] || { echo "FAIL: ${SCRIPT} não existe" >&2; exit 1; }
[[ -f "${DOC}" ]]    || { echo "FAIL: ${DOC} não existe"    >&2; exit 1; }

# Gates executados no script: linhas começando com "run G<N>_...".
SCRIPT_COUNT=$(grep -cE '^run G[0-9]+_' "${SCRIPT}" || true)

# Gates documentados na tabela do doc: linhas começando com "| G<N> |".
DOC_COUNT=$(grep -cE '^\| G[0-9]+ \|' "${DOC}" || true)

if [[ "${SCRIPT_COUNT}" -eq 0 ]]; then
  echo "FAIL: gate_cli.sh não tem 'run G<N>_...' (padrão inesperado)" >&2
  exit 1
fi

if [[ "${SCRIPT_COUNT}" != "${DOC_COUNT}" ]]; then
  echo "FAIL: gate paridade — script tem ${SCRIPT_COUNT} gates, doc tem ${DOC_COUNT}" >&2
  echo "" >&2
  echo "Gates no script:" >&2
  grep -E '^run G[0-9]+_' "${SCRIPT}" | sed 's/^/  /' >&2
  echo "" >&2
  echo "Gates no doc:" >&2
  grep -E '^\| G[0-9]+ \|' "${DOC}" | sed 's/^/  /' >&2
  exit 1
fi

echo "PASS: gate doc parity (${SCRIPT_COUNT} gates em ambos)"
