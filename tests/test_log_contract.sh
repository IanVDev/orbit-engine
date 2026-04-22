#!/usr/bin/env bash
# tests/test_log_contract.sh — Contrato estrutural do log JSON v1.
#
# Justificativa:
#   O dashboard Next.js lê ~/.orbit/logs/*.json diretamente (README.md).
#   Renomear ou remover campos do RunResult em tracking/cmd/orbit/run.go
#   quebra consumidores sem sinal no CI.
#
# O que este teste trava (qualquer um faltando → FAIL):
#   version, command, exit_code, output, proof, session_id, timestamp,
#   duration_ms, output_bytes, event, decision
#
# Também valida:
#   - version == 1 (schema v1; bump é breaking)
#   - exit_code é int; duration_ms é int; output_bytes é int
#   - run --json produz JSON com os mesmos campos do log persistido
#
# Fail-closed: qualquer desvio → exit 1.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export ORBIT_HOME="$(mktemp -d -t orbit-logc-XXXXXX)"
trap 'rm -rf "${ORBIT_HOME}"' EXIT

BIN="${ORBIT_HOME}/orbit"
COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo logc)"
LDFLAGS="-X main.Version=v0.0.0-logc -X main.Commit=${COMMIT} -X main.BuildTime=now"
(cd "${REPO_ROOT}/tracking" && GOTOOLCHAIN="${GOTOOLCHAIN:-local}" \
  go build -ldflags "${LDFLAGS}" -o "${BIN}" ./cmd/orbit) >/dev/null

# Gera uma execução e captura o JSON pelo --json e o log persistido.
"${BIN}" run --json echo hello >"${ORBIT_HOME}/stdout.json" 2>/dev/null
LOG="$(ls -1 "${ORBIT_HOME}/logs/"*.json 2>/dev/null | tail -1)"
[[ -n "${LOG}" ]] || { echo "FAIL: log não persistido" >&2; exit 1; }

python3 - "${ORBIT_HOME}/stdout.json" "${LOG}" <<'PY'
import json, sys

REQUIRED = [
    "version", "command", "exit_code", "output", "proof",
    "session_id", "timestamp", "duration_ms", "output_bytes",
    "event", "decision",
]

def validate(path, kind):
    try:
        d = json.load(open(path))
    except Exception as e:
        print(f"FAIL: {kind} não parseável: {e}", file=sys.stderr); sys.exit(1)

    missing = [k for k in REQUIRED if k not in d]
    if missing:
        print(f"FAIL: {kind} sem campos obrigatórios: {missing}",
              file=sys.stderr); sys.exit(1)

    if d["version"] != 1:
        print(f"FAIL: {kind}.version = {d['version']!r}; schema v1 esperado",
              file=sys.stderr); sys.exit(1)

    for k in ("exit_code", "duration_ms", "output_bytes"):
        if not isinstance(d[k], int):
            print(f"FAIL: {kind}.{k} = {d[k]!r}; int esperado",
                  file=sys.stderr); sys.exit(1)

    for k in ("command", "proof", "session_id", "timestamp", "event", "decision"):
        if not isinstance(d[k], str):
            print(f"FAIL: {kind}.{k} = {d[k]!r}; string esperado",
                  file=sys.stderr); sys.exit(1)

validate(sys.argv[1], "run --json stdout")
validate(sys.argv[2], "persisted log")

# Paridade: campos obrigatórios batem entre os dois canais.
a = json.load(open(sys.argv[1]))
b = json.load(open(sys.argv[2]))
for k in REQUIRED:
    if a[k] != b[k]:
        print(f"FAIL: campo {k} diverge — stdout={a[k]!r} log={b[k]!r}",
              file=sys.stderr); sys.exit(1)

print("PASS: log contract v1 (11 campos validados + paridade stdout/log)")
PY
