#!/usr/bin/env bash
# tests/test_readme_cli_parity.sh — paridade entre README.md e o switch
# de subcomandos em tracking/cmd/orbit/main.go.
#
# Justificativa:
#   O README listava `orbit logs prune` e `ORBIT_MAX_LOGS`, ambos
#   implementados no código mas NUNCA wired no main.go nem chamados em
#   runtime — falsa promessa para usuários públicos. Este guard previne
#   regressão: qualquer comando citado no README precisa ter um `case`
#   correspondente no main.go.
#
# Estratégia:
#   1. Extrai todos os comandos `orbit <subcmd>` do README.md.
#   2. Lê todos os `case "<subcmd>":` do main.go.
#   3. Falha se algum comando do README não tem case.
#
# Fail-closed: divergência → exit 1.
# Portabilidade: funciona em bash 3.2 (macOS) e bash 5 (Linux/CI).
# Uso: bash tests/test_readme_cli_parity.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
README="${REPO_ROOT}/README.md"
MAIN="${REPO_ROOT}/tracking/cmd/orbit/main.go"

[[ -f "${README}" ]] || { echo "FAIL: ${README} não existe" >&2; exit 1; }
[[ -f "${MAIN}" ]]   || { echo "FAIL: ${MAIN} não existe"   >&2; exit 1; }

# Subcomandos válidos do CLI: extrai literais de `case "<name>"` no main.go.
CLI_CMDS=$(
  grep -oE 'case "[a-z-]+"' "${MAIN}" \
    | sed -E 's/case "//; s/"$//' \
    | sort -u
)

# Subcomandos referenciados no README: linhas com `orbit <subcmd>`.
# Restringe a tokens [a-z-] para evitar pegar trechos como "orbit-engine".
README_CMDS=$(
  grep -oE '`orbit [a-z][a-z-]+' "${README}" \
    | awk '{print $2}' \
    | sort -u
)

# Allowlist: tokens que não precisam de case correspondente.
ALLOWLIST="help --help -h"

if [[ -z "${README_CMDS}" ]]; then
  echo "FAIL: nenhum comando \`orbit <subcmd>\` encontrado no README" >&2
  exit 1
fi

MISSING=""
README_COUNT=0
for cmd in ${README_CMDS}; do
  README_COUNT=$((README_COUNT + 1))

  # Skip allowlist
  skip=0
  for allowed in ${ALLOWLIST}; do
    if [[ "${cmd}" == "${allowed}" ]]; then skip=1; break; fi
  done
  [[ ${skip} -eq 1 ]] && continue

  found=0
  for cli in ${CLI_CMDS}; do
    if [[ "${cmd}" == "${cli}" ]]; then found=1; break; fi
  done

  if [[ ${found} -eq 0 ]]; then
    MISSING="${MISSING} ${cmd}"
  fi
done

if [[ -n "${MISSING}" ]]; then
  echo "FAIL: comandos citados no README mas ausentes do switch em main.go:" >&2
  for cmd in ${MISSING}; do
    echo "  - orbit ${cmd}" >&2
  done
  echo "" >&2
  echo "Cases existentes em main.go:" >&2
  for cli in ${CLI_CMDS}; do
    echo "  - ${cli}" >&2
  done
  exit 1
fi

CLI_COUNT=$(echo "${CLI_CMDS}" | wc -w | tr -d ' ')
echo "OK: README/CLI parity (${README_COUNT} comandos no README validados contra ${CLI_COUNT} cases em main.go)"
