"""
Tests for trend_report.py — 5 scenarios:
  TR-01  Consistent improvement renders correctly
  TR-02  Intermittent improvement renders correctly
  TR-03  Hidden regression renders warning
  TR-04  Insufficient data renders correctly
  TR-05  Legacy evidence_log entries are normalised
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from trend_analysis import TrendResult, MetricTrend
from trend_report import (
    format_trend_report,
    _normalise_entry,
    _confidence,
    _decision_signal,
    _regression_severity,
    _ALGORITHM_VERSION,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_tests: list[tuple[str, callable]] = []


def _test(label: str):
    def decorator(fn):
        _tests.append((label, fn))
        return fn
    return decorator


# ---------------------------------------------------------------------------
# TR-01 — Consistent improvement
# ---------------------------------------------------------------------------

@_test("TR-01  consistent improvement renders status + positive slope")
def test_consistent_improvement():
    r = TrendResult(
        status="improving",
        sessions_analysed=10,
        measured_sessions=10,
        mean_composite=0.55,
        sigma=0.03,
        slope=0.03,
        absorbed=True,
        absorption_detail="10/10 sessions within ±15pp of median (55%). Behaviour absorbed.",
        metric_trends=[
            MetricTrend("rework", 0.04, [0.50, 0.54, 0.58, 0.62, 0.66], False, 0),
            MetricTrend("efficiency", 0.02, [0.40, 0.42, 0.44, 0.46, 0.48], False, 0),
            MetricTrend("output", 0.03, [0.55, 0.58, 0.61, 0.64, 0.67], False, 0),
        ],
        hidden_regressions=[],
    )
    text = format_trend_report(r)

    assert "TREND REPORT" in text
    assert f"v{_ALGORITHM_VERSION}" in text   # algorithm version metadata
    assert "window" in text.lower()           # window size metadata
    assert "improving" in text
    assert "+55%" in text             # mean composite
    assert "0.03" in text             # sigma
    assert "+0.03" in text            # slope
    assert "absorbed" in text.lower()
    assert "yes" in text.lower()      # absorbed = yes
    assert "hidden regressions" not in text.lower()
    assert _confidence(r) == "medium"  # 10 sessions, low sigma
    assert _decision_signal(r) == "safe"
    assert "safe" in text
    assert _regression_severity(r) is None


# ---------------------------------------------------------------------------
# TR-02 — Intermittent improvement
# ---------------------------------------------------------------------------

@_test("TR-02  intermittent improvement renders high sigma warning")
def test_intermittent():
    r = TrendResult(
        status="intermittent",
        sessions_analysed=10,
        measured_sessions=10,
        mean_composite=0.48,
        sigma=0.35,
        slope=0.01,
        absorbed=False,
        absorption_detail="4/10 sessions within ±15pp of median (48%). Not yet stable.",
        metric_trends=[
            MetricTrend("rework", 0.01, [0.80, 0.10, 0.75, 0.15, 0.80], False, 0),
        ],
        hidden_regressions=[],
    )
    text = format_trend_report(r)

    assert "intermittent" in text
    assert "0.35" in text             # high sigma
    assert "no" in text.lower()       # absorbed = no
    assert "Not yet stable" in text or "Not yet" in text  # may be truncated
    assert _confidence(r) == "low"    # high sigma → low confidence
    assert _decision_signal(r) == "caution"
    assert "caution" in text
    assert _regression_severity(r) is None


# ---------------------------------------------------------------------------
# TR-03 — Hidden regression
# ---------------------------------------------------------------------------

@_test("TR-03  hidden regression renders CRITICAL alert + caps confidence")
def test_hidden_regression():
    r = TrendResult(
        status="stable",
        sessions_analysed=10,
        measured_sessions=10,
        mean_composite=0.55,
        sigma=0.04,
        slope=0.001,
        absorbed=True,
        absorption_detail="9/10 sessions within ±15pp of median (55%). Behaviour absorbed.",
        metric_trends=[
            MetricTrend("rework", 0.05, [0.70, 0.75, 0.80, 0.85, 0.90], False, 0),
            MetricTrend("efficiency", -0.10, [0.50, 0.40, 0.30, 0.20, 0.10], True, 4),
            MetricTrend("output", 0.02, [0.60, 0.62, 0.64, 0.66, 0.68], False, 0),
        ],
        hidden_regressions=["efficiency"],
    )
    text = format_trend_report(r)

    assert "CRITICAL" in text                 # critical alert, not just warning
    assert "hidden regressions" in text.lower()
    assert "efficiency" in text
    assert "4 consecutive drops" in text
    assert "confidence capped" in text.lower()
    assert "stable" in text                   # composite status is stable
    assert _confidence(r) == "medium"         # still medium (10 sessions, not high)
    assert _decision_signal(r) == "at_risk"
    assert "at_risk" in text
    assert _regression_severity(r) == "minor"
    assert "minor" in text
    assert "1/3 metrics" in text


# ---------------------------------------------------------------------------
# TR-04 — Insufficient data
# ---------------------------------------------------------------------------

@_test("TR-04  insufficient data renders minimal report")
def test_insufficient():
    r = TrendResult(
        status="insufficient_data",
        sessions_analysed=3,
        measured_sessions=2,
        mean_composite=None,
        sigma=None,
        slope=None,
        absorbed=False,
        absorption_detail="Need at least 5 measured sessions, have 2.",
    )
    text = format_trend_report(r)

    assert "insufficient_data" in text
    assert "2 measured sessions" in text
    assert "Need at least 5" in text
    assert "metric trends" not in text.lower()
    assert _confidence(r) == "none"
    assert _decision_signal(r) == "caution"   # insufficient_data → caution
    assert "caution" in text


# ---------------------------------------------------------------------------
# TR-05 — Legacy entry normalisation
# ---------------------------------------------------------------------------

@_test("TR-05  legacy evidence_log entry normalised correctly")
def test_legacy_normalisation():
    # Legacy format: no session_result, but has impact block
    legacy = {
        "timestamp": "2026-04-14T22:06:24+00:00",
        "origin": "manual",
        "verdict": "ACCEPT",
        "impact": {
            "sessions_analyzed": 4,
            "sessions_followed": 3,
            "rework_reduction": 0.7647,
            "efficiency_gain": 0.5895,
            "output_reduction": 0.702,
            "composite_improvement": 0.6996,
        },
    }

    normalised = _normalise_entry(legacy)

    assert normalised["measured"] is True
    assert normalised["impact_percent"] == 70.0  # 0.6996 * 100 rounded
    assert normalised["breakdown"]["rework"] == 76.5  # 0.7647 * 100 rounded
    assert normalised["breakdown"]["efficiency"] == 59.0
    assert normalised["origin"] == "manual"
    assert normalised["schema_version"] == "v1"

    # Entry with session_result — should pass through
    v1_entry = {
        "session_result": {"schema_version": "v1", "measured": True, "impact_percent": 70.0}
    }
    assert _normalise_entry(v1_entry) == v1_entry["session_result"]

    # Entry with neither — unmeasured
    bare = {"verdict": "HOLD", "origin": "manual"}
    assert _normalise_entry(bare)["measured"] is False


# ---------------------------------------------------------------------------
# TR-06 — Confidence cap from high to medium due to hidden regression
# ---------------------------------------------------------------------------

@_test("TR-06  hidden regression caps confidence from high → medium")
def test_confidence_cap_hidden_regression():
    # 25 sessions, low sigma → would be "high" confidence,
    # but hidden_regressions forces cap to "medium"
    r = TrendResult(
        status="stable",
        sessions_analysed=25,
        measured_sessions=25,
        mean_composite=0.60,
        sigma=0.05,
        slope=0.002,
        absorbed=True,
        absorption_detail="10/10 sessions within ±15pp of median (60%). Behaviour absorbed.",
        metric_trends=[
            MetricTrend("rework", 0.03, [0.70, 0.73, 0.76, 0.79, 0.82], False, 0),
            MetricTrend("efficiency", -0.08, [0.55, 0.47, 0.39, 0.31, 0.23], True, 4),
            MetricTrend("output", 0.01, [0.60, 0.61, 0.62, 0.63, 0.64], False, 0),
        ],
        hidden_regressions=["efficiency"],
    )

    # Without hidden regressions, this would be "high"
    r_clean = TrendResult(
        status="stable",
        sessions_analysed=25,
        measured_sessions=25,
        mean_composite=0.60,
        sigma=0.05,
        slope=0.002,
        absorbed=True,
        absorption_detail="...",
        metric_trends=[],
        hidden_regressions=[],
    )
    assert _confidence(r_clean) == "high", "Baseline should be high without regressions"

    # With hidden regressions: capped
    assert _confidence(r) == "medium", (
        f"Expected 'medium' (capped), got '{_confidence(r)}'"
    )

    text = format_trend_report(r)
    assert "CRITICAL" in text
    assert "medium" in text           # capped confidence shown
    assert "confidence capped" in text.lower()
    assert _decision_signal(r) == "at_risk"
    assert "at_risk" in text
    assert _regression_severity(r) == "minor"
    assert "minor" in text
    assert "1/3 metrics" in text


# ---------------------------------------------------------------------------
# TR-07 — Moderate severity (2 hidden regressions)
# ---------------------------------------------------------------------------

@_test("TR-07  two hidden regressions → moderate severity")
def test_moderate_severity():
    r = TrendResult(
        status="stable",
        sessions_analysed=15,
        measured_sessions=15,
        mean_composite=0.50,
        sigma=0.06,
        slope=0.001,
        absorbed=True,
        absorption_detail="12/15 sessions within ±15pp of median (50%). Behaviour absorbed.",
        metric_trends=[
            MetricTrend("rework", 0.03, [0.70, 0.73, 0.76, 0.79, 0.82], False, 0),
            MetricTrend("efficiency", -0.09, [0.50, 0.41, 0.32, 0.23, 0.14], True, 4),
            MetricTrend("output", -0.06, [0.55, 0.49, 0.43, 0.37, 0.31], True, 4),
        ],
        hidden_regressions=["efficiency", "output"],
    )
    text = format_trend_report(r)

    assert _decision_signal(r) == "at_risk"
    assert _regression_severity(r) == "moderate"
    assert "moderate" in text
    assert "2/3 metrics" in text
    assert "CRITICAL" in text
    assert "efficiency" in text
    assert "output" in text


# ---------------------------------------------------------------------------
# TR-08 — Critical severity (3 hidden regressions)
# ---------------------------------------------------------------------------

@_test("TR-08  three hidden regressions → critical severity")
def test_critical_severity():
    r = TrendResult(
        status="stable",
        sessions_analysed=20,
        measured_sessions=20,
        mean_composite=0.45,
        sigma=0.05,
        slope=-0.001,
        absorbed=True,
        absorption_detail="15/20 sessions within ±15pp of median (45%). Behaviour absorbed.",
        metric_trends=[
            MetricTrend("rework", -0.04, [0.60, 0.56, 0.52, 0.48, 0.44], True, 4),
            MetricTrend("efficiency", -0.07, [0.50, 0.43, 0.36, 0.29, 0.22], True, 4),
            MetricTrend("output", -0.05, [0.40, 0.35, 0.30, 0.25, 0.20], True, 4),
        ],
        hidden_regressions=["rework", "efficiency", "output"],
    )
    text = format_trend_report(r)

    assert _decision_signal(r) == "at_risk"
    assert _regression_severity(r) == "critical"
    assert "critical" in text
    assert "3/3 metrics" in text
    assert "CRITICAL" in text
    assert "rework" in text
    assert "efficiency" in text
    assert "output" in text
    # Confidence capped
    assert _confidence(r) == "medium"
    assert "confidence capped" in text.lower()


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    header = "Trend report tests"
    print(f"\n{'=' * 62}")
    print(f"  {header}")
    print(f"{'=' * 62}\n")

    passed = 0
    failed = 0
    for label, fn in _tests:
        try:
            fn()
            print(f"  \u2705  {label}")
            passed += 1
        except AssertionError as exc:
            print(f"  \u274c  {label}: {exc}")
            failed += 1
        except Exception as exc:
            print(f"  \u274c  {label}: {type(exc).__name__}: {exc}")
            failed += 1

    print(f"\n{'─' * 62}")
    print(f"  Tests: {passed + failed}  |  Passed: {passed}  |  Failed: {failed}")
    print(f"{'─' * 62}\n")

    if failed == 0:
        print("  🟢  All trend report tests passed.\n")
    else:
        print(f"  🔴  {failed} test(s) failed.\n")
        raise SystemExit(1)
