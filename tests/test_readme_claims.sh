#!/usr/bin/env bash
# tests/test_readme_claims.sh — enforcer do léxico de identidade do Orbit.
#
# Garante que a seção HERO do README.md (do início até o primeiro separador
# horizontal `---`) NÃO contenha vocabulário descartado da fase anterior do
# produto: "save", "economize", "optimize/optimization", "fewer tokens",
# "redução de tokens", "wasting tokens", "cost more", "83%".
#
# Falha com exit 1 se qualquer termo proibido for detectado, listando linha
# e termo. Roda standalone, sem dependências além de bash + grep + awk.
#
# Uso:
#   bash tests/test_readme_claims.sh
#
# Política: orbit é "evidência operacional", não "economia de tokens".

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
README="$REPO_ROOT/README.md"

if [[ ! -f "$README" ]]; then
  echo "FAIL: README.md não encontrado em $README" >&2
  exit 1
fi

# Extrai a seção HERO: tudo até o primeiro '---' em linha sozinha.
# awk: imprime até encontrar uma linha que é exatamente '---', então sai.
HERO="$(awk '/^---$/{exit} {print}' "$README")"

if [[ -z "$HERO" ]]; then
  echo "FAIL: seção HERO vazia — README malformado" >&2
  exit 1
fi

# Termos proibidos no léxico do produto. Case-insensitive, regex estendido.
# Mantenha a lista pequena e específica — falsos positivos quebram refactors.
FORBIDDEN_PATTERNS=(
  '\bsave\b'
  '\bsaved\b'
  '\bsaving\b'
  '\bsavings\b'
  '\beconomize\b'
  '\beconomy\b'
  'optimi[sz]e'
  'optimi[sz]ation'
  'fewer tokens'
  'reduç[ãa]o de tokens'
  'token reduction'
  'wasting tokens'
  'cost more'
  '83%'
)

VIOLATIONS=0
for pattern in "${FORBIDDEN_PATTERNS[@]}"; do
  if matches="$(echo "$HERO" | grep -niE "$pattern" || true)"; then
    if [[ -n "$matches" ]]; then
      while IFS= read -r line; do
        echo "FAIL: termo proibido /$pattern/ na seção HERO do README:" >&2
        echo "      $line" >&2
        VIOLATIONS=$((VIOLATIONS + 1))
      done <<< "$matches"
    fi
  fi
done

if [[ $VIOLATIONS -gt 0 ]]; then
  echo "" >&2
  echo "Total de violações: $VIOLATIONS" >&2
  echo "Léxico aceito: detect, record, diagnose, observe, prove (e derivados)." >&2
  exit 1
fi

echo "PASS: seção HERO do README está alinhada ao léxico (detect/record/diagnose/observe/prove)."
