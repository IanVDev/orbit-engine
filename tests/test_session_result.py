"""
Anti-regression tests for SESSION RESULT contract v1.

Tests assert against the SessionResultSchema produced by build_session_result()
and against the rendered output of format_session_result().
They do NOT depend on evolve.py, the test runner, or any external state.

Run:
    python3 tests/test_session_result.py
"""

from __future__ import annotations

import sys
from pathlib import Path

_TESTS_DIR = Path(__file__).parent
sys.path.insert(0, str(_TESTS_DIR))

from decision_engine import (
    ImpactFeedback,
    SessionImpact,
    SessionResultSchema,
    Verdict,
    _CAUSALITY_MSG,
    _METRIC_FLOOR,
    _W_EFFICIENCY,
    _W_OUTPUT,
    _W_REWORK,
    build_session_result,
    compute_real_impact,
    format_session_result,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_impact(sessions: list[SessionImpact]) -> ImpactFeedback:
    result = compute_real_impact(sessions)
    return ImpactFeedback(sessions=sessions, result=result)


def _good_sessions(n: int = 3) -> list[SessionImpact]:
    """n sessions where the skill clearly helps (>20% composite expected)."""
    return [
        SessionImpact(
            action_followed=True,
            rework_before=8, rework_after=1,
            message_count_before=30, message_count_after=12,
            output_size_before=800, output_size_after=200,
        )
        for _ in range(n)
    ]


def _tradeoff_sessions() -> list[SessionImpact]:
    """rework improves strongly, efficiency worsens — composite still positive."""
    return [
        SessionImpact(
            action_followed=True,
            rework_before=10, rework_after=1,
            message_count_before=15, message_count_after=25,
            output_size_before=500, output_size_after=300,
        )
        for _ in range(3)
    ]


# ---------------------------------------------------------------------------
# Test runner (stdlib only)
# ---------------------------------------------------------------------------

_RESULTS: list[tuple[str, bool, str]] = []


def _test(name: str) -> "callable":
    """Decorator — registers and runs a test function."""
    def wrapper(fn):
        try:
            fn()
            _RESULTS.append((name, True, ""))
        except AssertionError as exc:
            _RESULTS.append((name, False, str(exc)))
        return fn
    return wrapper


# ---------------------------------------------------------------------------
# Test 1 — skill-suggested origin produces causality message
# ---------------------------------------------------------------------------

@_test("SR-01  causality message when origin=skill-suggested")
def test_causality_skill_suggested():
    impact = _make_impact(_good_sessions())
    schema = build_session_result(impact, "skill-suggested", Verdict.ACCEPT)

    assert schema.causality_message == _CAUSALITY_MSG, (
        f"Expected causality_message == {_CAUSALITY_MSG!r}, "
        f"got {schema.causality_message!r}"
    )
    assert schema.origin == "skill-suggested"
    assert schema.schema_version == "v1"

    rendered = format_session_result(schema)
    assert _CAUSALITY_MSG in rendered, (
        "Causality message not found in rendered output"
    )
    assert "🔁" in rendered


# ---------------------------------------------------------------------------
# Test 2 — no impact → schema measured=False, all n/a fields None
# ---------------------------------------------------------------------------

@_test("SR-02  no impact data → measured=False, no breakdown")
def test_no_impact():
    schema = build_session_result(None, "manual", Verdict.HOLD)

    assert schema.measured is False
    assert schema.impact_percent is None
    assert schema.impact_status == "n/a"
    assert schema.breakdown_rework is None
    assert schema.breakdown_efficiency is None
    assert schema.breakdown_output is None
    assert schema.tradeoff_detected is False
    assert schema.tradeoff_metrics == []
    assert schema.causality_message is None

    rendered = format_session_result(schema)
    assert "n/a" in rendered
    assert "--impact" in rendered  # instruction to user


# ---------------------------------------------------------------------------
# Test 3 — tradeoff detection: composite positive + metric regresses
# ---------------------------------------------------------------------------

@_test("SR-03  tradeoff detected when composite>0 and metric regresses")
def test_tradeoff_detected():
    impact = _make_impact(_tradeoff_sessions())
    schema = build_session_result(impact, "manual", Verdict.HOLD)

    # Composite should be positive (rework dominates at 50% weight)
    assert schema.measured is True
    assert schema.impact_percent is not None and schema.impact_percent > 0, (
        f"Expected positive composite, got {schema.impact_percent}"
    )

    # Efficiency should be regressing
    assert schema.breakdown_efficiency is not None
    assert schema.breakdown_efficiency < _METRIC_FLOOR * 100, (
        f"Expected efficiency regression, got {schema.breakdown_efficiency}%"
    )

    # Schema must flag it
    assert schema.tradeoff_detected is True, "tradeoff_detected should be True"
    assert "efficiency" in schema.tradeoff_metrics, (
        f"efficiency missing from tradeoff_metrics: {schema.tradeoff_metrics}"
    )

    rendered = format_session_result(schema)
    assert "TRADEOFF DETECTED" in rendered
    assert "⚠" in rendered


# ---------------------------------------------------------------------------
# Test 4 — composite weights sum to 1.0 and match declared constants
# ---------------------------------------------------------------------------

@_test("SR-04  composite_weights sum to 1.0 and match _W_* constants")
def test_composite_weights():
    schema = build_session_result(None, "manual", Verdict.HOLD)
    w = schema.composite_weights

    assert set(w.keys()) == {"rework", "efficiency", "output"}, (
        f"Unexpected weight keys: {set(w.keys())}"
    )

    total = w["rework"] + w["efficiency"] + w["output"]
    assert abs(total - 1.0) < 1e-9, f"Weights sum to {total}, expected 1.0"

    assert w["rework"] == _W_REWORK, (
        f"rework weight {w['rework']} != _W_REWORK {_W_REWORK}"
    )
    assert w["efficiency"] == _W_EFFICIENCY, (
        f"efficiency weight {w['efficiency']} != _W_EFFICIENCY {_W_EFFICIENCY}"
    )
    assert w["output"] == _W_OUTPUT, (
        f"output weight {w['output']} != _W_OUTPUT {_W_OUTPUT}"
    )

    # Verify 50/30/20 specifically — these are the committed values
    assert w["rework"] == 0.50, "rework weight must be 0.50"
    assert w["efficiency"] == 0.30, "efficiency weight must be 0.30"
    assert w["output"] == 0.20, "output weight must be 0.20"


# ---------------------------------------------------------------------------
# Test 5 — composite formula in to_dict() is consistent with actual math
# ---------------------------------------------------------------------------

@_test("SR-05  composite_formula string present, to_dict() is JSON-safe")
def test_formula_and_serialisation():
    impact = _make_impact(_good_sessions())
    schema = build_session_result(impact, "manual", Verdict.ACCEPT)

    # Formula string must mention all three metrics
    assert "rework" in schema.composite_formula
    assert "efficiency" in schema.composite_formula
    assert "output" in schema.composite_formula

    d = schema.to_dict()

    # All required keys present
    required_keys = {
        "schema_version", "origin", "verdict", "measured",
        "impact_percent", "impact_status", "breakdown",
        "composite_formula", "composite_weights",
        "tradeoff_detected", "tradeoff_metrics", "causality_message",
    }
    missing = required_keys - set(d.keys())
    assert not missing, f"to_dict() missing keys: {missing}"

    # Breakdown sub-keys
    assert set(d["breakdown"].keys()) == {"rework", "efficiency", "output"}

    # All values must be JSON-serialisable (no custom types)
    import json
    try:
        json.dumps(d)
    except (TypeError, ValueError) as exc:
        raise AssertionError(f"to_dict() is not JSON-safe: {exc}") from exc

    # Verify impact_percent is rounded to 1 decimal
    pct = d["impact_percent"]
    assert isinstance(pct, float), f"impact_percent must be float, got {type(pct)}"
    assert pct == round(pct, 1), f"impact_percent not rounded to 1dp: {pct}"


# ---------------------------------------------------------------------------
# Test 6 — causality NOT set when origin is not skill-suggested
# ---------------------------------------------------------------------------

@_test("SR-06  no causality message when origin=manual")
def test_no_causality_for_manual():
    impact = _make_impact(_good_sessions())
    schema = build_session_result(impact, "manual", Verdict.ACCEPT)

    assert schema.causality_message is None, (
        f"causality_message should be None for origin=manual, "
        f"got {schema.causality_message!r}"
    )
    rendered = format_session_result(schema)
    assert _CAUSALITY_MSG not in rendered
    assert "🔁" not in rendered


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------

def main() -> int:
    print()
    print("=" * 62)
    print("  SESSION RESULT contract tests  (v1)")
    print("=" * 62)
    print()

    passed = 0
    failed = 0
    for name, ok, msg in _RESULTS:
        icon = "✅" if ok else "❌"
        print(f"  {icon}  {name}")
        if not ok:
            print(f"       ↳ {msg}")
            failed += 1
        else:
            passed += 1

    print()
    print("─" * 62)
    print(f"  Tests: {passed + failed}  |  Passed: {passed}  |  Failed: {failed}")
    print("─" * 62)
    print()

    if failed == 0:
        print("  🟢  All contract tests passed.")
    else:
        print(f"  🔴  {failed} contract test(s) failed.")
    print()

    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
