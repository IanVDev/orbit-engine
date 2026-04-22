#!/usr/bin/env bash
# tests/test_data_safety.sh — 3 invariantes de segurança de dados.
#
# Usa o binário real (sem mocks). ORBIT_HOME em tempdir, isolado.
# Fail-closed: qualquer assertiva falhou → exit 1.
#
#   [1/3] TestFailsIfSecretIsPersisted — I12 SECRET_SAFETY
#         roda `orbit run echo` com tokens/passwords no argv; lê o log
#         persistido e falha se o valor bruto estiver em texto puro.
#
#   [2/3] TestLogRotationEnforced — I13 LOG_RETENTION
#         ORBIT_MAX_LOGS=5; cria 15 runs; conta arquivos ≤ 5.
#
#   [3/3] TestEnvironmentIntegrity — I14 ENV_INTEGRITY
#         binário sem commit-stamp (sem -ldflags) deve abortar em
#         enforceStartupIntegrity, a menos que ORBIT_SKIP_GUARD=1.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d -t orbit-ds-XXXXXX)"
trap 'rm -rf "${TMP}"' EXIT

_fail() { echo "FAIL: $*" >&2; exit 1; }

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD)"
BIN="${TMP}/orbit"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "-X main.Version=v0.0.0-ds -X main.Commit=${COMMIT} -X main.BuildTime=now" \
           -o "${BIN}" ./cmd/orbit) >/dev/null

# ── [1/3] SECRET_SAFETY ──────────────────────────────────────────────────
echo "── [1/3] TestFailsIfSecretIsPersisted ──"
HOME1="${TMP}/h1"
mkdir -p "${HOME1}"
SECRETS=(
  "Bearer sk-live-REAL-TOKEN-ABCDEFGHIJ"
  "password=superSecretValue123"
  "AKIAIOSFODNN7EXAMPLE"
  "api_key: my-private-api-key-987"
)
for s in "${SECRETS[@]}"; do
  ORBIT_HOME="${HOME1}" "${BIN}" run echo "${s}" >/dev/null 2>&1
done

LOGS=$(ls "${HOME1}/logs/"*.json 2>/dev/null || true)
[[ -n "${LOGS}" ]] || _fail "nenhum log criado (redaction etapa indetectável)"

for s in "${SECRETS[@]}"; do
  # extrai só o valor sensível (sem o prefixo) para busca
  case "${s}" in
    Bearer*)   val="sk-live-REAL-TOKEN-ABCDEFGHIJ" ;;
    password*) val="superSecretValue123" ;;
    AKIA*)     val="AKIAIOSFODNN7EXAMPLE" ;;
    api_key*)  val="my-private-api-key-987" ;;
  esac
  if grep -rF "${val}" "${HOME1}/logs/" >/dev/null 2>&1; then
    grep -rF "${val}" "${HOME1}/logs/" | head -1
    _fail "secret persistido em texto puro: ${val}"
  fi
done
# E garante que o marker REDACTED aparece (prova que o código rodou)
grep -qF "***REDACTED***" "${HOME1}/logs/"*.json \
  || _fail "marker REDACTED ausente — redaction não foi aplicada"
echo "    ✓ 4 secrets redigidos, marker presente"

# ── [2/3] LOG_RETENTION ──────────────────────────────────────────────────
echo "── [2/3] TestLogRotationEnforced ──"
HOME2="${TMP}/h2"
mkdir -p "${HOME2}"
for i in $(seq 1 15); do
  ORBIT_MAX_LOGS=5 ORBIT_HOME="${HOME2}" "${BIN}" run echo "r${i}" >/dev/null 2>&1
done
COUNT=$(ls "${HOME2}/logs/"*.json 2>/dev/null | wc -l)
if [[ "${COUNT}" -gt 5 ]]; then
  _fail "ORBIT_MAX_LOGS=5 mas há ${COUNT} arquivos — rotação não foi aplicada"
fi
if [[ "${COUNT}" -lt 1 ]]; then
  _fail "todos logs foram apagados — rotação é agressiva demais"
fi
echo "    ✓ 15 runs → ${COUNT} logs persistidos (cap=5)"

# ── [3/3] ENV_INTEGRITY ──────────────────────────────────────────────────
echo "── [3/3] TestEnvironmentIntegrity ──"
BIN_NO_STAMP="${TMP}/orbit-nostamp"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -o "${BIN_NO_STAMP}" ./cmd/orbit) >/dev/null
# Sem ldflags → Version="dev" Commit="unknown" → guard deve abortar em `run`.
if ORBIT_HOME="${TMP}/h3" "${BIN_NO_STAMP}" run echo ok >/dev/null 2>&1; then
  _fail "binário sem commit-stamp executou `run` (guard não abortou)"
fi
# ORBIT_SKIP_GUARD=1 deve permitir bypass explícito (documented escape hatch).
if ! ORBIT_SKIP_GUARD=1 ORBIT_HOME="${TMP}/h3" "${BIN_NO_STAMP}" run echo ok >/dev/null 2>&1; then
  _fail "ORBIT_SKIP_GUARD=1 não bypassou o guard"
fi
echo "    ✓ guard aborta ambiente sem stamp; bypass explícito funciona"

echo ""
echo "PASS: data safety (I12 redaction + I13 rotation + I14 env integrity)"
