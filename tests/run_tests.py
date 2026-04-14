#!/usr/bin/env python3
"""
orbit-engine test runner.

Executes all validation tests and prints a summary report with scores.

Severity model:
    HARD fails → test fails (structural contract)
    SOFT fails → score penalty only (quality signal)

Usage:
    python tests/run_tests.py              # run all tests
    python tests/run_tests.py --verbose    # show assertion detail for every test
    python tests/run_tests.py --failures   # show detail only for failures

Exit codes:
    0 = all HARD asserts passed
    1 = one or more HARD asserts failed
"""

from __future__ import annotations

import sys
import os

# Ensure tests/ is on the path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from test_validation import TestSuite


def main() -> int:
    verbose = "--verbose" in sys.argv or "-v" in sys.argv
    failures_only = "--failures" in sys.argv or "-f" in sys.argv

    suite = TestSuite()
    suite.run_all()

    # ── Header ──
    print()
    print("=" * 64)
    print("  orbit-engine validation tests")
    print("=" * 64)
    print()

    # ── Results ──
    scores: list[float] = []
    for name, ok, score, detail in suite._results:
        icon = "✅" if ok else "❌"
        score_str = f"[{score:.0%}]"
        print(f"  {icon} {score_str:>6s}  {name}")
        scores.append(score)

        show_detail = verbose or (failures_only and not ok)
        if show_detail and detail.strip():
            for line in detail.strip().split("\n"):
                print(f"           {line}")
            print()

    # ── Scoring summary ──
    avg_score = sum(scores) / len(scores) if scores else 0.0
    total = suite.passed + suite.failed
    print()
    print("-" * 64)
    print(f"  Tests:  {total}  |  Passed: {suite.passed}  |  Failed: {suite.failed}")
    print(f"  Score:  {avg_score:.0%} average quality ({sum(scores):.1f}/{len(scores)} points)")
    print("-" * 64)

    # ── Gaming analysis ──
    warnings = suite.run_gaming_analysis()
    if warnings:
        print()
        print("  ⚠️  Gaming warnings:")
        for w in warnings:
            print(f"      {w}")
        print()

    # ── Verdict ──
    if suite.failed == 0:
        print()
        print("  🟢  All HARD asserts passed.")
        if avg_score < 1.0:
            soft_note = f"  💡  Quality score {avg_score:.0%} — SOFT fails exist (not blocking)."
            print(soft_note)
        print()
        return 0
    else:
        print()
        print(f"  🔴  {suite.failed} test(s) failed (HARD assert violations).")
        if not verbose and not failures_only:
            print("      Run with --failures or --verbose for details.")
        print()
        return 1


if __name__ == "__main__":
    sys.exit(main())
