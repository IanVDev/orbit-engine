#!/usr/bin/env bash
# scripts/orbit_compact_guard.sh — controle determinístico de compactação de contexto.
#
# Claude Code executa "compact" automaticamente quando a janela satura. Isso
# reinicia o contexto útil e pode gerar perda de continuidade (tarefa atual,
# objetivo, restrições, último output). Este guard intercepta o ciclo:
#
#   1. snapshot  → persiste estado estruturado antes do compact
#   2. detect    → identifica mensagens contendo "Compacted" e loga o evento
#   3. rehydrate → emite o snapshot salvo para reidratação
#
# Fail-closed: se rehydrate for chamado sem snapshot válido, aborta com exit 2.
# Melhor parar do que seguir com contexto fantasma.
#
# Subcomandos:
#   snapshot --current-task <s> --objective <s> [--session-id <s>] [--constraints <s>] [--last-output <s>]
#   detect <texto>                        exit 0 se contém "[Compacted" (marcador Claude Code); exit 1 caso contrário
#   rehydrate [--expect-session-id <s>]   imprime snapshot salvo ou aborta (exit 2) se ausente
#                                         se --expect-session-id passado, valida contra snapshot
#
# Env:
#   ORBIT_HOME   diretório do estado (default: $HOME/.orbit)
#
# Arquivos gravados em $ORBIT_HOME:
#   compact_snapshot.json   snapshot atômico (0600)
#   compact_guard.jsonl     log append-only de context_compacted / rehydration_status

set -uo pipefail

ORBIT_HOME="${ORBIT_HOME:-$HOME/.orbit}"
SNAPSHOT_PATH="$ORBIT_HOME/compact_snapshot.json"
LOG_PATH="$ORBIT_HOME/compact_guard.jsonl"

fail() { echo "orbit_compact_guard: FAIL — $*" >&2; exit 2; }
info() { echo "orbit_compact_guard: $*"; }

_now_utc() { date -u +"%Y-%m-%dT%H:%M:%S.000Z"; }

_ensure_home() {
    mkdir -p "$ORBIT_HOME" || fail "não foi possível criar $ORBIT_HOME"
    chmod 700 "$ORBIT_HOME" 2>/dev/null || true
}

_log_event() {
    local payload="$1"
    _ensure_home
    local fd
    {
        printf '%s\n' "$payload" >> "$LOG_PATH"
    } || fail "falha ao escrever em $LOG_PATH"
}

cmd_snapshot() {
    local current_task="" objective="" constraints="" last_output="" session_id=""
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --current-task) current_task="${2:-}"; shift 2 ;;
            --objective)    objective="${2:-}";    shift 2 ;;
            --constraints)  constraints="${2:-}";  shift 2 ;;
            --last-output)  last_output="${2:-}";  shift 2 ;;
            --session-id)   session_id="${2:-}";   shift 2 ;;
            *) fail "flag desconhecida em snapshot: $1" ;;
        esac
    done

    [ -n "$current_task" ] || fail "snapshot exige --current-task"
    [ -n "$objective" ]    || fail "snapshot exige --objective"

    _ensure_home
    CURRENT_TASK="$current_task" OBJECTIVE="$objective" \
    CONSTRAINTS="$constraints" LAST_OUTPUT="$last_output" \
    SESSION_ID="$session_id" \
    SNAPSHOT_PATH="$SNAPSHOT_PATH" python3 <<'PY' || fail "falha ao escrever snapshot"
import json, os
from datetime import datetime, timezone

path = os.environ["SNAPSHOT_PATH"]
snapshot = {
    "schema_version": 1,
    "written_at":     datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z",
    "session_id":     os.environ["SESSION_ID"],
    "current_task":   os.environ["CURRENT_TASK"],
    "objective":      os.environ["OBJECTIVE"],
    "constraints":    os.environ["CONSTRAINTS"],
    "last_output":    os.environ["LAST_OUTPUT"],
}
tmp = path + ".tmp"
with open(tmp, "w", encoding="utf-8") as f:
    json.dump(snapshot, f, ensure_ascii=False, sort_keys=True)
os.chmod(tmp, 0o600)
os.rename(tmp, path)
PY
    info "snapshot salvo em $SNAPSHOT_PATH"
}

cmd_detect() {
    local text="${1:-}"
    [ -n "$text" ] || fail "detect exige argumento textual"

    # Marcador determinístico: Claude Code emite "[Compacted" no início da mensagem de compact
    case "$text" in
        *\[Compacted*)
            local has_snapshot="false"
            [ -f "$SNAPSHOT_PATH" ] && has_snapshot="true"
            local ts; ts="$(_now_utc)"
            _log_event "{\"event\":\"context_compacted\",\"timestamp\":\"$ts\",\"has_snapshot\":$has_snapshot}"
            info "compact detectado (has_snapshot=$has_snapshot)"
            exit 0
            ;;
        *)
            exit 1
            ;;
    esac
}

cmd_rehydrate() {
    local expect_session_id=""
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --expect-session-id) expect_session_id="${2:-}"; shift 2 ;;
            *) fail "flag desconhecida em rehydrate: $1" ;;
        esac
    done

    local ts; ts="$(_now_utc)"

    if [ ! -f "$SNAPSHOT_PATH" ]; then
        _log_event "{\"event\":\"rehydration_status\",\"timestamp\":\"$ts\",\"status\":\"fail_closed\",\"reason\":\"snapshot_missing\"}"
        fail "snapshot ausente — abortando (fail-closed)"
    fi

    local out rc=0
    out=$(SNAPSHOT_PATH="$SNAPSHOT_PATH" EXPECT_SESSION_ID="$expect_session_id" python3 <<'PY' 2>&1
import json, os, sys
with open(os.environ["SNAPSHOT_PATH"], encoding="utf-8") as f:
    s = json.load(f)
expected = os.environ["EXPECT_SESSION_ID"]
stored   = s.get("session_id", "")
if expected and expected != stored:
    sys.stderr.write(f"SESSION_MISMATCH expected={expected!r} stored={stored!r}\n")
    sys.exit(10)
print("=== REHYDRATED CONTEXT ===")
print(f"session_id:   {stored}")
print(f"current_task: {s.get('current_task','')}")
print(f"objective:    {s.get('objective','')}")
print(f"constraints:  {s.get('constraints','')}")
print(f"last_output:  {s.get('last_output','')}")
print(f"written_at:   {s.get('written_at','')}")
PY
) || rc=$?

    if [ "$rc" = "10" ]; then
        _log_event "{\"event\":\"rehydration_status\",\"timestamp\":\"$ts\",\"status\":\"fail_closed\",\"reason\":\"session_id_mismatch\"}"
        fail "session_id divergente — abortando (fail-closed): $out"
    fi
    if [ "$rc" != "0" ]; then
        _log_event "{\"event\":\"rehydration_status\",\"timestamp\":\"$ts\",\"status\":\"fail_closed\",\"reason\":\"snapshot_corrupt\"}"
        fail "snapshot corrompido — abortando (fail-closed)"
    fi

    printf '%s\n' "$out"
    _log_event "{\"event\":\"rehydration_status\",\"timestamp\":\"$ts\",\"status\":\"ok\"}"
}

print_help() {
    cat <<'USAGE'
orbit_compact_guard.sh — controle determinístico de compact.

Subcomandos:
  snapshot --current-task <s> --objective <s> [--session-id <s>] [--constraints <s>] [--last-output <s>]
      Persiste snapshot em $ORBIT_HOME/compact_snapshot.json.

  detect <texto>
      Exit 0 se texto contém "[Compacted" (marcador Claude Code) e grava context_compacted no log.
      Exit 1 caso contrário (sem log).

  rehydrate [--expect-session-id <s>]
      Emite snapshot salvo e grava rehydration_status=ok.
      Exit 2 (fail-closed) se snapshot ausente, corrompido ou session_id divergente.

Env:
  ORBIT_HOME   default: $HOME/.orbit
USAGE
}

cmd="${1:-}"
if [ "$#" -gt 0 ]; then shift; fi
case "$cmd" in
    snapshot)     cmd_snapshot "$@" ;;
    detect)       cmd_detect "$@" ;;
    rehydrate)    cmd_rehydrate "$@" ;;
    -h|--help|"") print_help ;;
    *) fail "subcomando desconhecido: $cmd" ;;
esac
