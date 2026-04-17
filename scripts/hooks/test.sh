#!/usr/bin/env bash
# test.sh — validate the pre-commit hook behaviour end-to-end.
#
# Creates a throwaway git repo, installs the hook, and asserts:
#   (a) a small staged file commits successfully;
#   (b) a staged file >5MB is rejected with exit 1;
#   (c) multiple staged files >5MB are all reported and rejected;
#   (d) renaming a large tracked file is rejected;
#   (e) a staged file between 1MB and 5MB commits but emits a warning.
#
# No external deps; uses dd for the fixtures.
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
git restore --staged big.bin
rm -f big.bin
echo "ok  6MB file blocked by hook"

# Case 3 — multiple files >5MB are all reported and the commit is rejected.
dd if=/dev/zero of=big1.bin bs=1024 count=6144 status=none
dd if=/dev/zero of=big2.bin bs=1024 count=6144 status=none
git add big1.bin big2.bin
err_out="$(git commit -m multi 2>&1 >/dev/null || true)"
if echo "$err_out" | grep -q "^"; then :; fi
if ! echo "$err_out" | grep -q "2 staged file(s) exceed"; then
    echo "FAIL: expected both oversized files to be reported together" >&2
    echo "$err_out" >&2
    exit 1
fi
if echo "$err_out" | grep -q "big1.bin" && echo "$err_out" | grep -q "big2.bin"; then
    echo "ok  multiple >5MB files reported together"
else
    echo "FAIL: both filenames should appear in the output" >&2
    echo "$err_out" >&2
    exit 1
fi
git restore --staged big1.bin big2.bin
rm -f big1.bin big2.bin

# Case 4 — renaming a committed large file is rejected.
# We bypass the hook to seed a 6MB tracked file, then rename it.
dd if=/dev/zero of=tracked_big.bin bs=1024 count=6144 status=none
git -c core.hooksPath=/dev/null add tracked_big.bin
git -c core.hooksPath=/dev/null commit -q -m seed-big --no-verify
git mv tracked_big.bin renamed_big.bin
if git commit -q -m rename 2>/dev/null; then
    echo "FAIL: rename of >5MB file should have been rejected" >&2
    exit 1
fi
echo "ok  rename of >5MB file blocked by hook"
git restore --staged renamed_big.bin tracked_big.bin 2>/dev/null || true
git reset -q --hard HEAD

# Case 5 — file between 1MB and 5MB: commit succeeds with a warning on stderr.
dd if=/dev/zero of=mid.bin bs=1024 count=2048 status=none   # 2 MiB
git add mid.bin
err_out="$(git commit -m mid 2>&1 >/dev/null)"
if ! echo "$err_out" | grep -q "warning"; then
    echo "FAIL: expected a 1MB warning for the 2MB file" >&2
    echo "$err_out" >&2
    exit 1
fi
if ! echo "$err_out" | grep -q "mid.bin"; then
    echo "FAIL: warning should name mid.bin" >&2
    echo "$err_out" >&2
    exit 1
fi
# Confirm the commit actually landed.
if ! git log -1 --pretty=%s | grep -q "^mid$"; then
    echo "FAIL: 2MB file commit should have been accepted (warning only)" >&2
    exit 1
fi
echo "ok  1-5MB file commits with warning"

echo "all tests passed"
