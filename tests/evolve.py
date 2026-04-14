#!/usr/bin/env python3
"""
orbit-engine evolution orchestrator.

Single command that runs the full self-evolution cycle:
    backup → validate → decide → accept or reject.

Usage:
    python3 tests/evolve.py skill/SKILL.md              # full cycle
    python3 tests/evolve.py skill/SKILL.md --dry-run     # validate without commit
    python3 tests/evolve.py skill/SKILL.md --feedback f.jsonl  # include adoption data
    python3 tests/evolve.py --save-baseline              # snapshot current state

Exit codes:
    0 = ACCEPT (changes kept)
    1 = REJECT (backup restored)
    2 = HOLD (changes kept, flagged for review)

Dependencies: Python stdlib only.
"""

from __future__ import annotations

import shutil
import sys
import os
from pathlib import Path

# Ensure tests/ is on the path
_TESTS_DIR = Path(__file__).parent
sys.path.insert(0, str(_TESTS_DIR))

from test_validation import TestSuite
from decision_engine import (
    Baseline,
    DecisionResult,
    FeedbackMetrics,
    ValidationResults,
    Verdict,
    compute_skill_hash,
    count_lines,
    create_baseline,
    decide,
)

_BASELINE_PATH = _TESTS_DIR / ".baseline.json"
_PROJECT_ROOT = _TESTS_DIR.parent


# ---------------------------------------------------------------------------
# Test runner integration
# ---------------------------------------------------------------------------

def run_validation() -> ValidationResults:
    """Run the full test suite and return structured results."""
    suite = TestSuite()
    suite.run_all()

    per_test: dict[str, float] = {}
    scores: list[float] = []
    for name, ok, score, _ in suite._results:
        per_test[name] = score
        scores.append(score)

    avg = sum(scores) / len(scores) if scores else 0.0
    warnings = suite.run_gaming_analysis()

    return ValidationResults(
        tests_passed=suite.passed,
        tests_total=suite.passed + suite.failed,
        hard_all_passed=suite.failed == 0,
        avg_score=avg,
        per_test_scores=per_test,
        gaming_warnings=len(warnings),
    )


# ---------------------------------------------------------------------------
# Backup / restore
# ---------------------------------------------------------------------------

def backup(skill_path: Path) -> Path:
    """Create a backup of the skill file."""
    bak = skill_path.with_suffix(skill_path.suffix + ".bak")
    shutil.copy2(skill_path, bak)
    return bak


def restore(skill_path: Path) -> bool:
    """Restore skill file from backup."""
    bak = skill_path.with_suffix(skill_path.suffix + ".bak")
    if bak.exists():
        shutil.copy2(bak, skill_path)
        bak.unlink()
        return True
    return False


def cleanup_backup(skill_path: Path) -> None:
    """Remove backup after successful accept."""
    bak = skill_path.with_suffix(skill_path.suffix + ".bak")
    if bak.exists():
        bak.unlink()


# ---------------------------------------------------------------------------
# Report printer
# ---------------------------------------------------------------------------

def print_report(result: DecisionResult, skill_path: Path,
                 dry_run: bool = False) -> None:
    """Print a formatted evolution gate report."""
    print()
    print("┌" + "─" * 54 + "┐")
    print("│  orbit-engine evolution gate" + " " * 25 + "│")
    print("│" + " " * 54 + "│")

    for gate in result.gates:
        icon = {"ACCEPT": "✅", "REJECT": "🔴", "HOLD": "⚠️"}[gate.verdict.value]
        header = f"  {gate.gate}: {icon} {gate.verdict.value}"
        print(f"│{header:<54s}│")
        for reason in gate.reasons:
            line = f"    {reason}"
            # Truncate if too long
            if len(line) > 52:
                line = line[:49] + "..."
            print(f"│{line:<54s}│")

    print("│" + " " * 54 + "│")

    icon = {"ACCEPT": "✅", "REJECT": "🔴", "HOLD": "⚠️"}[result.verdict.value]
    verdict_line = f"  Verdict: {icon} {result.verdict.value}"
    print(f"│{verdict_line:<54s}│")

    for reason in result.reasons:
        line = f"  {reason}"
        if len(line) > 52:
            line = line[:49] + "..."
        print(f"│{line:<54s}│")

    if dry_run:
        print(f"│{'  (dry run — no changes applied)':<54s}│")

    print("└" + "─" * 54 + "┘")
    print()


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> int:
    args = sys.argv[1:]

    # ── Save baseline mode ──
    if "--save-baseline" in args:
        skill_path = _PROJECT_ROOT / "skill" / "SKILL.md"
        if not skill_path.exists():
            print(f"Error: {skill_path} not found.", file=sys.stderr)
            return 1

        print("Running tests to capture baseline...")
        validation = run_validation()

        if not validation.hard_all_passed:
            print("Error: Cannot save baseline — HARD failures exist.",
                  file=sys.stderr)
            return 1

        baseline = create_baseline(validation, skill_path)
        baseline.to_file(_BASELINE_PATH)
        print(f"Baseline saved to {_BASELINE_PATH}")
        print(f"  Tests: {baseline.tests_passed}/{baseline.tests_total}")
        print(f"  Score: {baseline.avg_score:.0%}")
        print(f"  Lines: {baseline.skill_lines}")
        print(f"  Hash:  {baseline.skill_hash}")
        return 0

    # ── Evolution mode ──
    dry_run = "--dry-run" in args
    clean_args = [a for a in args if not a.startswith("--")]

    if not clean_args:
        print("Usage: python3 tests/evolve.py <skill_file> [options]")
        print()
        print("Options:")
        print("  --dry-run              Validate without commit")
        print("  --feedback <file>      Include feedback JSONL")
        print("  --save-baseline        Snapshot current state")
        return 0

    skill_path = Path(clean_args[0])
    if not skill_path.is_absolute():
        skill_path = _PROJECT_ROOT / skill_path

    if not skill_path.exists():
        print(f"Error: {skill_path} not found.", file=sys.stderr)
        return 1

    # Load feedback if provided
    feedback: FeedbackMetrics | None = None
    if "--feedback" in args:
        idx = args.index("--feedback")
        if idx + 1 < len(args):
            fb_path = Path(args[idx + 1])
            if not fb_path.is_absolute():
                fb_path = _PROJECT_ROOT / fb_path
            feedback = FeedbackMetrics.from_jsonl(fb_path)

    # Load baseline if exists
    baseline: Baseline | None = None
    if _BASELINE_PATH.exists():
        baseline = Baseline.from_file(_BASELINE_PATH)

    # Step 1: Backup
    if not dry_run:
        bak_path = backup(skill_path)
        print(f"Backup: {skill_path.name} → {bak_path.name}")

    # Step 2: Validate
    print("Running tests...")
    validation = run_validation()

    # Step 3: Decide
    skill_content = skill_path.read_text(encoding="utf-8")
    skill_lines = count_lines(skill_path)

    result = decide(
        validation=validation,
        baseline=baseline,
        skill_content=skill_content,
        skill_lines=skill_lines,
        feedback=feedback,
    )

    # Step 4: Report
    print_report(result, skill_path, dry_run=dry_run)

    # Step 5: Act on verdict
    if dry_run:
        return 0

    if result.verdict == Verdict.REJECT:
        restored = restore(skill_path)
        if restored:
            print(f"  Restored {skill_path.name} from backup.")
        else:
            print(f"  Warning: no backup found to restore.", file=sys.stderr)
        return 1

    elif result.verdict == Verdict.HOLD:
        cleanup_backup(skill_path)
        print(f"  Changes kept. Manual review recommended.")
        return 2

    else:  # ACCEPT
        # Update baseline
        new_baseline = create_baseline(validation, skill_path)
        new_baseline.to_file(_BASELINE_PATH)
        cleanup_backup(skill_path)
        print(f"  Baseline updated. Changes accepted.")
        return 0


if __name__ == "__main__":
    sys.exit(main())
