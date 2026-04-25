#!/usr/bin/env bash
# scripts/check_claude_slash_command_bridge.sh — valida que a skill orbit-prompt
# tem um slash command bridge correspondente em .claude/commands/.
#
# Fail-closed: qualquer violação → exit 1.
# Uso: bash scripts/check_claude_slash_command_bridge.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

SKILL_MD="${REPO_ROOT}/skills/orbit-prompt/SKILL.md"
COMMAND_MD="${REPO_ROOT}/.claude/commands/orbit-prompt.md"

FAIL=0

check() {
  local msg="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "  [PASS] ${msg}"
  else
    echo "  [FAIL] ${msg}" >&2
    FAIL=1
  fi
}

echo ""
echo "── slash command bridge: orbit-prompt ──"
echo ""

# 1. Skill manifest existe no repo
check "skills/orbit-prompt/SKILL.md existe" test -f "${SKILL_MD}"

# 2. Slash command bridge existe
check ".claude/commands/orbit-prompt.md existe" test -f "${COMMAND_MD}"

# 3. Command bridge referencia orbit-prompt (não é um arquivo genérico)
check "command bridge referencia 'orbit-prompt'" \
  grep -q "orbit-prompt" "${COMMAND_MD}"

echo ""

if [[ "${FAIL}" -ne 0 ]]; then
  echo "FAIL: orbit-prompt slash command bridge incompleto" >&2
  echo "      Skill sem bridge quebra a primeira experiência do usuário." >&2
  exit 1
fi

echo "PASS: orbit-prompt slash command bridge OK"
