#!/usr/bin/env bash
# tests/test_skill_contract.sh — contrato estrutural do SKILL.md.
#
# Justificativa:
#   Os 18 evals em tests/run_tests.py validam a SAÍDA da skill (formato
#   DIAGNOSIS/ACTIONS/DO NOT DO NOW em contextos específicos). Ninguém valida
#   que o próprio skill/SKILL.md contém as seções invariantes do contrato.
#   Remover "## Observable Patterns", "## Silence Rule" ou o "MANDATORY
#   PRE-RESPONSE RULE" passa em CI hoje — e quebra a skill silenciosamente.
#
# O que este teste trava (qualquer marker ausente → FAIL):
#   - Frontmatter YAML: name, version, cli_compat (abertura + fechamento ---).
#   - Regra ativa: "MANDATORY PRE-RESPONSE RULE".
#   - Seções-núcleo: Observable Patterns, Output Format, Rules, Gating Rules,
#     Silence Rule.
#   - Tokens do output format: DIAGNOSIS, ACTIONS, DO NOT DO NOW.
#
# Fail-closed: qualquer ausência → exit 1.
# Uso: bash tests/test_skill_contract.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SKILL="${REPO_ROOT}/skill/SKILL.md"

[[ -f "${SKILL}" ]] || { echo "FAIL: ${SKILL} não existe" >&2; exit 1; }

# Cada entrada é "descrição|regex". Regex compatível com grep -E.
REQUIRED=(
  "frontmatter abertura|^---$"
  "name: orbit-engine|^name: orbit-engine$"
  "version field|^version: "
  "cli_compat field|^cli_compat: "
  "MANDATORY PRE-RESPONSE RULE|MANDATORY PRE-RESPONSE RULE"
  "## Observable Patterns|^## Observable Patterns$"
  "## Output Format|^## Output Format$"
  "## Rules|^## Rules$"
  "## Gating Rules|^## Gating Rules$"
  "## Silence Rule|^## Silence Rule$"
  "token DIAGNOSIS|^DIAGNOSIS$"
  "token ACTIONS|^ACTIONS$"
  "token DO NOT DO NOW|^DO NOT DO NOW$"
)

VIOLATIONS=0
for entry in "${REQUIRED[@]}"; do
  desc="${entry%%|*}"
  pat="${entry#*|}"
  if ! grep -qE "${pat}" "${SKILL}"; then
    echo "FAIL: marker ausente — ${desc} (regex: ${pat})" >&2
    VIOLATIONS=$((VIOLATIONS + 1))
  fi
done

# Frontmatter deve fechar: precisa haver EXATAMENTE 2 linhas "^---$" no começo.
OPENS=$(head -50 "${SKILL}" | grep -cE '^---$' || true)
if [[ "${OPENS}" -lt 2 ]]; then
  echo "FAIL: frontmatter YAML não fecha (esperado 2x '---' nas primeiras 50 linhas, achei ${OPENS})" >&2
  VIOLATIONS=$((VIOLATIONS + 1))
fi

if [[ "${VIOLATIONS}" -gt 0 ]]; then
  echo "" >&2
  echo "Total de violações: ${VIOLATIONS}" >&2
  echo "SKILL.md perdeu contrato. Se a remoção é intencional, atualize tests/test_skill_contract.sh." >&2
  exit 1
fi

echo "PASS: skill contract (${#REQUIRED[@]} markers + frontmatter fechado)"
