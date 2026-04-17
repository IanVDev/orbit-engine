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

# Pre-scan: strip --ignore-intent before positional arg extraction.
IGNORE_INTENT=0
_FILTERED=()
for _a in "$@"; do
    if [ "$_a" = "--ignore-intent" ]; then
        IGNORE_INTENT=1
    else
        _FILTERED+=("$_a")
    fi
done
[ "$IGNORE_INTENT" = "1" ] && set -- "${_FILTERED[@]+"${_FILTERED[@]}"}"
unset _a _FILTERED

ARG1="${1:-}"
ARG2="${2:-}"
ARG3="${3:-}"
ARG4="${4:-}"
ARG5="${5:-}"
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

  orbit_explain.sh --list [--since <ISO8601>] [--repo <caminho>]
      Lista sessões no ledger. Flags opcionais (qualquer ordem):
        --since <ISO8601>   filtra por ultimo_ts >= valor
        --repo  <caminho>   filtra por repositório git (substring)

  orbit_explain.sh --resume
      Exibe intent pendente salvo em active_task.intent.
      Exit 2 se nenhum intent existir (fail-closed).

  orbit_explain.sh --ignore-intent <comando>
      Ignora active_task.intent e executa <comando> normalmente.
      Uso para debug — não limpa o intent.

  orbit_explain.sh --verify-intent-log
      Verifica integridade da cadeia de hash em intent_overrides.jsonl.
      Exit 0 se íntegro, exit 2 se corrompido ou arquivo ausente.

  orbit_explain.sh -h | --help
      Esta mensagem.

Env:
  ORBIT_HOME                 diretório do ledger (default: ~/.orbit)
  ORBIT_BACKEND_URL          default: http://localhost:9100
  ORBIT_GATEWAY_URL          default: http://localhost:9091
  ORBIT_EXPLAIN_LOCAL_ONLY   "1" pula fases 2 e 3 (escopo de teste)

Exemplos de investigação:
  orbit_explain.sh --resume
  orbit_explain.sh --list --since 2026-04-17T00:00:00Z --repo /tmp/meu-repo
  orbit_explain.sh --list --repo /home/dev/project
  orbit_explain.sh drill-sessC-1776446786
USAGE
}

# _log_intent_override — grava entrada com hash encadeado em intent_overrides.jsonl.
# Cada entrada inclui prev_hash (hash da entrada anterior) e hash próprio:
#   hash = sha256(prev_hash + json_da_entrada_sem_hash)
# Entradas legadas (sem campo "hash") tratadas como prev_hash="" — cadeia reinicia.
# Fail-open: falha de I/O não bloqueia o bypass (observabilidade, não controle).
_log_intent_override() {
    local intent_path="$1"

    # Python grava o JSONL e emite "session_id<TAB>timestamp" para o bash
    # reaproveitar no payload AURYA — mesmo par de valores usado no log local.
    local sid_ts
    sid_ts=$(ORBIT_HOME="$ORBIT_HOME" INTENT_PATH="$intent_path" python3 <<'PY'
import hashlib, json, os
from datetime import datetime, timezone

orbit_home  = os.environ["ORBIT_HOME"]
intent_path = os.environ["INTENT_PATH"]

try:
    with open(intent_path, encoding="utf-8") as f:
        sid = json.load(f).get("session_id", "")
except Exception:
    sid = ""

ts       = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z"
log_path = os.path.join(orbit_home, "intent_overrides.jsonl")

# Ler último hash para encadeamento.
# Entrada sem campo "hash" (legado) → prev_hash = "" (cadeia reinicia).
prev_hash = ""
try:
    if os.path.exists(log_path):
        last_line = ""
        with open(log_path, encoding="utf-8") as f:
            for line in f:
                if line.strip():
                    last_line = line.strip()
        if last_line:
            try:
                prev_hash = json.loads(last_line).get("hash", "") or ""
            except json.JSONDecodeError:
                prev_hash = ""
except OSError:
    prev_hash = ""

# Construir entrada sem "hash" para cálculo canônico.
entry = {
    "event":      "intent_ignored",
    "timestamp":  ts,
    "session_id": sid,
    "reason":     "manual_override",
    "prev_hash":  prev_hash,
}
entry_json   = json.dumps(entry, separators=(",", ":"), sort_keys=True)
entry["hash"] = hashlib.sha256((prev_hash + entry_json).encode()).hexdigest()

try:
    os.makedirs(orbit_home, mode=0o700, exist_ok=True)
    fd = os.open(log_path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    try:
        os.write(fd, (json.dumps(entry, separators=(",", ":"), sort_keys=True) + "\n").encode())
    finally:
        os.close(fd)
except OSError:
    pass  # fail-open: bypass já aconteceu, log é observabilidade

print(f"{sid}\t{ts}")
PY
)
    local session_id="${sid_ts%%$'\t'*}"
    local timestamp="${sid_ts##*$'\t'}"
    session_id="${session_id:-unknown}"

    # Gate AURYA — opt-in explícito. Default = modo local (sem envio).
    if [ "${ORBIT_AURYA_ENABLED:-0}" != "1" ]; then
        echo "orbit: modo local (nenhum evento enviado)"
        return 0
    fi

    echo "orbit: registro remoto iniciado"

    local aurya_url="${ORBIT_AURYA_URL:-http://localhost:26657/v1/declarations/snapshot}"
    local payload
    payload=$(printf '{"type":"orbit.intent.override","session_id":"%s","timestamp":"%s","reason":"manual_override"}' \
        "$session_id" "$timestamp")

    # Envio assíncrono (fail-open). stdin/stdout fechados para que $(...)
    # do chamador não espere o curl; stderr herdado para expor o hash ao
    # usuário. Silencioso quando não há hash — verbose apenas sob demanda.
    (
        response=$(curl -s --max-time 2 -X POST "$aurya_url" \
            -H "Content-Type: application/json" \
            -d "$payload")
        event_hash=$(printf '%s' "$response" \
            | grep -o '"event_hash":"[^"]*"' \
            | head -n1 | cut -d':' -f2 | tr -d '"')
        if [ -n "$event_hash" ]; then
            echo "orbit: override registrado remotamente (ref: $event_hash) - by Orbit" >&2
        elif [ "${ORBIT_VERBOSE:-0}" = "1" ]; then
            echo "orbit: falha no registro remoto" >&2
        fi
    ) </dev/null >/dev/null & disown
}

# _check_and_print_intent — bloqueia execução se intent pendente existir.
# Se IGNORE_INTENT=1 (flag --ignore-intent):
#   → imprime aviso de bypass, grava log e retorna 0 (execução segue).
# Se intent ausente → return 0 (execução normal).
# Se intent válido → imprime banner + INTERROMPIDA e exit 3.
# Se intent corrompido (JSON inválido) → fail-closed exit 2.
_check_and_print_intent() {
    if [ "$IGNORE_INTENT" = "1" ]; then
        local intent_path="$ORBIT_HOME/active_task.intent"
        if [ -f "$intent_path" ]; then
            echo "! execution override manual (--ignore-intent) - by Orbit"
            echo ""
            _log_intent_override "$intent_path"
        fi
        return 0
    fi
    local intent_path="$ORBIT_HOME/active_task.intent"
    [ -f "$intent_path" ] || return 0
    local rc
    INTENT_PATH="$intent_path" python3 <<'PY'
import json, os, sys
path = os.environ["INTENT_PATH"]
try:
    with open(path, encoding="utf-8") as f:
        intent = json.load(f)
except (OSError, json.JSONDecodeError) as e:
    print(f"orbit_explain: FAIL — active_task.intent corrompido: {e}", file=sys.stderr)
    sys.exit(2)
sid  = intent.get("session_id", "<desconhecido>")
desc = intent.get("description", "<sem descrição>")
at   = intent.get("written_at", "?")
print("proteção de continuidade acionada - by Orbit")
print("")
print("! INTENT PENDENTE ───────────────────────────────────────────────")
print(f"  sessão   : {sid}")
print(f"  descrição: {desc}")
print(f"  desde    : {at}")
print("─────────────────────────────────────────────────────────────────")
print("")
print("EXECUÇÃO INTERROMPIDA (intent ativo não reconciliado)")
print("  Retome com: orbit_explain.sh --resume")
print("  Debug:      orbit_explain.sh --ignore-intent <comando>")
print("")
PY
    rc=$?
    if [ "$rc" -eq 2 ]; then
        fail "active_task.intent corrompido (JSON inválido)"
    fi
    exit 3
}

if [ "$ARG1" = "-h" ] || [ "$ARG1" = "--help" ] || [ -z "$ARG1" ]; then
    print_help
    [ -z "$ARG1" ] && exit 2 || exit 0
fi

# --resume: exibe intent pendente ou fail-closed se não existir.
if [ "$ARG1" = "--resume" ]; then
    intent_path="$ORBIT_HOME/active_task.intent"
    [ -f "$intent_path" ] || fail "nenhum intent pendente em $intent_path — nada a retomar"
    INTENT_PATH="$intent_path" python3 <<'PY'
import json, os, sys
path = os.environ["INTENT_PATH"]
try:
    with open(path, encoding="utf-8") as f:
        intent = json.load(f)
except (OSError, json.JSONDecodeError) as e:
    print(f"orbit_explain: FAIL — active_task.intent corrompido: {e}", file=sys.stderr)
    sys.exit(2)
sid    = intent.get("session_id", "<desconhecido>")
desc   = intent.get("description", "<sem descrição>")
at     = intent.get("written_at", "?")
status = intent.get("status", "?")
print("retomada de continuidade acionada - by Orbit")
print("")
print("==================================================================")
print("  RETOMADA DE TASK INTERROMPIDA")
print("==================================================================")
print(f"  session_id : {sid}")
print(f"  descrição  : {desc}")
print(f"  registrado : {at}")
print(f"  status     : {status}")
print("")
print("  Ação imediata: retomar a task acima antes de qualquer outra.")
print("  Ao concluir: --action reconcile no entrypoint limpa este intent.")
print("==================================================================")
PY
    exit $?
fi

# --verify-intent-log: verifica integridade da cadeia de hash em intent_overrides.jsonl.
# Exit 0 se íntegro, exit 2 se corrompido ou arquivo ausente.
# Entradas legadas (sem campo "hash") são puladas — cadeia reinicia a partir delas.
if [ "$ARG1" = "--verify-intent-log" ]; then
    log_path="$ORBIT_HOME/intent_overrides.jsonl"
    [ -f "$log_path" ] || fail "log não encontrado: $log_path"
    LOG_PATH="$log_path" python3 <<'PY'
import hashlib, json, os, sys

log_path = os.environ["LOG_PATH"]
with open(log_path, encoding="utf-8") as f:
    raw_lines = [l.strip() for l in f if l.strip()]

print("verificação de integridade acionada - by Orbit")
print("")
print("VERIFY intent_overrides.jsonl")
print(f"  arquivo: {log_path}")

if not raw_lines:
    print("  (arquivo vazio — nenhuma entrada)")
    print("")
    print("Status: ÍNTEGRO (0 entradas)")
    sys.exit(0)

entries = []
for i, line in enumerate(raw_lines, 1):
    try:
        entries.append(json.loads(line))
    except json.JSONDecodeError as e:
        print(f"\nFAIL: linha {i} não é JSON válido: {e}", file=sys.stderr)
        sys.exit(2)

checked = 0
skipped = 0
for i, entry in enumerate(entries):
    stored_hash = entry.get("hash", "")
    if not stored_hash:
        skipped += 1
        continue  # entrada legada sem hash — pular, cadeia reinicia

    prev_hash = entry.get("prev_hash", "")

    # Recomputa: sha256(prev_hash + canonical_json_sem_hash)
    entry_for_hash = {k: v for k, v in entry.items() if k != "hash"}
    entry_json     = json.dumps(entry_for_hash, separators=(",", ":"), sort_keys=True)
    recomputed     = hashlib.sha256((prev_hash + entry_json).encode()).hexdigest()

    if recomputed != stored_hash:
        print(f"\nCHAIN BROKEN — entrada {i+1}: hash diverge")
        print(f"  armazenado  : {stored_hash[:32]}...")
        print(f"  recomputado : {recomputed[:32]}...")
        print("")
        print("Status: CORROMPIDO")
        sys.exit(2)

    # Encadeamento: prev_hash deve igualar hash da entrada anterior.
    if i > 0:
        prev_entry_hash = entries[i-1].get("hash", "")
        if prev_hash != prev_entry_hash:
            print(f"\nCHAIN BROKEN — entrada {i+1}: prev_hash não corresponde ao hash anterior")
            print(f"  prev_hash esperado : {prev_entry_hash[:32]}...")
            print(f"  prev_hash presente : {prev_hash[:32]}...")
            print("")
            print("Status: CORROMPIDO")
            sys.exit(2)

    checked += 1

print(f"  entradas   : {len(entries)}")
print(f"  verificadas: {checked}")
if skipped:
    print(f"  legadas    : {skipped} (sem hash, puladas — cadeia reiniciou)")
print("")
print("Status: ÍNTEGRO")
PY
    exit $?
fi

# --list [--since <ISO>] [--repo <caminho>]
# Flags aceitas em qualquer ordem após --list.
if [ "$ARG1" = "--list" ]; then
    [ -f "$LEDGER" ] || fail "ledger não encontrado: $LEDGER"

    SINCE=""
    REPO_FILTER=""
    _flags=("$ARG2" "$ARG3" "$ARG4" "$ARG5")
    _i=0
    while [ "$_i" -lt 4 ]; do
        _flag="${_flags[$_i]}"
        _i=$(( _i + 1 ))
        _val="${_flags[$_i]:-}"
        case "$_flag" in
            --since)
                [ -n "$_val" ] || fail "--since requer valor ISO8601"
                SINCE="$_val"
                _i=$(( _i + 1 ))
                ;;
            --repo)
                [ -n "$_val" ] || fail "--repo requer valor (substring do caminho)"
                REPO_FILTER="$_val"
                _i=$(( _i + 1 ))
                ;;
            "")
                ;;
            *)
                fail "flag desconhecida após --list: $_flag"
                ;;
        esac
    done

    _check_and_print_intent
    LEDGER="$LEDGER" SINCE="$SINCE" REPO_FILTER="$REPO_FILTER" python3 <<'PY'
import json, os
from collections import defaultdict

path        = os.environ["LEDGER"]
since       = os.environ.get("SINCE", "").strip()
repo_filter = os.environ.get("REPO_FILTER", "").strip()

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

    # Filtro --since (lexicográfico — funciona para ISO8601 normalizado).
    if since and last_ts < since:
        continue

    # Filtro --repo (substring do git_repo de qualquer evento da sessão).
    if repo_filter:
        repos = [e.get("git_repo") or "" for e in evs]
        if not any(repo_filter in r for r in repos):
            continue

    tok = sum(int(x.get("impact_estimated_tokens", 0)) for x in evs)

    # HEAD_RANGE: first→last (6 chars cada) ou ≡ se não avançou.
    first_head = next((x["git_head"][:7] for x in evs     if x.get("git_head")), "")
    last_head  = next((x["git_head"][:7] for x in reversed(evs) if x.get("git_head")), "")
    if first_head and last_head:
        if first_head == last_head:
            head_range = f"≡ {first_head}"          # HEAD estável na sessão
        else:
            head_range = f"{first_head}→{last_head}"  # HEAD avançou
    else:
        head_range = "<sem git>"

    # Repo: basename truncado a 18 chars pelo sufixo — sufixos costumam ser
    # mais únicos que prefixos (ex: repos com mesmo prefixo + timestamp).
    all_repos = [e.get("git_repo") or "" for e in evs if e.get("git_repo")]
    repo_raw   = os.path.basename(all_repos[0]) if all_repos else ""
    repo_display = ("…" + repo_raw[-17:]) if len(repo_raw) > 18 else repo_raw

    rows.append((last_ts, sid, len(evs), tok,
                 evs[0].get("timestamp", ""), last_ts,
                 head_range, repo_display))

rows.sort(reverse=True)

if not rows:
    parts = []
    if since:
        parts.append(f"--since {since}")
    if repo_filter:
        parts.append(f"--repo {repo_filter}")
    filt = " ".join(parts)
    print(f"(nenhuma sessão com {filt})" if filt else "(ledger vazio)")
    raise SystemExit(0)

# Timestamps compactos: se todas as entradas são do mesmo dia, exibir
# apenas HH:MM:SS — a data repetida é ruído. Se dias diferentes, exibir
# YYYY-MM-DD HH:MM:SS completo.
unique_days = {last[:10] for _, _, _, _, _, last, _, _ in rows if last}
same_day    = len(unique_days) == 1
def fmt_ts(ts):
    if not ts:
        return ""
    return ts[11:19] if same_day else ts[:19].replace("T", " ")

day_hint = f"  (data: {next(iter(unique_days))} UTC)" if same_day else ""
ts_header = "HORA(UTC) " if same_day else "QUANDO(UTC)      "

print(f"{'SESSION_ID':<42}  {'EV':>3}  {'TOK':>5}  {'HEAD_RANGE':<16}  {'REPO':<18}  {ts_header}{day_hint}")
for _, sid, n, tok, first, last, hr, repo in rows:
    print(f"{sid:<42}  {n:>3}  {tok:>5}  {hr:<16}  {repo:<18}  {fmt_ts(last)}")
print()
print("  ≡ HEAD estável entre eventos  |  aaa→bbb HEAD avançou entre eventos  |  <sem git> sem captura")
print()
parts = []
if since:
    parts.append(f"--since {since}")
if repo_filter:
    parts.append(f"--repo {repo_filter}")
suffix = " (" + ", ".join(parts) + ")" if parts else ""
n_rows = len(rows)
label = "sessão" if n_rows == 1 else "sessões"
print(f"total: {n_rows} {label}{suffix}. Use: orbit_explain.sh <session_id>")
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
        echo "  [--] Que os commits entre o 1º e último evento rastreado foram CAUSADOS"
        echo "       por esta sessão — orbit registra correlação temporal,"
        echo "       não causalidade. git log é quem prova o que mudou."
    fi
    echo ""
    echo "PROXIMO PASSO (prova soberana):"
    echo "  orbit anchor $SESSION_ID   # ancora batch_hash externamente"
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
echo "SUMARIO              eventos=$LOCAL_COUNT  tokens_est=$TOTAL_TOK  intervalo=${DURATION_S}s (entre eventos rastreados)"
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
        echo "  HEAD no 1º evento   ${GIT_FIRST:0:12}"
    else
        echo "  HEAD no 1º evento   <não capturado>"
    fi
    if [ -n "$GIT_LAST" ]; then
        echo "  HEAD no último evento  ${GIT_LAST:0:12}"
    else
        echo "  HEAD no último evento  <não capturado>"
    fi
    if [ -n "$GIT_FIRST" ] && [ -n "$GIT_LAST" ] && [ "$GIT_FIRST" != "$GIT_LAST" ]; then
        echo "  HEAD avançou durante a sessão."
        echo ""
        echo "  Para investigar o que mudou entre os eventos rastreados:"
        echo "    cd ${GIT_REPO:-.}"
        echo "    git log --oneline ${GIT_FIRST:0:12}..${GIT_LAST:0:12}"
        echo "    git diff          ${GIT_FIRST:0:12} ${GIT_LAST:0:12}"
    elif [ -n "$GIT_FIRST" ] && [ "$GIT_FIRST" = "$GIT_LAST" ]; then
        echo "  HEAD não avançou entre eventos rastreados."
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
    _check_and_print_intent
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
_check_and_print_intent
echo "Status: OK (evidência local validada; não ancorado)"
exit 0
