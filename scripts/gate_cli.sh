#!/usr/bin/env bash
# scripts/gate_cli.sh — Prod Gate v1 para o binário `orbit` (CLI).
#
# Contrato único, fail-closed, offline. Roda em < 120s em ambiente limpo.
# Se qualquer Gi = FAIL → exit 1. Todos = PASS → exit 0.
#
# Cada gate emite uma linha para gate_report.json (JSON array, ao final).
#
# Uso:
#   bash scripts/gate_cli.sh
#
# Saída:
#   gate_report.json  (JSON array com {gate, status, duration_ms, tail?})
#   stdout            ([PASS]/[FAIL] por gate + veredito final)
#
# Pré-requisitos:
#   go (versão compatível com tracking/go.mod)
#   python3
#   bash
#
# NÃO REQUER: rede, Prometheus, Grafana, Docker, Alertmanager.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

REPORT="${REPO_ROOT}/gate_report.json"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

# GOTOOLCHAIN=local: forçar uso do Go instalado (sem fetch de toolchain).
# Se a versão local for insuficiente, o gate falha honestamente em G1.
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"

FAIL=0
ENTRIES=()

_now_ms() { date +%s%3N; }

run() {
  local name="$1"; shift
  local out="${TMP}/${name}.out"
  local start end dur status
  start=$(_now_ms)
  if "$@" >"${out}" 2>&1; then
    status="PASS"
  else
    status="FAIL"
    FAIL=1
  fi
  end=$(_now_ms)
  dur=$(( end - start ))

  # Escapa o tail do output para caber em JSON.
  local tail_json
  tail_json=$(tail -30 "${out}" | python3 -c \
    'import json,sys;print(json.dumps(sys.stdin.read()))' 2>/dev/null || echo '""')

  ENTRIES+=("{\"gate\":\"${name}\",\"status\":\"${status}\",\"duration_ms\":${dur},\"tail\":${tail_json}}")

  if [[ "${status}" == "PASS" ]]; then
    printf '  [PASS] %-28s (%dms)\n' "${name}" "${dur}"
  else
    printf '  [FAIL] %-28s (%dms)\n' "${name}" "${dur}"
    echo "  ── tail ─────────────────────────────"
    tail -20 "${out}" | sed 's/^/    /'
    echo "  ─────────────────────────────────────"
  fi
}

echo ""
echo "══ Prod Gate v1 — orbit CLI ══"
echo ""

# ── G1: Go tests (tracking/...) ────────────────────────────────────────
run G1_go_test bash -c 'cd tracking && go test ./... -count=1'

# ── G2: Invariante "Orbit não escreve fora de \$ORBIT_HOME" ───────────
run G2_no_user_writes bash tests/test_no_user_writes.sh

# ── G3: Léxico do README hero travado ─────────────────────────────────
run G3_readme_claims bash tests/test_readme_claims.sh

# ── G4: Evals Python (18 casos de ativação/silêncio do skill) ─────────
run G4_python_evals bash -c 'cd tests && python3 run_tests.py'

# ── G5: Smoke E2E — exercita o binário real como o usuário ───────────
run G5_smoke_e2e bash tests/smoke_e2e.sh

# ── G6: Contrato do log estruturado v1 ────────────────────────────────
run G6_log_contract bash tests/test_log_contract.sh

# ── G7: Rollback funcional ────────────────────────────────────────────
run G7_rollback bash tests/test_rollback.sh

# ── G8: Makefile sem targets duplicados ──────────────────────────────
run G8_no_mk_dup bash tests/test_makefile_no_dup.sh

# ── G9: docs públicos não apontam gate do Produto B ──────────────────
run G9_docs_scope bash tests/test_docs_dont_claim_v1.sh

# ── Relatório final ──────────────────────────────────────────────────

{
  echo "["
  local_joined=$(IFS=,; echo "${ENTRIES[*]}")
  # Re-emit com quebras de linha entre entradas para legibilidade.
  echo "${local_joined}" | sed 's/},{/},\n{/g'
  echo "]"
} > "${REPORT}"

echo ""
if [[ "${FAIL}" -eq 0 ]]; then
  echo "🟢 PROD GATE v1: PASS — ${#ENTRIES[@]} gates OK"
  echo "   report: ${REPORT}"
  echo ""
  exit 0
fi

echo "🔴 PROD GATE v1: FAIL — ver ${REPORT}"
echo ""
exit 1
