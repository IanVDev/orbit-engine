"""
Tests for metrics_report.py — aggregate skill metrics:
  MR-01  Basic aggregate: sessions, adoption, impact stats
  MR-02  All sessions unmeasured → zero impact, adoption 0%
  MR-03  Single measured session → correct min/max/median
  MR-04  Hidden regressions reflected in aggregate
  MR-05  Verdict distribution counts
  MR-06  Stability mirrors trend_analysis status
  MR-07  Positive rate accuracy (mix of positive/negative)
  MR-08  Percentile accuracy (p25, p75)
  MR-09  Format renders all sections
  MR-10  Empty entries list → safe defaults
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from trend_analysis import TrendResult, MetricTrend
from metrics_report import (
    compute_aggregate,
    format_metrics_report,
    AggregateMetrics,
    _median,
    _percentile,
    _VERSION,
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


def _make_measured_entry(
    impact_pct: float,
    *,
    rework: float = 50.0,
    efficiency: float = 50.0,
    output: float = 50.0,
    verdict: str = "ACCEPT",
    origin: str = "manual",
) -> dict:
    """Build a minimal measured session_result dict."""
    return {
        "schema_version": "v1",
        "origin": origin,
        "verdict": verdict,
        "measured": True,
        "impact_percent": impact_pct,
        "impact_status": "positive" if impact_pct > 0 else "negative",
        "breakdown": {
            "rework": rework,
            "efficiency": efficiency,
            "output": output,
        },
        "composite_formula": "",
        "composite_weights": {"rework": 0.50, "efficiency": 0.30, "output": 0.20},
        "tradeoff_detected": False,
        "tradeoff_metrics": [],
        "causality_message": None,
    }


def _make_unmeasured_entry(verdict: str = "HOLD") -> dict:
    return {
        "schema_version": "v1",
        "origin": "manual",
        "verdict": verdict,
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


# ---------------------------------------------------------------------------
# MR-01 — Basic aggregate
# ---------------------------------------------------------------------------

@_test("MR-01  basic aggregate: sessions, adoption, impact stats")
def test_basic_aggregate():
    entries = [
        _make_measured_entry(70.0),
        _make_measured_entry(50.0),
        _make_measured_entry(60.0),
        _make_measured_entry(80.0),
        _make_measured_entry(40.0),
        _make_unmeasured_entry(),
    ]
    agg = compute_aggregate(entries)

    assert agg.total_sessions == 6
    assert agg.measured_sessions == 5
    assert agg.unmeasured_sessions == 1
    # adoption = 5/6 ≈ 0.8333
    assert 0.83 <= agg.adoption_rate <= 0.84

    # Impact: [40, 50, 60, 70, 80]
    assert agg.mean_impact == 60.0
    assert agg.median_impact == 60.0
    assert agg.min_impact == 40.0
    assert agg.max_impact == 80.0
    assert agg.positive_rate == 1.0  # all positive


# ---------------------------------------------------------------------------
# MR-02 — All unmeasured
# ---------------------------------------------------------------------------

@_test("MR-02  all unmeasured → zero impact, adoption 0%")
def test_all_unmeasured():
    entries = [_make_unmeasured_entry() for _ in range(4)]
    agg = compute_aggregate(entries)

    assert agg.total_sessions == 4
    assert agg.measured_sessions == 0
    assert agg.adoption_rate == 0.0
    assert agg.mean_impact is None
    assert agg.median_impact is None
    assert agg.impact_values == []
    assert agg.positive_rate == 0.0


# ---------------------------------------------------------------------------
# MR-03 — Single measured session
# ---------------------------------------------------------------------------

@_test("MR-03  single measured → min = max = median = mean")
def test_single_measured():
    entries = [_make_measured_entry(55.0)]
    agg = compute_aggregate(entries)

    assert agg.measured_sessions == 1
    assert agg.mean_impact == 55.0
    assert agg.median_impact == 55.0
    assert agg.min_impact == 55.0
    assert agg.max_impact == 55.0
    assert agg.p25_impact == 55.0
    assert agg.p75_impact == 55.0


# ---------------------------------------------------------------------------
# MR-04 — Hidden regressions reflected
# ---------------------------------------------------------------------------

@_test("MR-04  hidden regressions → rate and metric list populated")
def test_hidden_regressions():
    # Build 6 measured entries where efficiency drops consistently
    # but composite stays flat/up (rework rising compensates)
    entries = []
    for i in range(6):
        entries.append(_make_measured_entry(
            impact_pct=55.0 + i * 0.5,
            rework=70.0 + i * 5,     # rising
            efficiency=50.0 - i * 10, # falling hard
            output=60.0 + i * 2,     # rising slightly
        ))

    agg = compute_aggregate(entries)

    # trend_analysis should detect efficiency as hidden regression
    assert agg.hidden_regression_count >= 1
    assert "efficiency" in agg.hidden_regression_metrics
    assert agg.hidden_regression_rate > 0.0
    # rate = count / 3
    assert agg.hidden_regression_rate == round(agg.hidden_regression_count / 3, 4)


# ---------------------------------------------------------------------------
# MR-05 — Verdict distribution
# ---------------------------------------------------------------------------

@_test("MR-05  verdict distribution counts correctly")
def test_verdict_distribution():
    entries = [
        _make_measured_entry(60.0, verdict="ACCEPT"),
        _make_measured_entry(50.0, verdict="ACCEPT"),
        _make_unmeasured_entry(verdict="HOLD"),
        _make_measured_entry(70.0, verdict="REJECT"),
        _make_measured_entry(45.0, verdict="ACCEPT"),
        _make_measured_entry(55.0, verdict="ACCEPT"),
        _make_measured_entry(65.0, verdict="ACCEPT"),
    ]
    agg = compute_aggregate(entries)

    assert agg.verdict_distribution["ACCEPT"] == 5
    assert agg.verdict_distribution["HOLD"] == 1
    assert agg.verdict_distribution["REJECT"] == 1


# ---------------------------------------------------------------------------
# MR-06 — Stability mirrors trend status
# ---------------------------------------------------------------------------

@_test("MR-06  stability mirrors trend_analysis status")
def test_stability():
    # < 5 measured → insufficient_data
    entries = [_make_measured_entry(50.0) for _ in range(3)]
    agg = compute_aggregate(entries)
    assert agg.stability == "insufficient_data"
    assert agg.absorbed is False


# ---------------------------------------------------------------------------
# MR-07 — Positive rate with mixed impacts
# ---------------------------------------------------------------------------

@_test("MR-07  positive rate: mix of positive and negative")
def test_positive_rate():
    entries = [
        _make_measured_entry(30.0),
        _make_measured_entry(-10.0),
        _make_measured_entry(50.0),
        _make_measured_entry(-5.0),
        _make_measured_entry(20.0),
    ]
    agg = compute_aggregate(entries)

    # 3 positive out of 5
    assert agg.positive_rate == 0.6


# ---------------------------------------------------------------------------
# MR-08 — Percentile accuracy
# ---------------------------------------------------------------------------

@_test("MR-08  p25 and p75 computed correctly")
def test_percentiles():
    # Values: 10, 20, 30, 40, 50 → p25=20, p75=40
    entries = [_make_measured_entry(v) for v in [10.0, 20.0, 30.0, 40.0, 50.0]]
    agg = compute_aggregate(entries)

    assert agg.p25_impact == 20.0
    assert agg.p75_impact == 40.0

    # Internal helpers
    assert _median([10, 20, 30, 40, 50]) == 30
    assert _percentile([10, 20, 30, 40, 50], 25) == 20.0
    assert _percentile([10, 20, 30, 40, 50], 75) == 40.0


# ---------------------------------------------------------------------------
# MR-09 — Format renders all sections
# ---------------------------------------------------------------------------

@_test("MR-09  format renders all report sections")
def test_format_sections():
    entries = [_make_measured_entry(60.0 + i) for i in range(6)]
    agg = compute_aggregate(entries)
    text = format_metrics_report(agg)

    assert "METRICS REPORT" in text
    assert f"v{_VERSION}" in text
    assert "sessions" in text.lower()
    assert "total" in text
    assert "measured" in text
    assert "adoption rate" in text
    assert "impact distribution" in text
    assert "mean" in text
    assert "median" in text
    assert "stability" in text
    assert "absorbed" in text.lower()
    assert "confidence distribution" in text
    assert "signal distribution" in text
    assert "verdict distribution" in text


# ---------------------------------------------------------------------------
# MR-10 — Empty entries
# ---------------------------------------------------------------------------

@_test("MR-10  empty entries → safe defaults")
def test_empty_entries():
    agg = compute_aggregate([])

    assert agg.total_sessions == 0
    assert agg.measured_sessions == 0
    assert agg.adoption_rate == 0.0
    assert agg.mean_impact is None
    assert agg.impact_values == []
    assert agg.stability == "insufficient_data"
    assert agg.hidden_regression_count == 0

    text = format_metrics_report(agg)
    assert "METRICS REPORT" in text
    assert "no measured sessions" in text.lower()


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    header = "Metrics report tests"
    print(f"\n{'=' * 62}")
    print(f"  {header}")
    print(f"{'=' * 62}\n")

    passed = 0
    failed = 0
    for label, fn in _tests:
        try:
            fn()
            print(f"  ✅  {label}")
            passed += 1
        except AssertionError as exc:
            print(f"  ❌  {label}: {exc}")
            failed += 1
        except Exception as exc:
            print(f"  ❌  {label}: {type(exc).__name__}: {exc}")
            failed += 1

    print(f"\n{'─' * 62}")
    print(f"  Tests: {passed + failed}  |  Passed: {passed}  |  Failed: {failed}")
    print(f"{'─' * 62}\n")

    if failed == 0:
        print("  🟢  All metrics report tests passed.\n")
    else:
        print(f"  🔴  {failed} test(s) failed.\n")
        raise SystemExit(1)
