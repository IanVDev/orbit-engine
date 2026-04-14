"""
trend_analysis.py — Analyse multiple SESSION RESULT entries from the
evidence_log to identify improvement trends, behavioural absorption,
and hidden per-metric regression.

Standalone module.  No dependency on decision_engine.py at runtime.
Reads plain dicts (the output of SessionResultSchema.to_dict()).

Minimum data: 5 measured sessions.  Below that, returns
status = "insufficient_data".
"""

from __future__ import annotations

import math
from dataclasses import dataclass, field

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

_MIN_SESSIONS = 5          # minimum measured sessions for any conclusion
_WINDOW = 5                # rolling window size for sigma / slope
_ABSORPTION_THRESHOLD = 8  # out of last 10, must be within ±15 pp of median
_ABSORPTION_SAMPLE = 10
_ABSORPTION_BAND = 0.15    # ±15 percentage points
_SIGMA_STABLE = 0.10       # σ below this = stable behaviour
_SIGMA_HIGH = 0.25         # σ above this = intermittent behaviour
_SLOPE_IMPROVING = 0.02    # slope above this = improving
_SLOPE_REGRESSING = -0.02  # slope below this = regressing
_METRIC_DECLINE = -0.05    # per-session slope below this = metric declining
_CONSECUTIVE_DECLINE = 3   # consecutive declining sessions to flag regression

_METRICS = ("rework", "efficiency", "output")


# ---------------------------------------------------------------------------
# Output data structures
# ---------------------------------------------------------------------------

@dataclass
class MetricTrend:
    """Trend for a single breakdown metric across the analysis window."""
    name: str
    slope: float          # per-session change (positive = improving)
    values: list[float]   # raw values used
    declining: bool       # True if slope < _METRIC_DECLINE
    consecutive_drops: int  # longest streak of consecutive decreases


@dataclass
class TrendResult:
    """Complete trend analysis output."""
    status: str                         # "insufficient_data" | "improving"
                                        # | "stable" | "regressing"
                                        # | "intermittent"
    sessions_analysed: int
    measured_sessions: int

    # composite window stats (last _WINDOW measured sessions)
    mean_composite: float | None
    sigma: float | None                 # standard deviation of window
    slope: float | None                 # linear slope over window

    # behavioural absorption
    absorbed: bool                      # True if behaviour is consistent
    absorption_detail: str              # human-readable explanation

    # per-metric trends
    metric_trends: list[MetricTrend] = field(default_factory=list)

    # hidden regressions: metrics declining while composite is flat/up
    hidden_regressions: list[str] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------

def _extract_measured(entries: list[dict]) -> list[dict]:
    """Keep only entries where impact was actually measured."""
    return [e for e in entries if e.get("measured") is True]


def _composites(entries: list[dict]) -> list[float]:
    """Extract impact_percent from measured entries, skipping None."""
    out: list[float] = []
    for e in entries:
        val = e.get("impact_percent")
        if val is not None:
            out.append(val / 100.0 if abs(val) > 1.5 else val)
    return out


def _breakdown_series(entries: list[dict], metric: str) -> list[float]:
    """Extract one metric from breakdown across entries."""
    out: list[float] = []
    for e in entries:
        bd = e.get("breakdown", {})
        val = bd.get(metric)
        if val is not None:
            out.append(val)
    return out


def _mean(values: list[float]) -> float:
    return sum(values) / len(values)


def _sigma(values: list[float]) -> float:
    if len(values) < 2:
        return 0.0
    m = _mean(values)
    return math.sqrt(sum((x - m) ** 2 for x in values) / len(values))


def _slope(values: list[float]) -> float:
    """Simple linear slope: (last - first) / (n - 1)."""
    if len(values) < 2:
        return 0.0
    return (values[-1] - values[0]) / (len(values) - 1)


def _consecutive_decreases(values: list[float]) -> int:
    """Longest streak of consecutive drops in the series."""
    best = 0
    current = 0
    for i in range(1, len(values)):
        if values[i] < values[i - 1]:
            current += 1
            best = max(best, current)
        else:
            current = 0
    return best


def _median(values: list[float]) -> float:
    s = sorted(values)
    n = len(s)
    if n % 2 == 1:
        return s[n // 2]
    return (s[n // 2 - 1] + s[n // 2]) / 2.0


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

def analyze_trend(entries: list[dict]) -> TrendResult:
    """Analyse a chronological list of session_result dicts.

    Parameters
    ----------
    entries : list[dict]
        Each dict is the output of ``SessionResultSchema.to_dict()``,
        ordered from oldest to newest.

    Returns
    -------
    TrendResult
        Full trend analysis.  Check ``.status`` first —
        ``"insufficient_data"`` means the other fields are not meaningful.
    """
    measured = _extract_measured(entries)
    composites = _composites(measured)

    # --- insufficient data -------------------------------------------------
    if len(composites) < _MIN_SESSIONS:
        return TrendResult(
            status="insufficient_data",
            sessions_analysed=len(entries),
            measured_sessions=len(composites),
            mean_composite=None,
            sigma=None,
            slope=None,
            absorbed=False,
            absorption_detail=(
                f"Need at least {_MIN_SESSIONS} measured sessions, "
                f"have {len(composites)}."
            ),
        )

    # --- window stats (last _WINDOW) ---------------------------------------
    window = composites[-_WINDOW:]
    w_mean = round(_mean(window), 4)
    w_sigma = round(_sigma(window), 4)
    w_slope = round(_slope(window), 4)

    # --- absorption --------------------------------------------------------
    sample = composites[-_ABSORPTION_SAMPLE:] if len(composites) >= _ABSORPTION_SAMPLE else composites
    med = _median(sample)
    within_band = sum(1 for v in sample if abs(v - med) <= _ABSORPTION_BAND)
    absorbed = (
        len(sample) >= _ABSORPTION_SAMPLE
        and within_band >= _ABSORPTION_THRESHOLD
    )
    absorption_detail = (
        f"{within_band}/{len(sample)} sessions within ±{int(_ABSORPTION_BAND * 100)}pp "
        f"of median ({med:.0%}). "
        + ("Behaviour absorbed." if absorbed else "Not yet stable.")
    )

    # --- per-metric trends -------------------------------------------------
    metric_trends: list[MetricTrend] = []
    for m in _METRICS:
        series = _breakdown_series(measured[-_WINDOW:], m)
        if len(series) < 2:
            continue
        m_slope = round(_slope(series), 4)
        m_consec = _consecutive_decreases(series)
        metric_trends.append(MetricTrend(
            name=m,
            slope=m_slope,
            values=[round(v, 4) for v in series],
            declining=m_slope < _METRIC_DECLINE,
            consecutive_drops=m_consec,
        ))

    # --- hidden regressions ------------------------------------------------
    # composite flat or improving, but a metric has persistent decline
    composite_ok = w_slope >= _SLOPE_REGRESSING
    hidden: list[str] = []
    for mt in metric_trends:
        if mt.declining and mt.consecutive_drops >= _CONSECUTIVE_DECLINE and composite_ok:
            hidden.append(mt.name)

    # --- overall status ----------------------------------------------------
    if w_sigma > _SIGMA_HIGH:
        status = "intermittent"
    elif w_slope > _SLOPE_IMPROVING:
        status = "improving"
    elif w_slope < _SLOPE_REGRESSING:
        status = "regressing"
    else:
        status = "stable"

    return TrendResult(
        status=status,
        sessions_analysed=len(entries),
        measured_sessions=len(composites),
        mean_composite=w_mean,
        sigma=w_sigma,
        slope=w_slope,
        absorbed=absorbed,
        absorption_detail=absorption_detail,
        metric_trends=metric_trends,
        hidden_regressions=hidden,
    )
