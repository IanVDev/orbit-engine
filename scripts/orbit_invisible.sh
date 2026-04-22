#!/usr/bin/env bash
# orbit_invisible.sh — instala/remove o modo invisível do Orbit.
#
# Modelo: o dev não precisa digitar "orbit run". Comandos comuns (go, npm,
# python, python3, make) são interceptados por aliases que delegam para
# `orbit run <cmd> <args>`. Se o binário orbit não existir ou
# ORBIT_INVISIBLE_OFF=1, roda o comando cru (fail-open para execução).
#
# Uso:
#   scripts/orbit_invisible.sh install      # gera shell-init + injeta source no rc
#   scripts/orbit_invisible.sh uninstall    # remove bloco marcado do rc + shell-init
#   scripts/orbit_invisible.sh status       # mostra estado atual
#   scripts/orbit_invisible.sh print        # emite o conteúdo do shell-init na stdout
#
# Escape hatches (no shell do dev):
#   ORBIT_INVISIBLE_OFF=1 go test ./...     # bypass temporário
#   unalias go npm python python3 make      # bypass da sessão corrente
#
# Futuro (auto-capture): ver bloco PROPOSTA_AUTO_CAPTURE no final.

set -euo pipefail

ORBIT_HOME="${ORBIT_HOME:-$HOME/.orbit}"
SHELL_INIT="$ORBIT_HOME/shell-init.sh"
MARK_BEGIN="# >>> orbit-invisible begin >>>"
MARK_END="# <<< orbit-invisible end <<<"

WRAPPED_COMMANDS=(go npm python python3 make)

detect_rc() {
    case "${SHELL:-}" in
        */zsh) printf '%s\n' "$HOME/.zshrc" ;;
        */bash) printf '%s\n' "$HOME/.bashrc" ;;
        *) printf '%s\n' "$HOME/.profile" ;;
    esac
}

emit_shell_init() {
    cat <<'SHELL_INIT_EOF'
# Gerado por orbit_invisible.sh — não editar à mão.
# Remova via: scripts/orbit_invisible.sh uninstall

orbit_wrap() {
    local cmd="$1"; shift
    if [ "${ORBIT_INVISIBLE_OFF:-0}" = "1" ]; then
        command "$cmd" "$@"
        return $?
    fi
    if ! command -v orbit >/dev/null 2>&1; then
        command "$cmd" "$@"
        return $?
    fi
    orbit run "$cmd" "$@"
}

alias go='orbit_wrap go'
alias npm='orbit_wrap npm'
alias python='orbit_wrap python'
alias python3='orbit_wrap python3'
alias make='orbit_wrap make'
SHELL_INIT_EOF
}

cmd_print() {
    emit_shell_init
}

cmd_install() {
    mkdir -p "$ORBIT_HOME"
    emit_shell_init > "$SHELL_INIT"
    chmod 0644 "$SHELL_INIT"

    local rc
    rc="$(detect_rc)"
    if [ ! -f "$rc" ]; then
        touch "$rc"
    fi

    if grep -qF "$MARK_BEGIN" "$rc" 2>/dev/null; then
        printf 'orbit-invisible já instalado em %s — pulando.\n' "$rc"
    else
        {
            printf '\n%s\n' "$MARK_BEGIN"
            printf '[ -f "%s" ] && . "%s"\n' "$SHELL_INIT" "$SHELL_INIT"
            printf '%s\n' "$MARK_END"
        } >> "$rc"
        printf 'orbit-invisible instalado em %s.\n' "$rc"
    fi

    printf 'Comandos interceptados: %s\n' "${WRAPPED_COMMANDS[*]}"
    printf 'Abra um novo shell ou rode: source %s\n' "$rc"
}

cmd_uninstall() {
    local rc
    rc="$(detect_rc)"
    if [ -f "$rc" ] && grep -qF "$MARK_BEGIN" "$rc" 2>/dev/null; then
        local tmp
        tmp="$(mktemp)"
        awk -v b="$MARK_BEGIN" -v e="$MARK_END" '
            $0 == b { inblock = 1; next }
            $0 == e { inblock = 0; next }
            !inblock { print }
        ' "$rc" > "$tmp"
        mv "$tmp" "$rc"
        printf 'Bloco removido de %s.\n' "$rc"
    else
        printf 'Nenhum bloco orbit-invisible em %s.\n' "$rc"
    fi

    if [ -f "$SHELL_INIT" ]; then
        rm -f "$SHELL_INIT"
        printf 'Removido %s.\n' "$SHELL_INIT"
    fi

    printf 'Abra um novo shell para sair dos aliases na sessão atual.\n'
}

cmd_status() {
    local rc
    rc="$(detect_rc)"
    printf 'ORBIT_HOME:   %s\n' "$ORBIT_HOME"
    printf 'shell-init:   %s %s\n' "$SHELL_INIT" "$([ -f "$SHELL_INIT" ] && echo '[ok]' || echo '[ausente]')"
    printf 'rc:           %s\n' "$rc"
    if [ -f "$rc" ] && grep -qF "$MARK_BEGIN" "$rc" 2>/dev/null; then
        printf 'rc injection: [presente]\n'
    else
        printf 'rc injection: [ausente]\n'
    fi
    if command -v orbit >/dev/null 2>&1; then
        printf 'orbit binary: %s\n' "$(command -v orbit)"
    else
        printf 'orbit binary: [não encontrado — fail-open ativaria passthrough]\n'
    fi
}

main() {
    local action="${1:-status}"
    case "$action" in
        install)   cmd_install ;;
        uninstall) cmd_uninstall ;;
        status)    cmd_status ;;
        print)     cmd_print ;;
        -h|--help)
            sed -n '2,20p' "$0"
            ;;
        *)
            printf 'Ação desconhecida: %s\n' "$action" >&2
            printf 'Use: install | uninstall | status | print\n' >&2
            exit 2
            ;;
    esac
}

main "$@"

# ──────────────────────────────────────────────────────────────────────────
# PROPOSTA_AUTO_CAPTURE (fase 2 — não implementada neste script)
# ──────────────────────────────────────────────────────────────────────────
# Objetivo: capturar execuções sem exigir aliases explícitos.
#
# Opção A — zsh preexec/precmd hooks
#   autoload -Uz add-zsh-hook
#   orbit_preexec() { ORBIT_LAST_CMD="$1"; ORBIT_LAST_START=$EPOCHREALTIME; }
#   orbit_precmd()  { orbit capture --cmd "$ORBIT_LAST_CMD" --rc $? --duration ...; }
#   add-zsh-hook preexec orbit_preexec
#   add-zsh-hook precmd  orbit_precmd
#   → requer subcomando `orbit capture` (não-bloqueante, async) no core.
#
# Opção B — bash DEBUG trap + PROMPT_COMMAND
#   equivalente ao A, mas com trap DEBUG (menos robusto).
#
# Opção C — VSCode/JetBrains extension
#   intercepta tasks.json run/test/build e chama `orbit run` internamente.
#   maior alcance, mas fora do escopo shell.
#
# Critério de passagem para fase 2: aliases rodando em produção por ≥ 2
# semanas com taxa de fallback (ORBIT_INVISIBLE_OFF) < 5% das execuções.
