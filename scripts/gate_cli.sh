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

# date +%s%3N não funciona no macOS (BSD date não suporta %N).
# python3 já é pré-requisito do gate e funciona em Linux e macOS.
_now_ms() { python3 -c "import time; print(int(time.time()*1000))"; }

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

# ── G6: Contrato do log estruturado v1 ────────────────────────────────
# I16: em CI (GITHUB_ACTIONS=true), ORBIT_SKIP_GUARD requer double-ack.
run G6_log_contract bash -c "ORBIT_SKIP_GUARD=1 ORBIT_SKIP_GUARD_IN_CI=1 bash tests/test_log_contract.sh"

# ── G8: Makefile sem targets duplicados ──────────────────────────────
run G8_no_mk_dup bash tests/test_makefile_no_dup.sh

# ── G11: paridade contagem gates script ↔ doc ─────────────────────────
run G11_gate_doc_parity bash tests/test_gate_doc_parity.sh

# ── G13: integrity — body_hash + chain + merkle + 1-byte tamper ──────
# I16: em CI (GITHUB_ACTIONS=true), ORBIT_SKIP_GUARD requer double-ack.
run G13_integrity bash -c "ORBIT_SKIP_GUARD=1 ORBIT_SKIP_GUARD_IN_CI=1 bash tests/test_integrity.sh"

# ── G16: consistência de versão da prompt skill ──────────────────────
run G16_skill_version bash tests/test_skill_version_consistency.sh

# ── G17: slash command bridge — orbit-prompt ─────────────────────────
run G17_slash_command_bridge bash scripts/check_claude_slash_command_bridge.sh

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
