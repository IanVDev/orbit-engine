#!/usr/bin/env bash
# orbit-hygiene/test.sh — validate the reusable pre-commit hook.
#
# Covers:
#   (a) small file commits cleanly;
#   (b) default 5MB limit blocks a 6MB file;
#   (c) multiple oversized files are reported together;
#   (d) renaming a committed large file is rejected;
#   (e) file between 1MB and 5MB commits with a warning on stderr;
#   (f) custom thresholds via ORBIT_HYGIENE_MAX_BYTES / _WARN_BYTES
#       (a 2MB file exceeds a 1MB custom cap and is rejected;
#        a 512KB file exceeds a 256KB custom warn and warns only).
#
# Portable across macOS and Linux. No external deps; uses dd for fixtures.
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

# Case 1 — small file accepted.
echo hello > small.txt
git add small.txt
if ! git commit -q -m small; then
    echo "FAIL: small file was rejected" >&2
    exit 1
fi
echo "ok  small file commits cleanly"

# Case 2 — default 5MB limit blocks a 6MB file.
dd if=/dev/zero of=big.bin bs=1024 count=6144 status=none
git add big.bin
if git commit -q -m big 2>/dev/null; then
    echo "FAIL: 6MB file should have been rejected (default 5MB cap)" >&2
    exit 1
fi
git restore --staged big.bin
rm -f big.bin
echo "ok  default 5MB cap blocks 6MB file"

# Case 3 — multiple oversized files reported together.
dd if=/dev/zero of=big1.bin bs=1024 count=6144 status=none
dd if=/dev/zero of=big2.bin bs=1024 count=6144 status=none
git add big1.bin big2.bin
err_out="$(git commit -m multi 2>&1 >/dev/null || true)"
if ! echo "$err_out" | grep -q "2 staged file(s) exceed"; then
    echo "FAIL: expected both oversized files reported together" >&2
    echo "$err_out" >&2
    exit 1
fi
echo "$err_out" | grep -q "big1.bin" && echo "$err_out" | grep -q "big2.bin" || {
    echo "FAIL: both filenames must appear in output" >&2; exit 1;
}
git restore --staged big1.bin big2.bin
rm -f big1.bin big2.bin
echo "ok  multiple >5MB files reported together"

# Case 4 — rename of a committed large file rejected.
dd if=/dev/zero of=tracked_big.bin bs=1024 count=6144 status=none
git -c core.hooksPath=/dev/null add tracked_big.bin
git -c core.hooksPath=/dev/null commit -q -m seed-big --no-verify
git mv tracked_big.bin renamed_big.bin
if git commit -q -m rename 2>/dev/null; then
    echo "FAIL: rename of >5MB file should have been rejected" >&2
    exit 1
fi
git reset -q --hard HEAD
echo "ok  rename of >5MB file blocked"

# Case 5 — 1-5MB file commits with warning.
dd if=/dev/zero of=mid.bin bs=1024 count=2048 status=none   # 2 MiB
git add mid.bin
err_out="$(git commit -m mid 2>&1 >/dev/null)"
echo "$err_out" | grep -q "warning" || {
    echo "FAIL: expected 1MB warning for 2MB file" >&2; echo "$err_out" >&2; exit 1;
}
git log -1 --pretty=%s | grep -q "^mid$" || {
    echo "FAIL: 2MB file commit should have been accepted" >&2; exit 1;
}
echo "ok  1-5MB file commits with warning"

# Case 6 — custom thresholds via env vars.
# Configure a commit environment that loads our custom caps.
CUSTOM_MAX=1048576   # 1 MiB cap
CUSTOM_WARN=262144   # 256 KiB warn

# 6a — 2MB file exceeds the 1MB custom cap → rejected.
dd if=/dev/zero of=custom_big.bin bs=1024 count=2048 status=none
git add custom_big.bin
err_out="$(ORBIT_HYGIENE_MAX_BYTES=$CUSTOM_MAX ORBIT_HYGIENE_WARN_BYTES=$CUSTOM_WARN \
            git commit -m custom-big 2>&1 >/dev/null || true)"
echo "$err_out" | grep -q "exceed ${CUSTOM_MAX} bytes" || {
    echo "FAIL: custom MAX_BYTES=$CUSTOM_MAX should have blocked 2MB file" >&2
    echo "$err_out" >&2; exit 1;
}
git restore --staged custom_big.bin
rm -f custom_big.bin
echo "ok  custom ORBIT_HYGIENE_MAX_BYTES enforced"

# 6b — 512KB file exceeds the 256KB custom warn but stays under the 1MB cap → warn + accept.
dd if=/dev/zero of=custom_mid.bin bs=1024 count=512 status=none
git add custom_mid.bin
err_out="$(ORBIT_HYGIENE_MAX_BYTES=$CUSTOM_MAX ORBIT_HYGIENE_WARN_BYTES=$CUSTOM_WARN \
            git commit -m custom-mid 2>&1 >/dev/null)"
echo "$err_out" | grep -q "exceed ${CUSTOM_WARN} bytes" || {
    echo "FAIL: custom WARN_BYTES=$CUSTOM_WARN should have warned on 512KB file" >&2
    echo "$err_out" >&2; exit 1;
}
git log -1 --pretty=%s | grep -q "^custom-mid$" || {
    echo "FAIL: 512KB file with custom warn should commit" >&2; exit 1;
}
echo "ok  custom ORBIT_HYGIENE_WARN_BYTES enforced"

echo "all tests passed"
