#!/usr/bin/env bash
# install.sh — copy the pre-commit hook into .git/hooks/ and make it
# executable. Idempotent: safe to re-run. Works on macOS and Linux.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$HERE" rev-parse --show-toplevel)"
HOOK_SRC="${HERE}/pre-commit"
HOOK_DST="${REPO_ROOT}/.git/hooks/pre-commit"

if [[ ! -f "$HOOK_SRC" ]]; then
    echo "install.sh: source hook missing at ${HOOK_SRC}" >&2
    exit 1
fi

mkdir -p "$(dirname "$HOOK_DST")"
cp "$HOOK_SRC" "$HOOK_DST"
chmod +x "$HOOK_DST"

echo "installed pre-commit hook → ${HOOK_DST}"
