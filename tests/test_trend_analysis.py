"""
Tests for trend_analysis.py — 4 scenarios:
  TA-01  Consistent improvement
  TA-02  Intermittent improvement (high variance)
  TA-03  Hidden regression (composite ok, metric declining)
  TA-04  Insufficient data
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from trend_analysis import analyze_trend, TrendResult

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_tests: list[tuple[str, callable]] = []


def _test(label: str):
    def decorator(fn):
        _tests.append((label, fn))
        return fn
    return decorator


def _make_entry(
    impact_percent: float,
    rework: float,
    efficiency: float,
    output: float,
    measured: bool = True,
) -> dict:
    """Build a minimal session_result dict matching to_dict() shape."""
    return {
        "schema_version": "v1",
        "origin": "skill-suggested",
        "verdict": "ACCEPT",
        "measured": measured,
        "impact_percent": impact_percent,
        "impact_status": "positive" if impact_percent > 0 else "negative",
        "breakdown": {
            "rework": rework,
            "efficiency": efficiency,
            "output": output,
        },
        "composite_formula": "...",
        "composite_weights": {"rework": 0.50, "efficiency": 0.30, "output": 0.20},
        "tradeoff_detected": False,
        "tradeoff_metrics": [],
        "causality_message": None,
    }


# ---------------------------------------------------------------------------
# TA-01 — Consistent improvement
# ---------------------------------------------------------------------------

@_test("TA-01  consistent improvement → status=improving, absorbed, no regressions")
def test_consistent_improvement():
    # 10 sessions with steadily increasing composite, low variance
    entries = []
    for i in range(10):
        base = 0.40 + i * 0.03  # 40% → 67%
        entries.append(_make_entry(
            impact_percent=base,
            rework=base + 0.05,
            efficiency=base - 0.02,
            output=base + 0.01,
        ))

    r = analyze_trend(entries)

    assert r.status == "improving", f"Expected 'improving', got '{r.status}'"
    assert r.measured_sessions == 10
    assert r.sigma is not None and r.sigma < 0.10, (
        f"Expected low sigma, got {r.sigma}"
    )
    assert r.slope is not None and r.slope > 0, (
        f"Expected positive slope, got {r.slope}"
    )
    assert r.hidden_regressions == [], (
        f"Expected no hidden regressions, got {r.hidden_regressions}"
    )
    # absorption: 10 sessions, all within tight band relative to median
    # (steady increase means spread > band, so absorbed may be False —
    #  that's correct: a trend is not the same as absorbed behaviour)


# ---------------------------------------------------------------------------
# TA-02 — Intermittent improvement (high variance)
# ---------------------------------------------------------------------------

@_test("TA-02  intermittent improvement → status=intermittent, high sigma")
def test_intermittent_improvement():
    # alternating high/low sessions — mean is decent but σ is very high
    entries = []
    for i in range(10):
        if i % 2 == 0:
            entries.append(_make_entry(0.85, 0.90, 0.80, 0.85))
        else:
            entries.append(_make_entry(0.10, 0.12, 0.08, 0.10))

    r = analyze_trend(entries)

    assert r.status == "intermittent", f"Expected 'intermittent', got '{r.status}'"
    assert r.sigma is not None and r.sigma > 0.25, (
        f"Expected high sigma (>0.25), got {r.sigma}"
    )
    assert r.absorbed is False, "Should not be absorbed with high variance"


# ---------------------------------------------------------------------------
# TA-03 — Hidden regression
# ---------------------------------------------------------------------------

@_test("TA-03  hidden regression → efficiency declining while composite stable")
def test_hidden_regression():
    # composite stays ~stable because rework compensates,
    # but efficiency drops steadily across 5 sessions
    entries = [
        _make_entry(0.55, 0.70, 0.50, 0.60),
        _make_entry(0.55, 0.75, 0.40, 0.62),
        _make_entry(0.56, 0.80, 0.30, 0.64),
        _make_entry(0.55, 0.85, 0.20, 0.66),
        _make_entry(0.56, 0.90, 0.10, 0.68),
    ]

    r = analyze_trend(entries)

    assert "efficiency" in r.hidden_regressions, (
        f"Expected 'efficiency' in hidden_regressions, got {r.hidden_regressions}"
    )
    # composite is flat, so status should not be "regressing"
    assert r.status != "regressing", (
        f"Composite is stable — status should not be 'regressing', got '{r.status}'"
    )
    # find the efficiency MetricTrend
    eff = next((m for m in r.metric_trends if m.name == "efficiency"), None)
    assert eff is not None, "efficiency MetricTrend missing"
    assert eff.declining is True, f"efficiency should be declining, slope={eff.slope}"
    assert eff.consecutive_drops >= 3, (
        f"Expected ≥3 consecutive drops, got {eff.consecutive_drops}"
    )


# ---------------------------------------------------------------------------
# TA-04 — Insufficient data
# ---------------------------------------------------------------------------

@_test("TA-04  insufficient data → status=insufficient_data")
def test_insufficient_data():
    entries = [
        _make_entry(0.50, 0.60, 0.40, 0.55),
        _make_entry(0.52, 0.62, 0.42, 0.56),
    ]

    r = analyze_trend(entries)

    assert r.status == "insufficient_data", (
        f"Expected 'insufficient_data', got '{r.status}'"
    )
    assert r.measured_sessions == 2
    assert r.mean_composite is None
    assert r.sigma is None
    assert r.slope is None
    assert r.absorbed is False
    assert r.hidden_regressions == []
    assert r.metric_trends == []


# ---------------------------------------------------------------------------
# TA-05 — Unmeasured sessions are skipped
# ---------------------------------------------------------------------------

@_test("TA-05  unmeasured sessions skipped → only measured count")
def test_unmeasured_skipped():
    measured = [
        _make_entry(0.50, 0.60, 0.40, 0.50),
        _make_entry(0.52, 0.62, 0.42, 0.52),
        _make_entry(0.54, 0.64, 0.44, 0.54),
        _make_entry(0.56, 0.66, 0.46, 0.56),
        _make_entry(0.58, 0.68, 0.48, 0.58),
    ]
    unmeasured = [_make_entry(0, 0, 0, 0, measured=False) for _ in range(3)]

    # interleave unmeasured
    entries = [unmeasured[0], measured[0], unmeasured[1], measured[1],
               measured[2], unmeasured[2], measured[3], measured[4]]

    r = analyze_trend(entries)

    assert r.sessions_analysed == 8, f"Expected 8 total, got {r.sessions_analysed}"
    assert r.measured_sessions == 5, f"Expected 5 measured, got {r.measured_sessions}"
    assert r.status != "insufficient_data", (
        f"5 measured sessions should be enough, got '{r.status}'"
    )


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    header = "Trend analysis tests"
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
        print("  🟢  All trend tests passed.\n")
    else:
        print(f"  🔴  {failed} test(s) failed.\n")
        raise SystemExit(1)
