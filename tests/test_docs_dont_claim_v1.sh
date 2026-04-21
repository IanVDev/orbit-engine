#!/usr/bin/env bash
# tests/test_docs_dont_claim_v1.sh — trava contra confusão de contratos.
#
# Pivô para Fase 1 (CLI v0.1.x) foi feito no CHANGELOG.md, mas docs na raiz
# ainda podiam referenciar o gate do Produto B (`make gate-v1`,
# `prelaunch_gate.sh → GO`) como se fosse o release atual. Isto confunde
# novos contribuidores e re-introduz a ambiguidade de escopo.
#
# Este teste garante que docs PÚBLICOS na raiz não apontem o gate errado
# como release gate da CLI.
#
# Docs em escopo (raiz pública — aquilo que o usuário lê primeiro):
#   README.md, CHANGELOG.md, ONBOARDING.md, QUICK-START.md, TUTORIAL.md,
#   GUIDE.md, CONTRIBUTING.md, VALIDATION.md
#
# Docs EXCLUÍDOS (escopo Produto B, movidos para docs/server-stack/):
#   LAUNCH_READINESS.md, V1_CONTRACT.md, V1_RELEASE_PLAN.md, RELEASE_PHASE_15.md
#
# Fail-closed: qualquer referência proibida → exit 1.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${REPO_ROOT}"

# CHANGELOG.md é histórico por definição — contém referências legítimas ao
# milestone v1.0 interno (marcado explicitamente "never tagged"). Fica fora.
PUBLIC_DOCS=(
  README.md
  ONBOARDING.md
  QUICK-START.md
  TUTORIAL.md
  GUIDE.md
  CONTRIBUTING.md
  VALIDATION.md
  FEEDBACK.md
  SELF-EVOLUTION.md
)

# Regexes proibidas nos docs públicos.
# - make gate-v1 / make gate-release: gates do Produto B
# - prelaunch_gate.sh.*GO-?NO-?GO: documento do milestone v1.0 interno
# - "v1.0.0 ready" / "v1.0 ready": afirmação sobre milestone nunca tagueado
FORBIDDEN=(
  'make gate-v1'
  'make gate-release'
  'prelaunch_gate\.sh.*GO.?NO.?GO'
  'VEREDITO:.?GO'
  'v1\.0\.0 ready'
  'v1\.0 ready'
)

VIOLATIONS=0
for doc in "${PUBLIC_DOCS[@]}"; do
  [[ -f "${doc}" ]] || continue
  for pat in "${FORBIDDEN[@]}"; do
    if grep -nE "${pat}" "${doc}" >/dev/null 2>&1; then
      echo "FAIL: ${doc} referencia gate do Produto B (pat: ${pat})" >&2
      grep -nE "${pat}" "${doc}" | sed 's/^/    /' >&2
      VIOLATIONS=$((VIOLATIONS + 1))
    fi
  done
done

if [[ "${VIOLATIONS}" -gt 0 ]]; then
  echo "" >&2
  echo "Total de violações: ${VIOLATIONS}" >&2
  echo "Docs públicos devem apontar 'make gate-cli' e docs/CLI_RELEASE_GATE.md" >&2
  echo "Se a referência ao Produto B é legítima, mova o texto para docs/server-stack/" >&2
  exit 1
fi

echo "PASS: docs públicos alinhados à CLI (${#PUBLIC_DOCS[@]} arquivos verificados)"
