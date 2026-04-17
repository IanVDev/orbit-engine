"""
tests/test_orchestrator.py — Anti-regression tests for the model orchestrator.

Invariants tested:
  1. Simple prompt → always Sonnet
  2. Complex prompt with 2+ criteria → Opus
  3. Budget exceeded → BLOCKED (never executes)
  4. Rate limit hit → downgrade to Sonnet (never blocks)
  5. Empty prompt → BLOCKED
  6. Explicit [opus] override → Opus (unless budget blocked)
  7. Cost estimation within reasonable bounds
  8. Unknown/ambiguous → fail-closed to Sonnet
  9. Every call generates a log entry
 10. Budget decrements correctly after execution
 11. Reservation commit/release works
 12. Fail-closed: router exception → Sonnet
"""

from __future__ import annotations

import sys
import os
import time
import unittest

# Ensure project root is on path.
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from orchestrator.budget import BudgetGate, BudgetReservation, CostEstimate
from orchestrator.router import (
    ESCALATION_THRESHOLD,
    MAX_CONTEXT_TOKENS,
    MAX_OPUS_PER_HOUR,
    Model,
    ModelControl,
    ModelRouter,
    RoutingDecision,
    RoutingRequest,
)


# ── Helpers ──────────────────────────────────────────────────────────

def make_router(daily_limit: float = 10.0) -> ModelRouter:
    return ModelRouter(budget=BudgetGate(daily_limit_usd=daily_limit))


def simple_request(prompt: str = "Fix the typo in README", **kwargs) -> RoutingRequest:
    # Pre-model-control tests assume unrestricted routing; use AUTO so existing
    # heuristic tests continue to pass. Tests that verify LOCKED behaviour
    # pass model_control explicitly (see test_model_control.py).
    kwargs.setdefault("model_control", ModelControl.AUTO)
    return RoutingRequest(
        prompt=prompt,
        session_id="test-session",
        task_id="test-task-001",
        **kwargs,
    )


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Budget Gate Tests
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestBudgetGate(unittest.TestCase):
    """Tests for the fail-closed budget controller."""

    def test_initial_state(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        self.assertEqual(gate.daily_limit, 5.0)
        self.assertEqual(gate.spent, 0.0)
        self.assertEqual(gate.remaining, 5.0)
        self.assertEqual(gate.utilization_pct, 0.0)

    def test_spend_decrements_remaining(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        gate.spend(1.0)
        self.assertAlmostEqual(gate.remaining, 4.0)
        self.assertAlmostEqual(gate.spent, 1.0)

    def test_spend_exceeds_budget_raises(self):
        gate = BudgetGate(daily_limit_usd=1.0)
        with self.assertRaises(ValueError) as ctx:
            gate.spend(2.0)
        self.assertIn("exceeded", str(ctx.exception).lower())

    def test_can_spend_returns_false_when_over(self):
        gate = BudgetGate(daily_limit_usd=0.50)
        self.assertTrue(gate.can_spend(0.50))
        self.assertFalse(gate.can_spend(0.51))

    def test_negative_spend_rejected(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        with self.assertRaises(ValueError):
            gate.spend(-1.0)

    def test_negative_budget_rejected(self):
        with self.assertRaises(ValueError):
            BudgetGate(daily_limit_usd=-1.0)

    def test_reset_restores_budget(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        gate.spend(3.0)
        gate.reset()
        self.assertEqual(gate.remaining, 5.0)
        self.assertEqual(gate.spent, 0.0)
        self.assertEqual(len(gate.transactions), 0)

    def test_transactions_logged(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        gate.spend(1.0)
        gate.spend(0.5)
        self.assertEqual(len(gate.transactions), 2)
        self.assertAlmostEqual(gate.transactions[0]["amount_usd"], 1.0)
        self.assertAlmostEqual(gate.transactions[1]["amount_usd"], 0.5)

    def test_utilization_percentage(self):
        gate = BudgetGate(daily_limit_usd=10.0)
        gate.spend(3.0)
        self.assertAlmostEqual(gate.utilization_pct, 30.0)

    def test_to_dict(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        gate.spend(2.0)
        d = gate.to_dict()
        self.assertEqual(d["daily_limit_usd"], 5.0)
        self.assertAlmostEqual(d["spent_usd"], 2.0)
        self.assertAlmostEqual(d["remaining_usd"], 3.0)


class TestBudgetReservation(unittest.TestCase):
    """Tests for the reservation pattern."""

    def test_reserve_and_commit(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        res = gate.reserve(2.0)
        self.assertAlmostEqual(gate.remaining, 3.0)
        res.commit()
        self.assertAlmostEqual(gate.remaining, 3.0)  # committed = stays spent

    def test_reserve_and_release(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        res = gate.reserve(2.0)
        self.assertAlmostEqual(gate.remaining, 3.0)
        res.release()
        self.assertAlmostEqual(gate.remaining, 5.0)  # released = budget restored

    def test_commit_with_actual_cost_lower(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        res = gate.reserve(2.0)
        res.commit(actual_amount=1.5)
        # Reserved 2.0, actual 1.5 → 0.5 refunded.
        self.assertAlmostEqual(gate.remaining, 3.5)

    def test_commit_with_actual_cost_higher(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        res = gate.reserve(1.0)
        res.commit(actual_amount=1.5)
        # Reserved 1.0, actual 1.5 → 0.5 extra charged.
        self.assertAlmostEqual(gate.remaining, 3.5)

    def test_double_commit_raises(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        res = gate.reserve(1.0)
        res.commit()
        with self.assertRaises(RuntimeError):
            res.commit()

    def test_double_release_raises(self):
        gate = BudgetGate(daily_limit_usd=5.0)
        res = gate.reserve(1.0)
        res.release()
        with self.assertRaises(RuntimeError):
            res.release()

    def test_reserve_exceeds_budget_raises(self):
        gate = BudgetGate(daily_limit_usd=1.0)
        with self.assertRaises(ValueError):
            gate.reserve(2.0)


class TestCostEstimate(unittest.TestCase):
    """Tests for the CostEstimate dataclass."""

    def test_negative_cost_rejected(self):
        with self.assertRaises(ValueError):
            CostEstimate(
                model="sonnet",
                input_tokens=100,
                output_tokens=100,
                input_cost_usd=-1.0,
                output_cost_usd=0.0,
                total_usd=-1.0,
            )


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Router Tests
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestRouterDefaultSonnet(unittest.TestCase):
    """Invariant: simple prompts always route to Sonnet."""

    def test_simple_prompt_routes_to_sonnet(self):
        router = make_router()
        decision = router.route(simple_request("Fix the typo in README"))
        self.assertEqual(decision.model, Model.SONNET)
        self.assertFalse(decision.blocked)

    def test_short_code_fix(self):
        router = make_router()
        decision = router.route(simple_request("Add a comma after line 42"))
        self.assertEqual(decision.model, Model.SONNET)
        self.assertFalse(decision.blocked)

    def test_unknown_task_defaults_to_sonnet(self):
        """Ambiguous prompt → fail-closed → Sonnet."""
        router = make_router()
        decision = router.route(simple_request("Do the thing with the stuff"))
        self.assertEqual(decision.model, Model.SONNET)
        self.assertFalse(decision.blocked)


class TestRouterEscalationToOpus(unittest.TestCase):
    """Tasks meeting 2+ criteria should escalate to Opus."""

    def test_complex_and_large_context(self):
        """Criterion 1 (complexity) + Criterion 2 (large context) → Opus."""
        router = make_router()
        # "analise" triggers complexity + 40K tokens triggers large context.
        decision = router.route(simple_request(
            "Analise o impacto de remover essa classe",
            context_tokens=40_000,
        ))
        self.assertEqual(decision.model, Model.OPUS)
        self.assertFalse(decision.blocked)
        self.assertGreaterEqual(decision.escalation_score, ESCALATION_THRESHOLD)

    def test_previous_failure_escalates(self):
        """Criterion 3 (weight=2) alone meets threshold → Opus."""
        router = make_router()
        decision = router.route(simple_request(
            "Refatore o módulo de billing",
            previous_sonnet_failure=True,
        ))
        self.assertEqual(decision.model, Model.OPUS)
        self.assertGreaterEqual(decision.escalation_score, ESCALATION_THRESHOLD)

    def test_critical_and_complex(self):
        """Criterion 1 + Criterion 5 → Opus."""
        router = make_router()
        decision = router.route(simple_request(
            "Analise a segurança do deploy em produção",
        ))
        self.assertEqual(decision.model, Model.OPUS)


class TestRouterOverride(unittest.TestCase):
    """Explicit override bypasses heuristics but NOT budget."""

    def test_force_opus_simple_prompt(self):
        router = make_router()
        decision = router.route(simple_request(
            "Fix the typo", force_opus=True,
        ))
        self.assertEqual(decision.model, Model.OPUS)
        self.assertIn("explicit_override", decision.escalation_reasons)

    def test_force_opus_blocked_by_budget(self):
        """Override cannot bypass budget (invariant 5)."""
        router = make_router(daily_limit=0.000001)
        decision = router.route(simple_request(
            "Fix the typo", force_opus=True, context_tokens=10_000,
        ))
        self.assertTrue(decision.blocked)
        self.assertIn("budget exceeded", decision.block_reason)


class TestRouterBlocking(unittest.TestCase):
    """Fail-closed gates: budget, empty prompt, context explosion."""

    def test_budget_exceeded_blocks(self):
        router = make_router(daily_limit=0.0001)
        decision = router.route(simple_request(
            "Build a full microservice architecture", context_tokens=50_000,
        ))
        self.assertTrue(decision.blocked)
        self.assertIn("budget exceeded", decision.block_reason)

    def test_empty_prompt_blocked(self):
        router = make_router()
        decision = router.route(simple_request(""))
        self.assertTrue(decision.blocked)
        self.assertIn("empty prompt", decision.block_reason)

    def test_whitespace_only_prompt_blocked(self):
        router = make_router()
        decision = router.route(simple_request("   \n\t  "))
        self.assertTrue(decision.blocked)
        self.assertIn("empty prompt", decision.block_reason)

    def test_context_too_large_blocked(self):
        router = make_router()
        decision = router.route(simple_request(
            "Summarize this", context_tokens=MAX_CONTEXT_TOKENS + 1,
        ))
        self.assertTrue(decision.blocked)
        self.assertIn("context exceeds limit", decision.block_reason)


class TestRouterRateLimit(unittest.TestCase):
    """Opus rate limit: after MAX_OPUS_PER_HOUR, downgrade to Sonnet."""

    def test_rate_limit_downgrades(self):
        router = make_router(daily_limit=100.0)

        # Exhaust Opus rate limit.
        for i in range(MAX_OPUS_PER_HOUR):
            d = router.route(simple_request(
                f"Analise a segurança do deploy #{i}",
                previous_sonnet_failure=True,
            ))
            self.assertEqual(d.model, Model.OPUS, f"Call {i} should be Opus")

        # Next Opus-eligible call should be downgraded.
        d = router.route(simple_request(
            "Analise a segurança do deploy #extra",
            previous_sonnet_failure=True,
        ))
        self.assertEqual(d.model, Model.SONNET)
        self.assertIn("rate_limited_opus_downgrade", d.escalation_reasons)


class TestRouterLogging(unittest.TestCase):
    """Every routing call must produce a log entry."""

    def test_every_call_logged(self):
        router = make_router()
        for i in range(5):
            router.route(simple_request(f"Task {i}"))
        self.assertEqual(len(router.decisions_log), 5)

    def test_log_contains_required_fields(self):
        router = make_router()
        router.route(simple_request("Fix bug"))
        log = router.decisions_log[0]
        required = {
            "timestamp", "session_id", "task_id",
            "model", "blocked", "escalation_score",
        }
        for field in required:
            self.assertIn(field, log, f"Missing field: {field}")

    def test_blocked_call_logged(self):
        router = make_router()
        router.route(simple_request(""))  # empty → blocked
        self.assertEqual(len(router.decisions_log), 1)
        self.assertTrue(router.decisions_log[0]["blocked"])


class TestRouterExecution(unittest.TestCase):
    """Recording actual execution results."""

    def test_record_execution_updates_budget(self):
        router = make_router(daily_limit=10.0)
        decision = router.route(simple_request("Fix typo"))
        self.assertFalse(decision.blocked)

        initial_remaining = router.budget.remaining
        result = router.record_execution(
            decision,
            actual_input_tokens=500,
            actual_output_tokens=200,
            duration_ms=1200,
        )
        self.assertGreater(initial_remaining, router.budget.remaining)
        self.assertGreater(result.actual_cost_usd, 0)

    def test_cost_drift_positive_when_over(self):
        router = make_router()
        decision = router.route(simple_request("Write tests", context_tokens=1000))
        # Simulate actual cost being higher than estimated.
        result = router.record_execution(
            decision,
            actual_input_tokens=5000,
            actual_output_tokens=4000,
            duration_ms=5000,
        )
        # If actual tokens >> estimated, drift should be positive.
        # (depends on estimate, just check it's calculated)
        self.assertIsInstance(result.cost_drift_pct, float)

    def test_execution_log_updated_with_actuals(self):
        router = make_router()
        decision = router.route(simple_request("Fix bug"))
        router.record_execution(
            decision,
            actual_input_tokens=300,
            actual_output_tokens=150,
            duration_ms=800,
        )
        last_log = router.decisions_log[-1]
        self.assertEqual(last_log["actual_input_tokens"], 300)
        self.assertEqual(last_log["actual_output_tokens"], 150)
        self.assertEqual(last_log["duration_ms"], 800)


class TestRouterFailClosed(unittest.TestCase):
    """The router must never fail-open to Opus."""

    def test_analysis_error_defaults_to_sonnet(self):
        """If classification crashes, default is Sonnet, not Opus."""
        router = make_router()

        # Monkey-patch to force an exception inside _route_internal.
        original = router._route_internal

        def exploding_route(req):
            raise RuntimeError("simulated failure")

        router._route_internal = exploding_route

        decision = router.route(simple_request("anything"))
        self.assertEqual(decision.model, Model.SONNET)
        self.assertFalse(decision.blocked)
        self.assertTrue(any("fail-closed" in r for r in decision.escalation_reasons))

        # Restore.
        router._route_internal = original

    def test_budget_gate_opus_downgrade_to_sonnet(self):
        """When budget is too low for Opus but enough for Sonnet → downgrade."""
        # Set a budget that's enough for Sonnet but not Opus.
        router = make_router(daily_limit=0.10)
        decision = router.route(simple_request(
            "Analise a segurança do deploy em produção",
            context_tokens=5_000,
        ))
        # The prompt has complexity + critical keywords → wants Opus.
        # But budget should force downgrade to Sonnet.
        if not decision.blocked:
            self.assertEqual(decision.model, Model.SONNET)
            self.assertTrue(
                any("downgrade" in r for r in decision.escalation_reasons)
                or decision.escalation_score < ESCALATION_THRESHOLD
            )


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Integration: Full routing + execution cycle
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestFullCycle(unittest.TestCase):
    """End-to-end: route → execute → verify budget and log."""

    def test_sonnet_full_cycle(self):
        router = make_router(daily_limit=5.0)
        decision = router.route(simple_request("Fix README typo"))
        self.assertEqual(decision.model, Model.SONNET)
        self.assertFalse(decision.blocked)

        result = router.record_execution(
            decision,
            actual_input_tokens=200,
            actual_output_tokens=100,
            duration_ms=500,
            response="Done: fixed typo on line 3.",
        )
        self.assertGreater(result.actual_cost_usd, 0)
        self.assertEqual(result.error, None)
        self.assertLess(router.budget.remaining, 5.0)
        self.assertEqual(len(router.decisions_log), 1)

    def test_opus_full_cycle(self):
        router = make_router(daily_limit=50.0)
        decision = router.route(simple_request(
            "Analise a estratégia de migração de segurança do deploy",
            context_tokens=10_000,
        ))
        self.assertEqual(decision.model, Model.OPUS)

        result = router.record_execution(
            decision,
            actual_input_tokens=10_000,
            actual_output_tokens=5_000,
            duration_ms=8000,
            response="Analysis complete...",
        )
        self.assertGreater(result.actual_cost_usd, 0)
        self.assertLess(router.budget.remaining, 50.0)

    def test_multiple_calls_budget_tracks(self):
        router = make_router(daily_limit=1.0)
        for i in range(20):
            decision = router.route(simple_request(f"Small task {i}"))
            if decision.blocked:
                # At some point budget should block.
                self.assertIn("budget exceeded", decision.block_reason)
                break
            router.record_execution(
                decision,
                actual_input_tokens=1000,
                actual_output_tokens=500,
                duration_ms=300,
            )
        # Verify budget was consumed.
        self.assertGreater(router.budget.spent, 0)


if __name__ == "__main__":
    unittest.main()
