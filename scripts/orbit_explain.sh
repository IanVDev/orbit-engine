#!/usr/bin/env bash
# scripts/orbit_explain.sh — mostra a trilha local de uma sessão e confirma
# integridade contra o backend orbit-engine.
#
# NÃO É PROVA SOBERANA. O ledger (~/.orbit/client_ledger.jsonl) é um arquivo
# local, gravado pelo próprio cliente da skill, e mutável pelo usuário local.
# Serve como self-audit reproduzível e como ponto de partida para uma prova
# externa (ex.: ancoragem em cadeia imutável). NÃO substitui uma camada de
# atestação externa.
#
# Separação explícita de camadas:
#   Orbit  →  produz evidência local, reproduzível (este comando)
#   AURYA  →  ancora evidência em sistema imutável (orbit anchor, futuro)
# Contrato orbit→aurya: docs/ORBIT_ANCHOR_CONTRACT.md
#
# Fluxo (fail-closed em qualquer etapa):
#   [1/3] LEDGER LOCAL — lê todas as entradas do session_id, recomputa
#         sha256(session_id|timestamp|impact_estimated_tokens) e compara
#         com skill_event_hash armazenado. Qualquer divergência → exit 2.
#   [2/3] BACKEND      — GET $ORBIT_BACKEND_URL/health deve retornar 200.
#   [3/3] MÉTRICA      — GET $ORBIT_GATEWAY_URL/api/v1/query com a recording
#         rule orbit:activations_total:prod. Gateway inacessível → exit 2.
#         Valor é impresso como contexto; comparação numérica NÃO é feita
#         porque a métrica é agregada em todas as sessões (scrape lag torna
#         comparação com count local enganosa).
#
# Uso:
#   scripts/orbit_explain.sh <session_id>
#
# Env opcionais:
#   ORBIT_HOME                 diretório do ledger (default: ~/.orbit)
#   ORBIT_BACKEND_URL          default: http://localhost:9100
#   ORBIT_GATEWAY_URL          default: http://localhost:9091
#   ORBIT_EXPLAIN_LOCAL_ONLY   "1" pula fases 2 e 3 (uso em testes isolados;
#                              não é um bypass de segurança — é um escopo)

set -uo pipefail

SESSION_ID="${1:-}"
ORBIT_HOME="${ORBIT_HOME:-$HOME/.orbit}"
LEDGER="$ORBIT_HOME/client_ledger.jsonl"
BACKEND="${ORBIT_BACKEND_URL:-http://localhost:9100}"
GATEWAY="${ORBIT_GATEWAY_URL:-http://localhost:9091}"
LOCAL_ONLY="${ORBIT_EXPLAIN_LOCAL_ONLY:-0}"

fail() { echo "orbit_explain: FAIL — $*" >&2; exit 2; }

# print_scope_block <mode>  — imprime o que foi verificado e o que NÃO foi.
# mode: "full" (todas as 3 fases) | "local-only" (fase 1 apenas).
print_scope_block() {
    local mode="$1"
    echo "=================================================================="
    echo "  ESCOPO DESTA VERIFICACAO"
    echo "=================================================================="
    echo ""
    echo "VERIFICADO (reproduzível a partir dos arquivos locais):"
    echo "  [OK] $LOCAL_COUNT eventos deste session_id existem no ledger"
    echo "  [OK] sha256(session_id|timestamp|impact_estimated_tokens) de"
    echo "       cada evento bate com skill_event_hash armazenado"
    if [ "$mode" = "full" ]; then
        echo "  [OK] backend orbit-engine responde em $BACKEND/health"
        echo "  [OK] recording rule $QUERY retorna valor agregado"
    fi
    echo ""
    echo "NAO VERIFICADO (fora do escopo deste comando):"
    echo "  [--] Que $LEDGER não foi reescrito — é mutável pelo usuário"
    echo "       local. Orbit registra evidência; não ancora em sistema"
    echo "       externo imutável."
    echo "  [--] Ordem cronológica dos eventos (prev_hash não é encadeado"
    echo "       no skill_event_hash da versão atual)."
    echo "  [--] Existência desta sessão em prova externa independente."
    echo ""
    echo "PROXIMO PASSO (prova soberana):"
    echo "  orbit anchor $SESSION_ID   # publica batch_hash em AURYA"
    echo "  Contrato: docs/ORBIT_ANCHOR_CONTRACT.md (ainda não implementado)"
    echo ""
}

[ -n "$SESSION_ID" ] || fail "uso: orbit_explain.sh <session_id>"
[ -f "$LEDGER" ]     || fail "ledger não encontrado: $LEDGER"

# ---------------------------------------------------------------------------
# Fase 1 — integridade local
# Python faz parse + recompute; exit != 0 sinaliza corrupção.
# ---------------------------------------------------------------------------
LOCAL_REPORT=$(
    SESSION_ID="$SESSION_ID" LEDGER="$LEDGER" python3 <<'PY'
import hashlib
import json
import os
import sys

session_id = os.environ["SESSION_ID"]
ledger_path = os.environ["LEDGER"]

events = []
with open(ledger_path, "r", encoding="utf-8") as f:
    for lineno, raw in enumerate(f, 1):
        raw = raw.strip()
        if not raw:
            continue
        try:
            entry = json.loads(raw)
        except json.JSONDecodeError as e:
            print(f"LEDGER CORROMPIDO linha {lineno}: {e}", file=sys.stderr)
            sys.exit(2)
        if entry.get("session_id") == session_id:
            events.append((lineno, entry))

if not events:
    print(f"nenhum evento para session_id={session_id}", file=sys.stderr)
    sys.exit(2)

required = [
    "session_id", "timestamp", "impact_estimated_tokens",
    "skill_event_hash", "event_type", "mode",
]

bad = 0
out_lines = []
for idx, (lineno, e) in enumerate(events, 1):
    missing = [k for k in required if k not in e]
    if missing:
        print(f"LEDGER INVALIDO linha {lineno}: campos ausentes {missing}",
              file=sys.stderr)
        sys.exit(2)

    sid = e["session_id"]
    ts = e["timestamp"]
    try:
        tok = int(e["impact_estimated_tokens"])
    except (TypeError, ValueError):
        print(f"LEDGER INVALIDO linha {lineno}: impact_estimated_tokens "
              f"não é inteiro", file=sys.stderr)
        sys.exit(2)
    stored = e["skill_event_hash"]
    recomp = hashlib.sha256(f"{sid}|{ts}|{tok}".encode()).hexdigest()
    server_id = e.get("server_event_id", "")

    if recomp != stored:
        bad += 1
        out_lines.append(
            f"  evento #{idx}  linha {lineno}  FAIL  "
            f"recomputado={recomp[:16]}…  armazenado={stored[:16]}…"
        )
    else:
        out_lines.append(
            f"  evento #{idx}  ts={ts}  "
            f"tokens={tok}  action={e.get('action','?')}  "
            f"hash={recomp[:16]}…  server_id={server_id[:16] if server_id else '<vazio>'}…  OK"
        )

print("\n".join(out_lines))
print(f"__SUMMARY__ count={len(events)} bad={bad}")
if bad > 0:
    sys.exit(2)
PY
)
PHASE1_EXIT=$?
if [ "$PHASE1_EXIT" -ne 0 ]; then
    # Reemite relatório (se houver) no stdout para diagnóstico antes de sair.
    [ -n "$LOCAL_REPORT" ] && printf '%s\n' "$LOCAL_REPORT"
    fail "integridade local quebrada (fase 1)"
fi

SUMMARY=$(printf '%s\n' "$LOCAL_REPORT" | grep '^__SUMMARY__' | head -1)
LOCAL_COUNT=$(printf '%s' "$SUMMARY" | sed -n 's/.*count=\([0-9]*\).*/\1/p')
REPORT_BODY=$(printf '%s\n' "$LOCAL_REPORT" | grep -v '^__SUMMARY__')

echo "=================================================================="
echo "  orbit explain  —  session_id=$SESSION_ID"
echo "=================================================================="
echo ""
echo "[1/3] LEDGER LOCAL   $LEDGER"
printf '%s\n' "$REPORT_BODY"
echo "      integridade: OK ($LOCAL_COUNT eventos)"
echo ""

# ---------------------------------------------------------------------------
# Fase 2 e 3 — backend + métrica agregada
# ---------------------------------------------------------------------------
if [ "$LOCAL_ONLY" = "1" ]; then
    echo "[2/3] BACKEND              pulado (ORBIT_EXPLAIN_LOCAL_ONLY=1)"
    echo "[3/3] METRICA AGREGADA     pulado"
    echo ""
    print_scope_block "local-only"
    echo "Status: OK (local-only)"
    exit 0
fi

HEALTH=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$BACKEND/health" \
    2>/dev/null || echo "000")
[ "$HEALTH" = "200" ] || fail "backend $BACKEND/health respondeu $HEALTH"
echo "[2/3] BACKEND              $BACKEND/health = 200"

QUERY="orbit:activations_total:prod"
RAW=$(curl -sG --max-time 3 "$GATEWAY/api/v1/query" \
    --data-urlencode "query=$QUERY" 2>/dev/null || echo "")
[ -n "$RAW" ] || fail "gateway $GATEWAY inacessível"

VALUE=$(printf '%s' "$RAW" | python3 -c '
import json, sys
try:
    d = json.loads(sys.stdin.read())
except Exception as e:
    print("ERR:" + str(e), file=sys.stderr); sys.exit(1)
status = d.get("status")
if status != "success":
    print("ERR:status=" + str(status), file=sys.stderr); sys.exit(1)
r = d.get("data", {}).get("result", [])
total = sum(float(x["value"][1]) for x in r) if r else 0.0
print(int(total))
' 2>/dev/null) || fail "gateway $GATEWAY retornou resposta inválida: ${RAW:0:120}"

echo "[3/3] METRICA AGREGADA     $QUERY = $VALUE  (via $GATEWAY)"
echo ""
print_scope_block "full"
echo "Status: OK (evidência local validada; não ancorado)"
exit 0
