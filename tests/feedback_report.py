#!/usr/bin/env python3
"""
orbit-engine feedback report.

Reads JSONL produced by feedback_collector.py and prints summary statistics.
With --with-validation, correlates adoption metrics with validation scores.

Usage:
    python3 tests/feedback_report.py feedback.jsonl
    python3 tests/feedback_report.py feedback.jsonl --with-validation

Dependencies: Python stdlib only.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path


# ---------------------------------------------------------------------------
# Data loading
# ---------------------------------------------------------------------------

def load_entries(path: Path) -> list[dict]:
    """Load JSONL entries from a file."""
    entries = []
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if line:
                entries.append(json.loads(line))
    return entries


# ---------------------------------------------------------------------------
# Summary statistics
# ---------------------------------------------------------------------------

def compute_summary(entries: list[dict]) -> dict:
    """Compute aggregate metrics across all activations."""
    if not entries:
        return {}

    total = len(entries)
    silenced = sum(1 for e in entries if e.get("silence"))
    clarifications = sum(e.get("clarification_requests", 0) for e in entries)
    recurrences = sum(1 for e in entries if e.get("pattern_recurrence"))

    # Time-to-action (exclude nulls = silence)
    tta_values = [
        e["time_to_action"] for e in entries
        if e.get("time_to_action") is not None
    ]
    avg_tta = sum(tta_values) / len(tta_values) if tta_values else None

    # Adoption rate
    total_actions = sum(e.get("action_items", 0) for e in entries)
    total_adopted = sum(e.get("actions_adopted", 0) for e in entries)
    adoption_rate = total_adopted / total_actions if total_actions > 0 else 0.0

    # Post-action rework
    rework_values = [e.get("post_action_rework", 0) for e in entries]
    avg_rework = sum(rework_values) / len(rework_values)

    # Risk distribution
    risk_counts: dict[str, int] = {}
    for e in entries:
        r = e.get("risk", "unknown") or "unknown"
        risk_counts[r] = risk_counts.get(r, 0) + 1

    # Pattern distribution
    pattern_counts: dict[str, int] = {}
    for e in entries:
        p = e.get("pattern", "unknown") or "unknown"
        pattern_counts[p] = pattern_counts.get(p, 0) + 1

    return {
        "total_activations": total,
        "silence_rate": silenced / total,
        "avg_time_to_action": avg_tta,
        "adoption_rate": adoption_rate,
        "total_clarifications": clarifications,
        "avg_post_action_rework": avg_rework,
        "pattern_recurrence_rate": recurrences / total,
        "risk_distribution": risk_counts,
        "pattern_distribution": pattern_counts,
    }


# ---------------------------------------------------------------------------
# Validation correlation
# ---------------------------------------------------------------------------

def compute_correlation(entries: list[dict]) -> dict | None:
    """Correlate validation scores with adoption metrics.

    Groups entries by validation score quartile and compares adoption rates.
    Returns None if not enough data.
    """
    scored = [e for e in entries if e.get("validation_score") is not None]
    if len(scored) < 4:
        return None

    # Sort by validation score and split into quartiles
    scored.sort(key=lambda e: e["validation_score"])
    q_size = max(1, len(scored) // 4)

    quartiles = []
    for i in range(4):
        start = i * q_size
        end = start + q_size if i < 3 else len(scored)
        group = scored[start:end]
        if not group:
            continue

        total_actions = sum(e.get("action_items", 0) for e in group)
        total_adopted = sum(e.get("actions_adopted", 0) for e in group)
        adoption = total_adopted / total_actions if total_actions > 0 else 0.0

        score_range = (
            group[0]["validation_score"],
            group[-1]["validation_score"],
        )

        tta_vals = [
            e["time_to_action"] for e in group
            if e.get("time_to_action") is not None
        ]

        quartiles.append({
            "quartile": f"Q{i + 1}",
            "score_range": f"{score_range[0]:.0%}–{score_range[1]:.0%}",
            "count": len(group),
            "adoption_rate": adoption,
            "avg_time_to_action": (
                sum(tta_vals) / len(tta_vals) if tta_vals else None
            ),
            "silence_rate": (
                sum(1 for e in group if e.get("silence")) / len(group)
            ),
        })

    return {"quartiles": quartiles, "total_scored": len(scored)}


# ---------------------------------------------------------------------------
# Report printer
# ---------------------------------------------------------------------------

def print_report(
    summary: dict,
    correlation: dict | None = None,
) -> None:
    """Print a formatted report to stdout."""
    print()
    print("=" * 60)
    print("  orbit-engine feedback report")
    print("=" * 60)
    print()

    if not summary:
        print("  No data to report.")
        return

    total = summary["total_activations"]
    print(f"  Activations analyzed: {total}")
    print()

    # --- Primary metrics ---
    print("  Primary metrics")
    print("  " + "-" * 40)
    sr = summary["silence_rate"]
    print(f"    Silence rate:         {sr:.0%} ({int(sr * total)}/{total})")

    tta = summary["avg_time_to_action"]
    tta_str = f"{tta:.1f} turns" if tta is not None else "n/a (all silent)"
    print(f"    Avg time-to-action:   {tta_str}")

    ar = summary["adoption_rate"]
    print(f"    Action adoption rate: {ar:.0%}")

    cl = summary["total_clarifications"]
    print(f"    Clarification reqs:   {cl}")
    print()

    # --- Impact metrics ---
    print("  Impact metrics")
    print("  " + "-" * 40)
    rw = summary["avg_post_action_rework"]
    print(f"    Avg post-action rework: {rw:.1f} files")

    pr = summary["pattern_recurrence_rate"]
    print(f"    Pattern recurrence:     {pr:.0%}")
    print()

    # --- Risk distribution ---
    print("  Risk distribution")
    print("  " + "-" * 40)
    for risk, count in sorted(summary["risk_distribution"].items()):
        pct = count / total
        bar = "█" * int(pct * 20)
        print(f"    {risk:>8s}: {count:3d} ({pct:4.0%}) {bar}")
    print()

    # --- Pattern distribution ---
    print("  Pattern distribution")
    print("  " + "-" * 40)
    for pattern, count in sorted(
        summary["pattern_distribution"].items(),
        key=lambda x: -x[1],
    ):
        print(f"    {pattern:.<30s} {count}")
    print()

    # --- Sample size warning ---
    if total < 10:
        print("  ⚠️  Small sample size (<10). Statistics are directional only.")
        print()

    # --- Correlation ---
    if correlation is None:
        return

    print("  Validation score ↔ Adoption correlation")
    print("  " + "-" * 40)
    print(f"    Entries with scores: {correlation['total_scored']}")
    print()

    if correlation["total_scored"] < 4:
        print("    ⚠️  Not enough scored entries for quartile analysis.")
        print()
        return

    # Table header
    print(f"    {'Q':>4s}  {'Score range':>13s}  {'#':>3s}  "
          f"{'Adoption':>9s}  {'Avg TTA':>8s}  {'Silence':>8s}")
    print("    " + "-" * 56)

    for q in correlation["quartiles"]:
        tta_s = (
            f"{q['avg_time_to_action']:.1f}t"
            if q["avg_time_to_action"] is not None
            else "n/a"
        )
        print(
            f"    {q['quartile']:>4s}  {q['score_range']:>13s}  "
            f"{q['count']:3d}  {q['adoption_rate']:>8.0%}  "
            f"{tta_s:>8s}  {q['silence_rate']:>7.0%}"
        )

    print()

    # Interpretation
    qs = correlation["quartiles"]
    if len(qs) >= 2:
        top_adoption = qs[-1]["adoption_rate"]
        bottom_adoption = qs[0]["adoption_rate"]
        if top_adoption > bottom_adoption + 0.1:
            print("    ✅ Higher validation scores correlate with higher adoption.")
        elif abs(top_adoption - bottom_adoption) <= 0.1:
            print("    ⚠️  No clear correlation between score and adoption.")
        else:
            print("    🔴 Lower validation scores correlate with higher adoption!")
            print("       → Tests may be measuring the wrong quality signals.")
    print()


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def main() -> int:
    args = sys.argv[1:]
    if not args or "--help" in args or "-h" in args:
        print("Usage: python3 tests/feedback_report.py <feedback.jsonl> "
              "[--with-validation]")
        return 0

    source = Path(args[0])
    if not source.exists():
        print(f"Error: {source} not found.", file=sys.stderr)
        return 1

    entries = load_entries(source)
    if not entries:
        print("No entries found.", file=sys.stderr)
        return 1

    summary = compute_summary(entries)

    correlation = None
    if "--with-validation" in args:
        correlation = compute_correlation(entries)

    print_report(summary, correlation)
    return 0


if __name__ == "__main__":
    sys.exit(main())
