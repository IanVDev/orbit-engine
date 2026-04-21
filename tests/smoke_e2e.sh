#!/usr/bin/env bash
# tests/smoke_e2e.sh — Smoke E2E que exercita o binário `orbit` como o usuário.
#
# Cobre os subcomandos críticos do Prod Gate v1:
#   version, run (ok), run (fail), verify (ok), verify (tamper), doctor --json.
#
# Fail-closed: qualquer assertiva falha → exit 1. Todas passam → exit 0.
#
# Isolamento: ORBIT_HOME=$(mktemp -d) — zero impacto em ~/.orbit.
# Build: sempre com -ldflags para respeitar o startup guard (fail-closed).
#
# Pré-requisitos: go, python3, bash.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export ORBIT_HOME="$(mktemp -d -t orbit-smoke-XXXXXX)"
trap 'rm -rf "${ORBIT_HOME}"' EXIT

# Sempre build local com -ldflags: o binário exige commit stamp
# (startup guard fail-closed em tracking/cmd/orbit/startup_guard.go).
BIN="${ORBIT_HOME}/orbit"
COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo smoke)"
VERSION="v0.0.0-smoke"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}"

(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "${LDFLAGS}" -o "${BIN}" ./cmd/orbit) >/dev/null

_fail() { echo "FAIL: $*" >&2; exit 1; }

# --------------------------------------------------------------------------
echo "── smoke: version ────────────────────────────────"
# stdout puro (separado do banner stderr).
VOUT="$("${BIN}" version 2>/dev/null)"
echo "${VOUT}"
echo "${VOUT}" | grep -qE "^orbit version ${VERSION} \(commit=${COMMIT} build=${BUILD_TIME}\)$" \
  || _fail "version format/valor inesperado: ${VOUT}"

# --------------------------------------------------------------------------
echo ""
echo "── smoke: run (success) ──────────────────────────"
"${BIN}" run --json echo smoke-hello >"${ORBIT_HOME}/run.json" 2>/dev/null

python3 - "${ORBIT_HOME}/run.json" <<'PY'
import json, sys
path = sys.argv[1]
d = json.load(open(path))
required = ["version", "command", "exit_code", "output", "proof",
            "session_id", "timestamp", "duration_ms"]
missing = [k for k in required if k not in d]
if missing:
    print(f"FAIL: campos ausentes: {missing}", file=sys.stderr); sys.exit(1)
if d["exit_code"] != 0:
    print(f"FAIL: exit_code {d['exit_code']} != 0", file=sys.stderr); sys.exit(1)
if "smoke-hello" not in d["output"]:
    print(f"FAIL: output sem 'smoke-hello': {d['output']!r}", file=sys.stderr); sys.exit(1)
PY

LOG="$(ls -1 "${ORBIT_HOME}/logs/"*.json 2>/dev/null | tail -1)"
[[ -n "${LOG}" ]] || _fail "log não criado em ${ORBIT_HOME}/logs/"
echo "log: ${LOG}"

# --------------------------------------------------------------------------
echo ""
echo "── smoke: verify (ok) ────────────────────────────"
"${BIN}" verify "${LOG}" >/dev/null 2>&1 \
  || _fail "verify rejeitou log legítimo"

# --------------------------------------------------------------------------
echo ""
echo "── smoke: verify (tampering) ─────────────────────"
# Proof cobre session_id + timestamp + output_bytes (verify.go:15-17).
# Adulterar output_bytes garante mismatch sem depender de contrato estendido.
TAMPERED="${ORBIT_HOME}/tampered.json"
python3 - "${LOG}" "${TAMPERED}" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
d["output_bytes"] = int(d.get("output_bytes", 0)) + 1
json.dump(d, open(sys.argv[2], "w"))
PY
if "${BIN}" verify "${TAMPERED}" >/dev/null 2>&1; then
  _fail "verify DEVE rejeitar log adulterado (output_bytes alterado)"
fi

# --------------------------------------------------------------------------
echo ""
echo "── smoke: run (exit != 0 propagado) ──────────────"
set +e
"${BIN}" run false >/dev/null 2>&1
RC=$?
set -e
[[ "${RC}" != "0" ]] || _fail "run 'false' deveria propagar exit != 0 (got ${RC})"

# Convenção do logstore: <ts>_<sid>_exit<N>.json (logstore.go).
found=0
for f in "${ORBIT_HOME}/logs/"*_exit1.json; do
  [[ -f "${f}" ]] || continue
  found=1; break
done
[[ "${found}" -eq 1 ]] || _fail "log de execução falha (exit1) não foi registrado"

# --------------------------------------------------------------------------
echo ""
echo "── smoke: doctor --json (schema v1) ──────────────"
DOC_FILE="${ORBIT_HOME}/doctor.json"
"${BIN}" doctor --json >"${DOC_FILE}" 2>/dev/null || true
[[ -s "${DOC_FILE}" ]] || _fail "doctor --json não produziu saída"

python3 - "${DOC_FILE}" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception as e:
    print(f"FAIL: doctor --json não parseável: {e}", file=sys.stderr); sys.exit(1)
# Serialização JSON usa chaves lowercase (struct tags). A fonte do contrato
# é o teste Go TestDoctorReport_SchemaVersion (doctor_structured_test.go).
if d.get("version") != "v1":
    print(f"FAIL: DoctorReport.version = {d.get('version')!r}; esperado 'v1'",
          file=sys.stderr); sys.exit(1)
for key in ("checks", "summary"):
    if key not in d:
        print(f"FAIL: chave ausente: {key}", file=sys.stderr); sys.exit(1)
PY

echo ""
echo "PASS: smoke_e2e (7 asserts)"
