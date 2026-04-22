#!/usr/bin/env bash
# tests/test_system_contract.sh — meta-teste do contrato do sistema.
#
# Justificativa:
#   docs/SYSTEM_CONTRACT.md enumera 11 invariantes + 7 garantias. Cada uma
#   aponta para um arquivo (código) e um teste (prova). Este meta-teste
#   valida que todas essas referências EXISTEM e (quando possível) PASSAM.
#
#   Isso impede regressão silenciosa: remover um teste sem atualizar o
#   contrato (ou vice-versa) quebra o gate G12.
#
# Fail-closed: qualquer referência ausente → exit 1.
# Não roda os testes referenciados (isso é trabalho dos outros gates do
# gate-cli). Valida apenas que existem e que o contrato os cita.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONTRACT="${REPO_ROOT}/docs/SYSTEM_CONTRACT.md"

[[ -f "${CONTRACT}" ]] || { echo "FAIL: ${CONTRACT} não existe" >&2; exit 1; }

_fail() { echo "FAIL: $*" >&2; exit 1; }

# ── Invariantes: cada uma precisa ter o par código+teste existentes ─────
# Formato: INVARIANT | code_path | test_path
INVARIANTS=(
  "I1|tests/test_no_user_writes.sh|tests/test_no_user_writes.sh"
  "I2|tracking/cmd/orbit/run.go|tests/smoke_e2e.sh"
  "I3|tracking/cmd/orbit/run.go|tests/test_log_contract.sh"
  "I4|tracking/cmd/orbit/verify.go|tests/smoke_e2e.sh"
  "I5|tracking/cmd/orbit/logstore.go|tests/test_system_contract.sh"
  "I6|skill/SKILL.md|tests/test_skill_contract.sh"
  "I7|tracking/store.go|tracking/store_test.go"
  "I8|tracking/repo_hygiene_test.go|tracking/repo_hygiene_test.go"
  "I9|Makefile|tests/test_makefile_no_dup.sh"
  "I10|docs/server-stack|tests/test_docs_dont_claim_v1.sh"
  "I11|docs/CLI_RELEASE_GATE.md|tests/test_gate_doc_parity.sh"
  "I12|tracking/redact.go|tests/test_data_safety.sh"
  "I13|tracking/cmd/orbit/logstore.go|tests/test_data_safety.sh"
  "I14|tracking/cmd/orbit/startup_guard.go|tests/test_data_safety.sh"
  "I15|tracking/anchor.go|tests/test_wipe_and_ci_guard.sh"
  "I16|tracking/cmd/orbit/startup_guard.go|tests/test_wipe_and_ci_guard.sh"
  "I17|tracking/cmd/orbit/integrity.go|tests/test_integrity.sh"
  "I18|tracking/cmd/orbit/verify.go|tests/test_integrity.sh"
  "I19|tracking/cmd/orbit/merkle.go|tests/test_integrity.sh"
)

echo "── Validando ${#INVARIANTS[@]} invariantes do contrato ──"
for entry in "${INVARIANTS[@]}"; do
  IFS='|' read -r id code test <<<"${entry}"

  # (a) código referenciado existe
  [[ -e "${REPO_ROOT}/${code}" ]] \
    || _fail "${id}: caminho de código '${code}' não existe"

  # (b) teste referenciado existe
  [[ -e "${REPO_ROOT}/${test}" ]] \
    || _fail "${id}: teste '${test}' não existe"

  # (c) o próprio SYSTEM_CONTRACT.md cita o id
  grep -qE "^\| ${id} " "${CONTRACT}" \
    || _fail "${id}: não aparece na tabela §2 do SYSTEM_CONTRACT.md"

  echo "  ✓ ${id}  →  ${code}  ×  ${test}"
done

# ── Invariante I5 diretamente verificável aqui: perms no código ─────────
# I5 afirma que logstore.go grava 0600/0700. Se alguém relaxar para 0644,
# este meta-teste pega (não deixa essa violação silenciosa).
echo ""
echo "── I5: perms hard-coded em logstore.go ──"
grep -qE "MkdirAll\(dir, 0o700\)" "${REPO_ROOT}/tracking/cmd/orbit/logstore.go" \
  || _fail "I5: logstore.go não cria diretório com 0o700"
grep -qE "WriteFile\(path, payload, 0o600\)" "${REPO_ROOT}/tracking/cmd/orbit/logstore.go" \
  || _fail "I5: logstore.go não escreve arquivo com 0o600"
echo "  ✓ logstore.go grava perms 0o600/0o700 conforme I5"

# ── I12 diretamente verificável: run.go chama RedactSecrets ─────────────
echo ""
echo "── I12: run.go chama tracking.RedactSecrets ──"
grep -qE "tracking\.RedactSecrets\(" "${REPO_ROOT}/tracking/cmd/orbit/run.go" \
  || _fail "I12: run.go não invoca RedactSecrets — secret leak silencioso"
echo "  ✓ run.go redige output/args antes de persistir"

# ── I13 diretamente verificável: logstore.go chama pruneOldLogs ─────────
echo ""
echo "── I13: logstore.go chama pruneOldLogs após write ──"
grep -qE "pruneOldLogs\(dir\)" "${REPO_ROOT}/tracking/cmd/orbit/logstore.go" \
  || _fail "I13: logstore.go não chama pruneOldLogs — crescimento linear sem cap"
echo "  ✓ logstore.go aplica cap após cada write"

# ── I15 diretamente verificável: run.go chama SaveAnchor ────────────────
echo ""
echo "── I15: run.go chama tracking.SaveAnchor após write ──"
grep -qE "tracking\.SaveAnchor\(" "${REPO_ROOT}/tracking/cmd/orbit/run.go" \
  || _fail "I15: run.go não chama SaveAnchor — wipe não detectável"
grep -qE "enforceHistoryAnchor\(" "${REPO_ROOT}/tracking/cmd/orbit/main.go" \
  || _fail "I15: main.go não chama enforceHistoryAnchor — verify não acontece"
echo "  ✓ anchor é salvo após run e verificado antes de run"

# ── I16 diretamente verificável: guard tem check de CI + ack ────────────
echo ""
echo "── I16: startup_guard.go bloqueia ORBIT_SKIP_GUARD em CI sem ack ──"
grep -qE 'ORBIT_SKIP_GUARD_IN_CI' "${REPO_ROOT}/tracking/cmd/orbit/startup_guard.go" \
  || _fail "I16: startup_guard.go não referencia ORBIT_SKIP_GUARD_IN_CI"
grep -qE '"CI"' "${REPO_ROOT}/tracking/cmd/orbit/startup_guard.go" \
  || _fail "I16: startup_guard.go não detecta env CI"
echo "  ✓ guard hardening em CI presente"

# ── I17 diretamente verificável: run.go preenche BodyHash + verify checa ─
echo ""
echo "── I17: body_hash gravado e verificado ──"
grep -qE "CanonicalHash\(" "${REPO_ROOT}/tracking/cmd/orbit/run.go" \
  || _fail "I17: run.go não chama CanonicalHash — body_hash órfão"
grep -qE "verifyBodyHash\(" "${REPO_ROOT}/tracking/cmd/orbit/verify.go" \
  || _fail "I17: verify.go não chama verifyBodyHash — 1 byte alterado passaria"
echo "  ✓ body_hash gravado por run.go e checado por verify.go"

# ── I18 diretamente verificável: verify --chain wired em main.go ────────
echo ""
echo "── I18: verify --chain acessível via CLI ──"
grep -qE 'chain\s*:=\s*fs\.Bool\("chain"' "${REPO_ROOT}/tracking/cmd/orbit/main.go" \
  || _fail "I18: main.go não wired --chain flag"
grep -qE "runVerifyChain\(" "${REPO_ROOT}/tracking/cmd/orbit/main.go" \
  || _fail "I18: main.go não chama runVerifyChain"
echo "  ✓ --chain flag wired e dispatcha runVerifyChain"

# ── I19 diretamente verificável: anchor command wired em main.go ────────
echo ""
echo "── I19: orbit anchor acessível via CLI ──"
grep -qE '^\s*case "anchor":' "${REPO_ROOT}/tracking/cmd/orbit/main.go" \
  || _fail "I19: main.go não tem case anchor — merkle/anchor código órfão"
grep -qE "runAnchor\(" "${REPO_ROOT}/tracking/cmd/orbit/main.go" \
  || _fail "I19: main.go não chama runAnchor"
echo "  ✓ orbit anchor wired e invoca runAnchor"

# ── Garantias operacionais: existência dos artefatos ────────────────────
GUARANTEES=(
  "O1|scripts/gate_cli.sh|tests/test_system_contract.sh"
  "O2|scripts/orbit_rollback.sh|tests/test_rollback.sh"
  "O3|scripts/install_remote.sh|tests/test_install_remote.sh"
  "O4|scripts/release_gate.sh|tests/test_release_gate.sh"
  "O5|tracking/cmd/orbit/release.go|tests/test_orbit_release.sh"
  "O6|skill/SKILL.md|tests/run_tests.py"
  "O7|tracking/store.go|tracking/store_test.go"
)

echo ""
echo "── Validando ${#GUARANTEES[@]} garantias operacionais ──"
for entry in "${GUARANTEES[@]}"; do
  IFS='|' read -r id code test <<<"${entry}"
  [[ -e "${REPO_ROOT}/${code}" ]] || _fail "${id}: código '${code}' não existe"
  [[ -e "${REPO_ROOT}/${test}" ]] || _fail "${id}: teste '${test}' não existe"
  grep -qE "^\| ${id} " "${CONTRACT}" \
    || _fail "${id}: não aparece na tabela §3 do SYSTEM_CONTRACT.md"
  echo "  ✓ ${id}  →  ${code}  ×  ${test}"
done

# ── O1 diretamente verificável: gate_cli.sh não faz chamadas externas ───
# Garante o contrato "<120s offline". Se alguém adicionar curl/wget/nc,
# este meta-teste pega (fail-closed).
echo ""
echo "── O1: gate_cli.sh é offline (sem curl/wget/nc) ──"
if grep -qE '\b(curl|wget|nc|ping)\b' "${REPO_ROOT}/scripts/gate_cli.sh"; then
  _fail "O1: gate_cli.sh contém chamada de rede (curl|wget|nc|ping) — quebra o contrato offline"
fi
echo "  ✓ gate_cli.sh não tem chamadas de rede"

# ── Meta: contagem bate com o documento? ────────────────────────────────
DOC_INVS=$(grep -cE "^\| I[0-9]+ " "${CONTRACT}")
DOC_GUARS=$(grep -cE "^\| O[0-9]+ " "${CONTRACT}")

if [[ "${DOC_INVS}" != "${#INVARIANTS[@]}" ]]; then
  _fail "meta: doc tem ${DOC_INVS} invariantes, script espera ${#INVARIANTS[@]}"
fi
if [[ "${DOC_GUARS}" != "${#GUARANTEES[@]}" ]]; then
  _fail "meta: doc tem ${DOC_GUARS} garantias, script espera ${#GUARANTEES[@]}"
fi

echo ""
echo "PASS: system contract (${#INVARIANTS[@]} invariantes + ${#GUARANTEES[@]} garantias, todas ancoradas em código+teste)"
