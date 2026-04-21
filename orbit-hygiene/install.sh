#!/usr/bin/env bash
# orbit-hygiene/install.sh — copy the pre-commit hook into .git/hooks/
# and make it executable. Idempotent: safe to re-run.
#
# Must be invoked from inside the target git repository (or any of its
# subdirectories); the repository root is resolved via git rev-parse.
# Works on macOS and Linux — no external deps beyond bash + git.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git rev-parse --show-toplevel)"
HOOK_SRC="${HERE}/pre-commit"
HOOK_DST="${REPO_ROOT}/.git/hooks/pre-commit"

if [[ ! -f "$HOOK_SRC" ]]; then
    echo "install.sh: source hook missing at ${HOOK_SRC}" >&2
    exit 1
fi

mkdir -p "$(dirname "$HOOK_DST")"
cp "$HOOK_SRC" "$HOOK_DST"
chmod +x "$HOOK_DST"

echo "installed orbit-hygiene pre-commit hook → ${HOOK_DST}"
