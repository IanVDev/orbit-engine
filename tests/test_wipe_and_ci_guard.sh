#!/usr/bin/env bash
# tests/test_wipe_and_ci_guard.sh — I15 HISTORY_ANCHOR + I16 GUARD_HARDENING.
#
# Usa binário real. ORBIT_HOME e ORBIT_ANCHOR_PATH isolados em tempdir.
# Fail-closed: qualquer assertiva falha → exit 1.
#
#   [1/2] TestFailsOnHistoryWipe — I15
#         roda 2 vezes (anchor criado); rm -rf ORBIT_HOME; próximo run
#         DEVE abortar com CRITICAL.
#
#   [2/2] TestFailsWhenGuardBypassedInCI — I16
#         CI=true + ORBIT_SKIP_GUARD=1 SEM ack extra → deve falhar.
#         Adicionar ORBIT_SKIP_GUARD_IN_CI=1 → passa.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-wca-XXXXXX)"
trap 'rm -rf "${TMP}"' EXIT

_fail() { echo "FAIL: $*" >&2; exit 1; }

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
BIN="${TMP}/orbit"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v0.0.0-wca -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null

BIN_NO_STAMP="${TMP}/orbit-nostamp"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -o "${BIN_NO_STAMP}" ./cmd/orbit) >/dev/null

# ── [1/2] History wipe ───────────────────────────────────────────────────
echo "── [1/2] TestFailsOnHistoryWipe ──"
H1="${TMP}/h1"
A1="${TMP}/a1"

# 2 runs → anchor gravado + logs criados
for i in 1 2; do
  ORBIT_HOME="${H1}" ORBIT_ANCHOR_PATH="${A1}" "${BIN}" run echo "r${i}" >/dev/null 2>&1 \
    || _fail "run ${i} falhou no setup"
done
[[ -f "${A1}" ]] || _fail "anchor não foi criado em ${A1}"
TOTAL=$(python3 -c "import json;print(json.load(open('${A1}'))['total_runs'])")
[[ "${TOTAL}" == "2" ]] || _fail "anchor total_runs=${TOTAL} (esperado 2)"

# Wipe
rm -rf "${H1}"

# Próximo run DEVE abortar com CRITICAL
if ORBIT_HOME="${H1}" ORBIT_ANCHOR_PATH="${A1}" "${BIN}" run echo ok >"${TMP}/wipe.out" 2>&1; then
  cat "${TMP}/wipe.out"
  _fail "cenário wipe deveria abortar, mas run teve sucesso"
fi
grep -q "CRITICAL: history wipe detected" "${TMP}/wipe.out" \
  || { cat "${TMP}/wipe.out"; _fail "mensagem CRITICAL de wipe não apareceu"; }

# Recovery path documentado: rm anchor → próximo run passa
rm -f "${A1}"
ORBIT_HOME="${H1}" ORBIT_ANCHOR_PATH="${A1}" "${BIN}" run echo recovery >/dev/null 2>&1 \
  || _fail "após rm anchor, recovery run deveria passar"
echo "    ✓ wipe detectado + recovery documentado funciona"

# ── [2/2] CI bypass hardening ────────────────────────────────────────────
echo "── [2/2] TestFailsWhenGuardBypassedInCI ──"
H2="${TMP}/h2"

# Sub-cenário A: CI=true + ORBIT_SKIP_GUARD=1 SEM ack → deve falhar.
# Usa binário sem stamp (que acionaria o guard normalmente) para garantir
# que o teste prova o PATH do bypass, não o path normal.
if CI=true ORBIT_SKIP_GUARD=1 ORBIT_HOME="${H2}" \
     "${BIN_NO_STAMP}" run echo x >"${TMP}/ci.out" 2>&1; then
  cat "${TMP}/ci.out"
  _fail "cenário 2A: CI+skip deveria falhar sem ORBIT_SKIP_GUARD_IN_CI=1"
fi
grep -q "CI bypass não autorizado\|ORBIT_SKIP_GUARD=1 em CI exige" "${TMP}/ci.out" \
  || { cat "${TMP}/ci.out"; _fail "cenário 2A: mensagem de bloqueio de CI bypass não apareceu"; }
echo "    ✓ CI+skip sem ack → FAIL corretamente"

# Sub-cenário B: double-ack permite bypass
if ! CI=true ORBIT_SKIP_GUARD=1 ORBIT_SKIP_GUARD_IN_CI=1 ORBIT_HOME="${H2}" \
       "${BIN_NO_STAMP}" run echo x >"${TMP}/ci2.out" 2>&1; then
  cat "${TMP}/ci2.out"
  _fail "cenário 2B: double-ack deveria permitir bypass em CI"
fi
echo "    ✓ CI+skip+ack → PASS (escape hatch explícito)"

# Sub-cenário C: sem CI, apenas ORBIT_SKIP_GUARD=1 segue permitido (dev local)
if ! ORBIT_SKIP_GUARD=1 ORBIT_HOME="${H2}" "${BIN_NO_STAMP}" run echo x >/dev/null 2>&1; then
  _fail "cenário 2C: skip fora de CI deveria funcionar (dev local)"
fi
echo "    ✓ skip fora de CI continua permitido"

echo ""
echo "PASS: wipe + ci guard (I15 + I16)"
