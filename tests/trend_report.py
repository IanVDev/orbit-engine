#!/usr/bin/env python3
"""
trend_report.py — CLI that reads SESSION RESULT entries from the
evidence_log and prints a human-readable trend summary.

Usage:
    python3 tests/trend_report.py                          # default log
    python3 tests/trend_report.py --log path/to/log.jsonl  # custom log

Does NOT modify decision_engine.py.  Depends only on trend_analysis.py.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

# Ensure sibling modules are importable when run as script.
sys.path.insert(0, str(Path(__file__).parent))

from trend_analysis import TrendResult, MetricTrend, analyze_trend, _WINDOW

# ---------------------------------------------------------------------------
# Metadata
# ---------------------------------------------------------------------------

_ALGORITHM_VERSION = "0.1.0"

# ---------------------------------------------------------------------------
# Evidence log reader
# ---------------------------------------------------------------------------

_DEFAULT_LOG = Path(__file__).parent / "evidence_log.jsonl"


def _normalise_entry(raw: dict) -> dict:
    """Turn any evidence_log entry into a session_result-shaped dict.

    Handles three shapes:
    1. Entry with ``session_result`` key (v1 schema) — use directly.
    2. Entry with ``impact`` key (legacy format) — convert on the fly.
    3. Entry with neither — return unmeasured stub.
    """
    if "session_result" in raw:
        return raw["session_result"]

    if "impact" in raw:
        imp = raw["impact"]
        composite = imp.get("composite_improvement")
        rework = imp.get("rework_reduction")
        efficiency = imp.get("efficiency_gain")
        output = imp.get("output_reduction")
        measured = composite is not None
        # Legacy values are 0-1 floats (e.g. 0.76 = 76%).
        # Store as-is; trend_analysis._composites handles both scales.
        return {
            "schema_version": "v1",
            "origin": raw.get("origin", "manual"),
            "verdict": raw.get("verdict", "HOLD"),
            "measured": measured,
            "impact_percent": round(composite * 100, 1) if composite is not None else None,
            "impact_status": (
                "positive" if composite is not None and composite > 0 else
                "negative" if composite is not None and composite < 0 else
                "n/a"
            ),
            "breakdown": {
                "rework": round(rework * 100, 1) if rework is not None else None,
                "efficiency": round(efficiency * 100, 1) if efficiency is not None else None,
                "output": round(output * 100, 1) if output is not None else None,
            },
            "composite_formula": "",
            "composite_weights": {"rework": 0.50, "efficiency": 0.30, "output": 0.20},
            "tradeoff_detected": False,
            "tradeoff_metrics": [],
            "causality_message": None,
        }

    # No impact data at all
    return {
        "schema_version": "v1",
        "origin": raw.get("origin", "manual"),
        "verdict": raw.get("verdict", "HOLD"),
        "measured": False,
        "impact_percent": None,
        "impact_status": "n/a",
        "breakdown": {"rework": None, "efficiency": None, "output": None},
        "composite_formula": "",
        "composite_weights": {"rework": 0.50, "efficiency": 0.30, "output": 0.20},
        "tradeoff_detected": False,
        "tradeoff_metrics": [],
        "causality_message": None,
    }


def load_entries(log_path: Path) -> list[dict]:
    """Read evidence_log.jsonl and return normalised session_result dicts."""
    if not log_path.exists():
        return []
    entries: list[dict] = []
    with open(log_path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                raw = json.loads(line)
                entries.append(_normalise_entry(raw))
            except json.JSONDecodeError:
                continue  # skip corrupt lines
    return entries


# ---------------------------------------------------------------------------
# Confidence tier
# ---------------------------------------------------------------------------

def _confidence(r: TrendResult) -> str:
    """Derive a confidence label from measured sessions and sigma.

    Signal priority: hidden regressions cap confidence at "medium"
    regardless of session count or sigma — the trend summary cannot
    be fully trusted when a metric is silently declining.
    """
    if r.measured_sessions < 5:
        return "none"

    # Base tier from volume + variance
    if r.measured_sessions < 10:
        base = "low"
    elif r.sigma is not None and r.sigma > 0.25:
        base = "low"
    elif r.measured_sessions >= 20 and r.sigma is not None and r.sigma < 0.10:
        base = "high"
    else:
        base = "medium"

    # Cap: hidden regressions → never higher than "medium"
    if r.hidden_regressions and base == "high":
        return "medium"

    return base


# ---------------------------------------------------------------------------
# Decision signal
# ---------------------------------------------------------------------------

def _decision_signal(r: TrendResult) -> str:
    """Derive an actionable signal from the trend analysis.

    Returns one of:
      - "safe"    — no concerning signals, trend is healthy.
      - "caution" — some instability but no hidden damage.
      - "at_risk" — hidden regressions or active composite decline.

    Evaluation order (highest priority first):
      1. hidden_regressions not empty         → at_risk
      2. status == "regressing"               → at_risk
      3. status == "intermittent"             → caution
      4. sigma > 0.25 (high variance)         → caution
      5. status == "insufficient_data"        → caution
      6. everything else                      → safe
    """
    if r.hidden_regressions:
        return "at_risk"
    if r.status == "regressing":
        return "at_risk"
    if r.status == "intermittent":
        return "caution"
    if r.sigma is not None and r.sigma > 0.25:
        return "caution"
    if r.status == "insufficient_data":
        return "caution"
    return "safe"


# ---------------------------------------------------------------------------
# Regression severity
# ---------------------------------------------------------------------------

def _regression_severity(r: TrendResult) -> str | None:
    """Classify severity of hidden regressions by count of affected metrics.

    Returns None when there are no hidden regressions.
      - "minor"    — 1 metric declining (localised issue)
      - "moderate" — 2 metrics declining (pattern spreading)
      - "critical" — 3 metrics declining (systemic failure)
    """
    n = len(r.hidden_regressions)
    if n == 0:
        return None
    if n == 1:
        return "minor"
    if n == 2:
        return "moderate"
    return "critical"


# ---------------------------------------------------------------------------
# Formatter
# ---------------------------------------------------------------------------

_STATUS_ICON = {
    "improving": "📈",
    "stable": "➡️",
    "regressing": "📉",
    "intermittent": "🔀",
    "insufficient_data": "⏳",
}

_CONFIDENCE_ICON = {
    "none": "⚪",
    "low": "🟡",
    "medium": "🟠",
    "high": "🟢",
}

_SIGNAL_ICON = {
    "safe": "🟢",
    "caution": "🟡",
    "at_risk": "🔴",
}

_SEVERITY_ICON = {
    "minor": "▪",
    "moderate": "▪▪",
    "critical": "▪▪▪",
}


def _bar(value: float, width: int = 16) -> str:
    """Render a simple bar ████░░░░ for a 0-1 value."""
    clamped = max(0.0, min(1.0, value))
    filled = int(clamped * width)
    return "█" * filled + "░" * (width - filled)


def format_trend_report(r: TrendResult) -> str:
    """Render a TrendResult as a human-readable box report."""
    conf = _confidence(r)
    signal = _decision_signal(r)
    severity = _regression_severity(r)
    lines: list[str] = []
    W = 58  # inner width

    lines.append("┌" + "─" * W + "┐")
    lines.append("│" + "  TREND REPORT".ljust(W) + "│")
    meta = f"  algorithm v{_ALGORITHM_VERSION}  |  window {_WINDOW}"
    lines.append("│" + meta.ljust(W) + "│")
    lines.append("│" + "─" * W + "│")

    # -- status row ---------------------------------------------------------
    icon = _STATUS_ICON.get(r.status, "?")
    status_line = f"  status      {icon}  {r.status}"
    lines.append("│" + status_line.ljust(W) + "│")

    # -- decision signal ----------------------------------------------------
    s_icon = _SIGNAL_ICON.get(signal, "?")
    signal_line = f"  signal      {s_icon}  {signal}"
    lines.append("│" + signal_line.ljust(W) + "│")

    # -- confidence ---------------------------------------------------------
    c_icon = _CONFIDENCE_ICON.get(conf, "?")
    conf_line = f"  confidence  {c_icon}  {conf}  ({r.measured_sessions} measured sessions)"
    lines.append("│" + conf_line.ljust(W) + "│")

    if r.status == "insufficient_data":
        lines.append("│" + " " * W + "│")
        detail = f"  {r.absorption_detail}"
        if len(detail) > W:
            detail = detail[: W - 3] + "..."
        lines.append("│" + detail.ljust(W) + "│")
        lines.append("└" + "─" * W + "┘")
        return "\n".join(lines)

    lines.append("│" + "─" * W + "│")

    # -- composite stats ----------------------------------------------------
    mean_str = f"{r.mean_composite:+.0%}" if r.mean_composite is not None else "n/a"
    sigma_str = f"{r.sigma:.2f}" if r.sigma is not None else "n/a"
    slope_str = f"{r.slope:+.4f}/session" if r.slope is not None else "n/a"

    lines.append("│" + f"  mean composite   {mean_str}".ljust(W) + "│")
    lines.append("│" + f"  sigma (σ)        {sigma_str}".ljust(W) + "│")
    lines.append("│" + f"  slope            {slope_str}".ljust(W) + "│")

    # -- absorption ---------------------------------------------------------
    lines.append("│" + "─" * W + "│")
    abs_icon = "✅" if r.absorbed else "❌"
    abs_line = f"  absorbed    {abs_icon}  {'yes' if r.absorbed else 'no'}"
    lines.append("│" + abs_line.ljust(W) + "│")
    detail = f"  {r.absorption_detail}"
    if len(detail) > W:
        detail = detail[: W - 3] + "..."
    lines.append("│" + detail.ljust(W) + "│")

    # -- per-metric trends --------------------------------------------------
    if r.metric_trends:
        lines.append("│" + "─" * W + "│")
        lines.append("│" + "  metric trends (last 5 sessions)".ljust(W) + "│")
        for mt in r.metric_trends:
            direction = "↘" if mt.declining else ("↗" if mt.slope > 0.02 else "→")
            val = mt.values[-1] if mt.values else 0.0
            pct = val / 100.0 if abs(val) > 1.5 else val
            b = _bar(max(0.0, pct))
            metric_line = f"    {mt.name:<12s} {b}  {direction} slope {mt.slope:+.4f}"
            if len(metric_line) > W:
                metric_line = metric_line[: W - 3] + "..."
            lines.append("│" + metric_line.ljust(W) + "│")

    # -- hidden regressions — CRITICAL alert ------------------------------
    if r.hidden_regressions:
        sev_icon = _SEVERITY_ICON.get(severity or "", "")
        lines.append("│" + "─" * W + "│")
        lines.append("│" + "  🔴 CRITICAL — hidden regressions detected".ljust(W) + "│")
        sev_line = f"  severity: {severity}  {sev_icon}  ({len(r.hidden_regressions)}/3 metrics)"
        lines.append("│" + sev_line.ljust(W) + "│")
        lines.append("│" + "  composite is stable but these metrics are declining:".ljust(W) + "│")
        for m in r.hidden_regressions:
            mt = next((t for t in r.metric_trends if t.name == m), None)
            if mt:
                warn = f"    → {m}: {mt.consecutive_drops} consecutive drops, slope {mt.slope:+.4f}"
            else:
                warn = f"    → {m}: declining"
            if len(warn) > W:
                warn = warn[: W - 3] + "..."
            lines.append("│" + warn.ljust(W) + "│")
        lines.append("│" + "  confidence capped at medium due to signal conflict".ljust(W) + "│")

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
            print("Usage: python3 tests/trend_report.py [--log <path>]")
            return 0
        else:
            print(f"Unknown argument: {args[i]}", file=sys.stderr)
            return 1

    entries = load_entries(log_path)
    if not entries:
        print(f"No entries found in {log_path}")
        return 1

    result = analyze_trend(entries)
    print()
    print(format_trend_report(result))
    print()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
