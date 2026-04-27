#!/usr/bin/env bash
# tests/test_bind_loopback.sh — garante que nenhum ListenAndServe em código de
# produção (non-test) aceita bind em 0.0.0.0 ou em porta nua (":PORT").
#
# Justificativa:
#   ResolveListenAddr (bind.go) garante loopback por default, mas a função
#   nunca é chamada em produção — apenas em testes. O único ListenAndServe
#   real está em quickstart.go e usa "127.0.0.1:0" hardcoded. Este guard
#   previne que um desenvolvedor adicione um novo servidor em ":PORT" sem
#   passar por ResolveListenAddr.
#
# O que é verificado:
#   1. Nenhum `net.Listen("tcp", ":PORT")` em código de produção.
#   2. Nenhum `ListenAndServe(":PORT"` em código de produção.
#   3. Nenhum `Addr: ":PORT"` em estrutura http.Server em produção.
#   4. O embed server em quickstart.go usa 127.0.0.1 explicitamente.
#
# Fail-closed: qualquer match → exit 1.
# Portabilidade: bash 3.2 (macOS) e bash 5 (Linux/CI).
# Uso: bash tests/test_bind_loopback.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TRACKING="${REPO_ROOT}/tracking"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "OK: $*"; }

# ── Check 1: net.Listen com porta nua em produção ──────────────────────────
# Padrão proibido: net.Listen("tcp", ":NNNN") sem prefixo de host.
BARE_LISTEN=$(
  grep -rn --include="*.go" 'net\.Listen.*"tcp".*":[0-9]' "${TRACKING}" \
    | grep -v "_test.go" || true
)
if [[ -n "${BARE_LISTEN}" ]]; then
  echo "FAIL: net.Listen com porta nua (bind em 0.0.0.0) encontrado em produção:" >&2
  echo "${BARE_LISTEN}" >&2
  exit 1
fi
ok "Check 1: sem net.Listen com porta nua em produção"

# ── Check 2: http.ListenAndServe com porta nua em produção ─────────────────
BARE_LAS=$(
  grep -rn --include="*.go" 'ListenAndServe(":[0-9]' "${TRACKING}" \
    | grep -v "_test.go" || true
)
if [[ -n "${BARE_LAS}" ]]; then
  echo "FAIL: ListenAndServe com porta nua (bind em 0.0.0.0) encontrado em produção:" >&2
  echo "${BARE_LAS}" >&2
  exit 1
fi
ok "Check 2: sem ListenAndServe com porta nua em produção"

# ── Check 3: http.Server{Addr: ":PORT"} em produção ────────────────────────
BARE_ADDR=$(
  grep -rn --include="*.go" 'Addr:.*":[0-9]' "${TRACKING}" \
    | grep -v "_test.go" || true
)
if [[ -n "${BARE_ADDR}" ]]; then
  echo "FAIL: http.Server{Addr: \":PORT\"} (bind em 0.0.0.0) encontrado em produção:" >&2
  echo "${BARE_ADDR}" >&2
  exit 1
fi
ok "Check 3: sem http.Server{Addr: ':PORT'} em produção"

# ── Check 4: embedded server em quickstart.go usa 127.0.0.1 ─────────────────
QUICKSTART="${TRACKING}/cmd/orbit/quickstart.go"
[[ -f "${QUICKSTART}" ]] || fail "${QUICKSTART} não existe"

if ! grep -q '127\.0\.0\.1' "${QUICKSTART}"; then
  fail "quickstart.go não usa 127.0.0.1 explicitamente para bind do servidor embedded"
fi
ok "Check 4: quickstart.go usa 127.0.0.1 para bind do servidor embedded"

echo ""
echo "OK: bind loopback — todos os 4 checks passaram"
