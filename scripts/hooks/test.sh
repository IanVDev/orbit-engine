#!/usr/bin/env bash
# test.sh — validate the pre-commit hook behaviour end-to-end.
#
# Creates a throwaway git repo, installs the hook, and asserts:
#   (a) a small staged file commits successfully;
#   (b) a staged file >5MB is rejected with exit 1.
#
# No external deps; uses dd for the 6MB fixture.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="${HERE}/pre-commit"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cd "$tmp"
git init -q
git config user.email test@test
git config user.name test
git config commit.gpgsign false

install -m 0755 "$HOOK" .git/hooks/pre-commit

# Case 1 — small file is accepted.
echo hello > small.txt
git add small.txt
if ! git commit -q -m small; then
    echo "FAIL: small file was rejected" >&2
    exit 1
fi
echo "ok  small file commits cleanly"

# Case 2 — 6MB file is rejected.
dd if=/dev/zero of=big.bin bs=1024 count=6144 status=none
git add big.bin
if git commit -q -m big 2>/dev/null; then
    echo "FAIL: 6MB file should have been rejected" >&2
    exit 1
fi
echo "ok  6MB file blocked by hook"

echo "all tests passed"
