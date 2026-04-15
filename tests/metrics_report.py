#!/usr/bin/env python3
"""
metrics_report.py — CLI that reads the evidence_log.jsonl and computes
aggregate skill metrics: adoption, impact distribution, stability,
hidden regression rate, and confidence distribution.

Usage:
    python3 tests/metrics_report.py                          # default log
    python3 tests/metrics_report.py --log path/to/log.jsonl  # custom log

Does NOT modify decision_engine.py.
Reuses normalisation from trend_report.py and analysis from trend_analysis.py.
"""

from __future__ import annotations

import math
import sys
from pathlib import Path

# Ensure sibling modules are importable when run as script.
sys.path.insert(0, str(Path(__file__).parent))

from trend_report import load_entries, _confidence, _decision_signal, _DEFAULT_LOG
from trend_analysis import analyze_trend, TrendResult

# ---------------------------------------------------------------------------
# Metadata
# ---------------------------------------------------------------------------

_VERSION = "0.1.0"

# ---------------------------------------------------------------------------
# Aggregate data structure
# ---------------------------------------------------------------------------


class AggregateMetrics:
    """Plain container for all computed aggregate metrics."""

    __slots__ = (
        "total_sessions",
        "measured_sessions",
        "unmeasured_sessions",
        "adoption_rate",
        "impact_values",
        "mean_impact",
        "median_impact",
        "min_impact",
        "max_impact",
        "p25_impact",
        "p75_impact",
        "positive_rate",
        "stability",
        "absorbed",
        "hidden_regression_count",
        "hidden_regression_rate",
        "hidden_regression_metrics",
        "confidence_distribution",
        "signal_distribution",
        "verdict_distribution",
    )

    def __init__(self) -> None:
        self.total_sessions: int = 0
        self.measured_sessions: int = 0
        self.unmeasured_sessions: int = 0
        self.adoption_rate: float = 0.0
        self.impact_values: list[float] = []
        self.mean_impact: float | None = None
        self.median_impact: float | None = None
        self.min_impact: float | None = None
        self.max_impact: float | None = None
        self.p25_impact: float | None = None
        self.p75_impact: float | None = None
        self.positive_rate: float = 0.0
        self.stability: str = "unknown"
        self.absorbed: bool = False
        self.hidden_regression_count: int = 0
        self.hidden_regression_rate: float = 0.0
        self.hidden_regression_metrics: list[str] = []
        self.confidence_distribution: dict[str, int] = {}
        self.signal_distribution: dict[str, int] = {}
        self.verdict_distribution: dict[str, int] = {}


# ---------------------------------------------------------------------------
# Statistics helpers
# ---------------------------------------------------------------------------


def _median(values: list[float]) -> float:
    """Compute median of a sorted-able list."""
    s = sorted(values)
    n = len(s)
    if n % 2 == 1:
        return s[n // 2]
    return (s[n // 2 - 1] + s[n // 2]) / 2.0


def _percentile(values: list[float], p: float) -> float:
    """Compute the p-th percentile (0–100) using linear interpolation."""
    s = sorted(values)
    n = len(s)
    if n == 1:
        return s[0]
    k = (p / 100.0) * (n - 1)
    lo = int(math.floor(k))
    hi = int(math.ceil(k))
    if lo == hi:
        return s[lo]
    return s[lo] + (s[hi] - s[lo]) * (k - lo)


# ---------------------------------------------------------------------------
# Core computation
# ---------------------------------------------------------------------------


def compute_aggregate(entries: list[dict]) -> AggregateMetrics:
    """Compute aggregate metrics from normalised session_result dicts.

    Parameters
    ----------
    entries : list[dict]
        Output of ``load_entries()`` — normalised session_result dicts,
        ordered oldest → newest.

    Returns
    -------
    AggregateMetrics
        All computed fields populated.
    """
    m = AggregateMetrics()
    m.total_sessions = len(entries)

    measured = [e for e in entries if e.get("measured") is True]
    m.measured_sessions = len(measured)
    m.unmeasured_sessions = m.total_sessions - m.measured_sessions
    m.adoption_rate = (
        round(m.measured_sessions / m.total_sessions, 4)
        if m.total_sessions > 0
        else 0.0
    )

    # -- Impact distribution (percent values, not 0-1) ----------------------
    for e in measured:
        val = e.get("impact_percent")
        if val is not None:
            m.impact_values.append(val)

    if m.impact_values:
        m.mean_impact = round(sum(m.impact_values) / len(m.impact_values), 1)
        m.median_impact = round(_median(m.impact_values), 1)
        m.min_impact = round(min(m.impact_values), 1)
        m.max_impact = round(max(m.impact_values), 1)
        m.p25_impact = round(_percentile(m.impact_values, 25), 1)
        m.p75_impact = round(_percentile(m.impact_values, 75), 1)
        positive = sum(1 for v in m.impact_values if v > 0)
        m.positive_rate = round(positive / len(m.impact_values), 4)

    # -- Trend analysis for stability + regressions -------------------------
    trend: TrendResult = analyze_trend(entries)
    m.stability = trend.status          # improving / stable / regressing / intermittent / insufficient_data
    m.absorbed = trend.absorbed
    m.hidden_regression_count = len(trend.hidden_regressions)
    m.hidden_regression_metrics = list(trend.hidden_regressions)
    # Total metrics tracked = 3 (rework, efficiency, output)
    m.hidden_regression_rate = round(
        m.hidden_regression_count / 3, 4
    )

    # -- Confidence distribution (simulate per window) ----------------------
    # We compute one confidence for the overall trend
    conf = _confidence(trend)
    m.confidence_distribution = {conf: 1}

    # -- Signal distribution ------------------------------------------------
    signal = _decision_signal(trend)
    m.signal_distribution = {signal: 1}

    # -- Verdict distribution -----------------------------------------------
    verdicts: dict[str, int] = {}
    for e in entries:
        v = e.get("verdict", "UNKNOWN")
        verdicts[v] = verdicts.get(v, 0) + 1
    m.verdict_distribution = verdicts

    return m


# ---------------------------------------------------------------------------
# Formatter
# ---------------------------------------------------------------------------


def format_metrics_report(m: AggregateMetrics) -> str:
    """Render AggregateMetrics as a human-readable box report."""
    W = 58
    lines: list[str] = []

    lines.append("┌" + "─" * W + "┐")
    lines.append("│" + "  METRICS REPORT".ljust(W) + "│")
    lines.append("│" + f"  v{_VERSION}".ljust(W) + "│")
    lines.append("│" + "─" * W + "│")

    # -- Sessions -----------------------------------------------------------
    lines.append("│" + "  sessions".ljust(W) + "│")
    lines.append("│" + f"    total              {m.total_sessions}".ljust(W) + "│")
    lines.append("│" + f"    measured            {m.measured_sessions}".ljust(W) + "│")
    lines.append("│" + f"    unmeasured          {m.unmeasured_sessions}".ljust(W) + "│")
    rate_pct = f"{m.adoption_rate:.0%}"
    lines.append("│" + f"    adoption rate       {rate_pct}".ljust(W) + "│")

    # -- Verdicts -----------------------------------------------------------
    if m.verdict_distribution:
        lines.append("│" + "─" * W + "│")
        lines.append("│" + "  verdict distribution".ljust(W) + "│")
        for v in ("ACCEPT", "REJECT", "HOLD"):
            count = m.verdict_distribution.get(v, 0)
            if count > 0 or v in m.verdict_distribution:
                lines.append("│" + f"    {v:<20s} {count}".ljust(W) + "│")
        # Any unexpected verdicts
        for v, count in sorted(m.verdict_distribution.items()):
            if v not in ("ACCEPT", "REJECT", "HOLD"):
                lines.append("│" + f"    {v:<20s} {count}".ljust(W) + "│")

    # -- Impact distribution ------------------------------------------------
    lines.append("│" + "─" * W + "│")
    if m.impact_values:
        lines.append("│" + "  impact distribution (measured sessions)".ljust(W) + "│")
        lines.append("│" + f"    mean               {m.mean_impact:+.1f}%".ljust(W) + "│")
        lines.append("│" + f"    median             {m.median_impact:+.1f}%".ljust(W) + "│")
        lines.append("│" + f"    min                {m.min_impact:+.1f}%".ljust(W) + "│")
        lines.append("│" + f"    max                {m.max_impact:+.1f}%".ljust(W) + "│")
        lines.append("│" + f"    p25                {m.p25_impact:+.1f}%".ljust(W) + "│")
        lines.append("│" + f"    p75                {m.p75_impact:+.1f}%".ljust(W) + "│")
        pos_pct = f"{m.positive_rate:.0%}"
        lines.append("│" + f"    positive rate       {pos_pct}".ljust(W) + "│")
    else:
        lines.append("│" + "  impact: no measured sessions yet".ljust(W) + "│")

    # -- Stability ----------------------------------------------------------
    lines.append("│" + "─" * W + "│")
    _STAB_ICON = {
        "improving": "📈",
        "stable": "➡️",
        "regressing": "📉",
        "intermittent": "🔀",
        "insufficient_data": "⏳",
        "unknown": "❓",
    }
    icon = _STAB_ICON.get(m.stability, "❓")
    lines.append("│" + f"  stability    {icon}  {m.stability}".ljust(W) + "│")
    abs_icon = "✅" if m.absorbed else "❌"
    lines.append("│" + f"  absorbed     {abs_icon}  {'yes' if m.absorbed else 'no'}".ljust(W) + "│")

    # -- Hidden regressions -------------------------------------------------
    if m.hidden_regression_count > 0:
        lines.append("│" + "─" * W + "│")
        lines.append("│" + f"  🔴 hidden regressions: {m.hidden_regression_count}/3 metrics".ljust(W) + "│")
        rate_str = f"{m.hidden_regression_rate:.0%}"
        lines.append("│" + f"    regression rate     {rate_str}".ljust(W) + "│")
        for met in m.hidden_regression_metrics:
            lines.append("│" + f"    → {met}".ljust(W) + "│")
    else:
        lines.append("│" + f"  hidden regressions   0/3 metrics  ✅".ljust(W) + "│")

    # -- Confidence ---------------------------------------------------------
    lines.append("│" + "─" * W + "│")
    _CONF_ICON = {"none": "⚪", "low": "🟡", "medium": "🟠", "high": "🟢"}
    lines.append("│" + "  confidence distribution".ljust(W) + "│")
    for tier in ("none", "low", "medium", "high"):
        count = m.confidence_distribution.get(tier, 0)
        if count > 0:
            c_icon = _CONF_ICON.get(tier, "?")
            lines.append("│" + f"    {c_icon}  {tier:<12s} {count}".ljust(W) + "│")

    # -- Signal -------------------------------------------------------------
    _SIG_ICON = {"safe": "🟢", "caution": "🟡", "at_risk": "🔴"}
    lines.append("│" + "  signal distribution".ljust(W) + "│")
    for sig in ("safe", "caution", "at_risk"):
        count = m.signal_distribution.get(sig, 0)
        if count > 0:
            s_icon = _SIG_ICON.get(sig, "?")
            lines.append("│" + f"    {s_icon}  {sig:<12s} {count}".ljust(W) + "│")

    lines.append("└" + "─" * W + "┘")
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main() -> int:
    args = sys.argv[1:]
    log_path = _DEFAULT_LOG

    i = 0
    while i < len(args):
        if args[i] == "--log" and i + 1 < len(args):
            log_path = Path(args[i + 1])
            i += 2
        elif args[i] in ("-h", "--help"):
            print("Usage: python3 tests/metrics_report.py [--log <path>]")
            return 0
        else:
            print(f"Unknown argument: {args[i]}", file=sys.stderr)
            return 1

    if not log_path.exists():
        print(f"Log not found: {log_path}", file=sys.stderr)
        return 1

    entries = load_entries(log_path)
    if not entries:
        print("No entries in evidence log.", file=sys.stderr)
        return 1

    agg = compute_aggregate(entries)
    print(format_metrics_report(agg))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
