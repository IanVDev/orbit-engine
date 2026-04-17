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

ARG1="${1:-}"
ARG2="${2:-}"
ARG3="${3:-}"
ORBIT_HOME="${ORBIT_HOME:-$HOME/.orbit}"
LEDGER="$ORBIT_HOME/client_ledger.jsonl"
BACKEND="${ORBIT_BACKEND_URL:-http://localhost:9100}"
GATEWAY="${ORBIT_GATEWAY_URL:-http://localhost:9091}"
LOCAL_ONLY="${ORBIT_EXPLAIN_LOCAL_ONLY:-0}"

fail() { echo "orbit_explain: FAIL — $*" >&2; exit 2; }

print_help() {
    cat <<'USAGE'
orbit explain — evidência auditável de sessões de IA no seu repositório.

uso:
  orbit_explain.sh <session_id>
      Verifica integridade + exibe correlação git de uma sessão.

  orbit_explain.sh --list
      Lista todas as sessões presentes no ledger.

  orbit_explain.sh --list --since <ISO8601>
      Idem, filtrando por sessões cujo último evento é >= <ISO8601>.
      Útil em incident response: "sessões das últimas 24h".

  orbit_explain.sh -h | --help
      Esta mensagem.

Env:
  ORBIT_HOME                 diretório do ledger (default: ~/.orbit)
  ORBIT_BACKEND_URL          default: http://localhost:9100
  ORBIT_GATEWAY_URL          default: http://localhost:9091
  ORBIT_EXPLAIN_LOCAL_ONLY   "1" pula fases 2 e 3 (escopo de teste)

Exemplos de investigação:
  orbit_explain.sh --list --since 2026-04-17T00:00:00Z
  orbit_explain.sh refine-1776443535
USAGE
}

if [ "$ARG1" = "-h" ] || [ "$ARG1" = "--help" ] || [ -z "$ARG1" ]; then
    print_help
    [ -z "$ARG1" ] && exit 2 || exit 0
fi

# --list [--since <ISO>] — tabela de sessões. Resolve "qual session_id eu
# passo pro explain?" e "quais sessões rodaram desde X?" sem virar
# subcomandos separados.
if [ "$ARG1" = "--list" ]; then
    [ -f "$LEDGER" ] || fail "ledger não encontrado: $LEDGER"
    SINCE=""
    if [ "$ARG2" = "--since" ]; then
        [ -n "$ARG3" ] || fail "--since requer valor (ISO8601): orbit_explain.sh --list --since 2026-04-17"
        SINCE="$ARG3"
    elif [ -n "$ARG2" ]; then
        fail "flag desconhecida após --list: $ARG2 (use --since <ISO8601>)"
    fi
    LEDGER="$LEDGER" SINCE="$SINCE" python3 <<'PY'
import json, os
from collections import defaultdict

path = os.environ["LEDGER"]
since = os.environ.get("SINCE", "").strip()

sess = defaultdict(list)
with open(path, "r", encoding="utf-8") as f:
    for raw in f:
        raw = raw.strip()
        if not raw:
            continue
        try:
            e = json.loads(raw)
        except json.JSONDecodeError:
            continue
        sid = e.get("session_id")
        if sid:
            sess[sid].append(e)

if not sess:
    print("(ledger vazio)")
    raise SystemExit(0)

rows = []
for sid, evs in sess.items():
    evs.sort(key=lambda x: x.get("timestamp", ""))
    last_ts = evs[-1].get("timestamp", "")
    # Filtro --since: comparação lexicográfica funciona para ISO8601 normalizado.
    if since and last_ts < since:
        continue
    tok = sum(int(x.get("impact_estimated_tokens", 0)) for x in evs)
    git_head = ""
    for x in evs:
        if x.get("git_head"):
            git_head = x["git_head"][:12]
            break
    rows.append((last_ts, sid, len(evs), tok,
                 evs[0].get("timestamp", ""), last_ts, git_head))
rows.sort(reverse=True)

if not rows:
    if since:
        print(f"(nenhuma sessão com ultimo_ts >= {since})")
    else:
        print("(ledger vazio)")
    raise SystemExit(0)

print(f"{'SESSION_ID':<40}  {'EV':>3}  {'TOK':>5}  {'GIT_HEAD':<12}  PRIMEIRO_TS                ULTIMO_TS")
for _, sid, n, tok, first, last, ghead in rows:
    gh = ghead if ghead else "<sem git>"
    print(f"{sid:<40}  {n:>3}  {tok:>5}  {gh:<12}  {first:<25}  {last}")
print()
suffix = f" (filtro: --since {since})" if since else ""
print(f"total: {len(rows)} sessões{suffix}. Use: orbit_explain.sh <session_id>")
PY
    exit 0
fi

SESSION_ID="$ARG1"

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
    if [ -n "$GIT_FIRST" ]; then
        echo "  [OK] git HEAD capturado no momento de cada evento — permite"
        echo "       correlação externa via git log (ver bloco ARTEFATO)"
    fi
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
    if [ -n "$GIT_FIRST" ]; then
        echo "  [--] Que os commits entre HEAD inicial e final foram CAUSADOS"
        echo "       por esta sessão — orbit registra correlação temporal,"
        echo "       não causalidade. git log é quem prova o que mudou."
    fi
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
first_ts = events[0][1].get("timestamp", "")
last_ts = events[-1][1].get("timestamp", "")
total_tok = sum(int(e.get("impact_estimated_tokens", 0)) for _, e in events)

# Git correlation: primeiro e ultimo HEAD observados na sessao. Campos
# podem estar ausentes (ledger antigo) ou vazios (sessao fora de git repo).
first_git_head = events[0][1].get("git_head", "") or ""
last_git_head = events[-1][1].get("git_head", "") or ""
# Repo: assume estavel dentro de uma sessao; primeiro nao-vazio vence.
git_repo = ""
for _, e in events:
    r = e.get("git_repo", "") or ""
    if r:
        git_repo = r
        break

print(f"__SUMMARY__ count={len(events)} bad={bad} "
      f"first={first_ts} last={last_ts} tokens={total_tok}")
print(f"__GIT__ repo={git_repo} first={first_git_head} last={last_git_head}")
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
FIRST_TS=$(printf '%s' "$SUMMARY"    | sed -n 's/.*first=\([^ ]*\).*/\1/p')
LAST_TS=$(printf '%s' "$SUMMARY"     | sed -n 's/.*last=\([^ ]*\).*/\1/p')
TOTAL_TOK=$(printf '%s' "$SUMMARY"   | sed -n 's/.*tokens=\([0-9]*\).*/\1/p')

GIT_LINE=$(printf '%s\n' "$LOCAL_REPORT" | grep '^__GIT__' | head -1)
GIT_REPO=$(printf '%s' "$GIT_LINE"       | sed -n 's/.*repo=\([^ ]*\).*/\1/p')
GIT_FIRST=$(printf '%s' "$GIT_LINE"      | sed -n 's/.*first=\([^ ]*\).*/\1/p')
GIT_LAST=$(printf '%s' "$GIT_LINE"       | sed -n 's/.*last=\([^ ]*\).*/\1/p')

REPORT_BODY=$(printf '%s\n' "$LOCAL_REPORT" | grep -v '^__SUMMARY__' | grep -v '^__GIT__')

# Duração em segundos (0 se só houver 1 evento ou se timestamps quebrarem).
DURATION_S=$(FIRST="$FIRST_TS" LAST="$LAST_TS" python3 -c '
import os
from datetime import datetime
def p(s):
    s = s.replace("Z", "+00:00")
    return datetime.fromisoformat(s)
try:
    d = (p(os.environ["LAST"]) - p(os.environ["FIRST"])).total_seconds()
    print(f"{d:.1f}")
except Exception:
    print("0.0")
' 2>/dev/null)

echo "=================================================================="
echo "  orbit explain  —  session_id=$SESSION_ID"
echo "=================================================================="
echo ""
echo "SUMARIO              eventos=$LOCAL_COUNT  tokens=$TOTAL_TOK  duração=${DURATION_S}s"
echo "                     primeiro=$FIRST_TS"
echo "                     ultimo  =$LAST_TS"
echo ""
echo "[1/3] LEDGER LOCAL   $LEDGER"
printf '%s\n' "$REPORT_BODY"
echo "      integridade: OK ($LOCAL_COUNT eventos)"
echo ""

# Bloco ARTEFATO — ponto de partida para forensics. Mostra o estado do
# repositório git ao tempo de cada ativação. NÃO infere causalidade:
# "HEAD avançou" é fato, "sessão causou os commits" é interpretação humana
# via `git log`/`git diff` externos.
if [ -n "$GIT_FIRST" ] || [ -n "$GIT_LAST" ] || [ -n "$GIT_REPO" ]; then
    echo "ARTEFATO CORRELACIONADO (git)    ← ponto de partida para investigação"
    if [ -n "$GIT_REPO" ]; then
        echo "  repo              $GIT_REPO"
    fi
    if [ -n "$GIT_FIRST" ]; then
        echo "  HEAD ao iniciar   ${GIT_FIRST:0:12}"
    else
        echo "  HEAD ao iniciar   <não capturado>"
    fi
    if [ -n "$GIT_LAST" ]; then
        echo "  HEAD ao encerrar  ${GIT_LAST:0:12}"
    else
        echo "  HEAD ao encerrar  <não capturado>"
    fi
    if [ -n "$GIT_FIRST" ] && [ -n "$GIT_LAST" ] && [ "$GIT_FIRST" != "$GIT_LAST" ]; then
        echo "  HEAD avançou durante a sessão."
        echo ""
        echo "  Para investigar o que mudou nesta sessão:"
        echo "    cd ${GIT_REPO:-.}"
        echo "    git log --oneline ${GIT_FIRST:0:12}..${GIT_LAST:0:12}"
        echo "    git diff          ${GIT_FIRST:0:12} ${GIT_LAST:0:12}"
    elif [ -n "$GIT_FIRST" ] && [ "$GIT_FIRST" = "$GIT_LAST" ]; then
        echo "  HEAD não avançou — nenhum commit publicado durante a sessão."
        echo "  (mudanças podem existir em working tree/stash; verifique separadamente)"
    fi
    echo ""
else
    echo "ARTEFATO CORRELACIONADO (git)"
    echo "  <não capturado — ledger gravado antes da captura git, ou"
    echo "   ativações ocorreram fora de um repositório>"
    echo ""
fi

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

echo "[3/3] CONTEXTO GLOBAL      $QUERY = $VALUE  (todas as sessões; não compara"
echo "                           com esta sessão — valor apenas de contexto)"
echo ""
print_scope_block "full"
echo "Status: OK (evidência local validada; não ancorado)"
exit 0
