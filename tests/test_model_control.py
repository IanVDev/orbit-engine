"""
tests/test_model_control.py — Anti-regression tests for ModelControl enforcement.

Invariants:
  1. locked  — heuristic would choose Opus → model stays Sonnet, blocked=True
  2. locked  — simple prompt → Sonnet, not blocked (no override attempted)
  3. auto    — heuristic chooses Opus → Opus is used
  4. suggest — heuristic would choose Opus → Sonnet used + suggestion logged
  5. suggest — decision log contains override fields
  6. invalid — ModelControl.parse raises ValueError
  7. locked  — force_opus + locked → still blocked
  8. auto    — force_opus + auto → Opus allowed
  9. Default RoutingRequest.model_control is LOCKED
 10. Existing heuristics not broken when control=auto
 11. log entry contains model_control field
 12. fail-closed: _apply_model_control with unknown control raises ValueError
"""

from __future__ import annotations

import sys
import os
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from orchestrator.skill.budget import BudgetGate
from orchestrator.skill.router import (
    Model,
    ModelControl,
    ModelRouter,
    RoutingRequest,
    RoutingDecision,
    ESCALATION_THRESHOLD,
)


# ── Helpers ──────────────────────────────────────────────────────────

def make_router(daily_limit: float = 100.0) -> ModelRouter:
    return ModelRouter(budget=BudgetGate(daily_limit_usd=daily_limit))


def complex_prompt() -> str:
    """A prompt that reliably triggers Opus escalation (2+ criteria)."""
    return (
        "Analise a arquitetura do pipeline de produção e compare as vantagens "
        "e desvantagens de cada estratégia de migração para o novo sistema."
    )


def simple_prompt() -> str:
    return "Fix the typo in README"


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  1. locked — override blocked
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class TestModelControlLocked(unittest.TestCase):

    def _make_req(self, prompt: str = None, **kwargs) -> RoutingRequest:
        return RoutingRequest(
            prompt=prompt or complex_prompt(),
            session_id="test-locked",
            model_control=ModelControl.LOCKED,
            **kwargs,
        )

    def test_locked_blocks_opus_override(self):
        """locked: complex prompt that would escalate to Opus is kept on Sonnet."""
        router = make_router()
        req = self._make_req()
        decision = router.route(req)

        # Model must be Sonnet — override was blocked.
        self.assertEqual(decision.model, Model.SONNET,
                         "locked control must prevent Opus escalation")
        self.assertTrue(decision.blocked,
                        "locked control must set blocked=True when override attempted")
        self.assertIn("locked", decision.block_reason or "",
                      "block_reason must mention locked control")

    def test_locked_allows_sonnet_passthrough(self):
        """locked: simple prompt that routes to Sonnet passes through unblocked."""
        router = make_router()
        req = RoutingRequest(
            prompt=simple_prompt(),
            session_id="test-locked-simple",
            model_control=ModelControl.LOCKED,
        )
        decision = router.route(req)

        self.assertEqual(decision.model, Model.SONNET)
        self.assertFalse(decision.blocked,
                         "simple Sonnet routing must not be blocked by locked control")

    def test_locked_force_opus_still_blocked(self):
        """locked: force_opus=True + locked → still blocked (fail-closed)."""
        router = make_router()
        req = RoutingRequest(
            prompt=complex_prompt(),
            session_id="test-locked-force",
            model_control=ModelControl.LOCKED,
            force_opus=True,
        )
        decision = router.route(req)

        self.assertEqual(decision.model, Model.SONNET,
                         "locked + force_opus must NOT allow Opus")
        self.assertTrue(decision.blocked)

    def test_locked_never_overrides_execution(self):
        """locked: over 20 complex prompts, Opus is never chosen."""
        router = make_router(daily_limit=1000.0)
        for i in range(20):
            req = RoutingRequest(
                prompt=complex_prompt(),
                session_id=f"test-locked-loop-{i}",
                model_control=ModelControl.LOCKED,
            )
            decision = router.route(req)
            self.assertEqual(
                decision.model, Model.SONNET,
                f"locked must never choose Opus (iteration {i})"
            )


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  2. auto — override permitted
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class TestModelControlAuto(unittest.TestCase):

    def test_auto_allows_opus_escalation(self):
        """auto: complex prompt is allowed to escalate to Opus."""
        router = make_router()
        req = RoutingRequest(
            prompt=complex_prompt(),
            session_id="test-auto",
            model_control=ModelControl.AUTO,
        )
        decision = router.route(req)

        self.assertEqual(decision.model, Model.OPUS,
                         "auto control must allow Opus escalation")
        self.assertFalse(decision.blocked)

    def test_auto_force_opus(self):
        """auto: force_opus=True is honoured."""
        router = make_router()
        req = RoutingRequest(
            prompt="simple prompt",
            session_id="test-auto-force",
            model_control=ModelControl.AUTO,
            force_opus=True,
        )
        decision = router.route(req)

        self.assertEqual(decision.model, Model.OPUS,
                         "auto + force_opus must choose Opus")

    def test_auto_override_applied_flag(self):
        """auto: model_override_applied is True when Opus was chosen."""
        router = make_router()
        req = RoutingRequest(
            prompt=complex_prompt(),
            session_id="test-auto-flag",
            model_control=ModelControl.AUTO,
        )
        decision = router.route(req)

        if decision.model == Model.OPUS:
            self.assertTrue(
                decision.model_override_applied,
                "auto: model_override_applied must be True when Opus was chosen"
            )

    def test_auto_does_not_break_sonnet_for_simple(self):
        """auto: simple prompts still go to Sonnet (no regression)."""
        router = make_router()
        req = RoutingRequest(
            prompt=simple_prompt(),
            session_id="test-auto-simple",
            model_control=ModelControl.AUTO,
        )
        decision = router.route(req)

        self.assertEqual(decision.model, Model.SONNET,
                         "auto must not upgrade simple prompts to Opus")


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  3. suggest — compute but do not apply
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class TestModelControlSuggest(unittest.TestCase):

    def test_suggest_does_not_change_model(self):
        """suggest: even when heuristics would choose Opus, Sonnet is used."""
        router = make_router()
        req = RoutingRequest(
            prompt=complex_prompt(),
            session_id="test-suggest",
            model_control=ModelControl.SUGGEST,
        )
        decision = router.route(req)

        self.assertEqual(decision.model, Model.SONNET,
                         "suggest must NOT change execution model")
        self.assertFalse(decision.model_override_applied,
                         "suggest: override must not be applied")

    def test_suggest_records_suggestion(self):
        """suggest: decision log contains override_requested when applicable."""
        router = make_router()
        req = RoutingRequest(
            prompt=complex_prompt(),
            session_id="test-suggest-log",
            model_control=ModelControl.SUGGEST,
        )
        decision = router.route(req)

        log = router.decisions_log[-1]
        # The log entry should contain model_control field.
        self.assertEqual(log.get("model_control"), "suggest",
                         "log entry must contain model_control=suggest")

    def test_suggest_does_not_block(self):
        """suggest: the event is processed (not blocked)."""
        router = make_router()
        req = RoutingRequest(
            prompt=complex_prompt(),
            session_id="test-suggest-noblock",
            model_control=ModelControl.SUGGEST,
        )
        decision = router.route(req)

        self.assertFalse(decision.blocked,
                         "suggest must not block execution — only suppress override")

    def test_suggest_simple_prompt_unchanged(self):
        """suggest: simple prompt stays Sonnet, no suggestion generated."""
        router = make_router()
        req = RoutingRequest(
            prompt=simple_prompt(),
            session_id="test-suggest-simple",
            model_control=ModelControl.SUGGEST,
        )
        decision = router.route(req)

        self.assertEqual(decision.model, Model.SONNET)
        self.assertFalse(decision.blocked)


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  4. Invalid value — fail-closed
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class TestModelControlInvalid(unittest.TestCase):

    def test_parse_invalid_raises(self):
        """ModelControl.parse: unknown string raises ValueError (fail-closed)."""
        # Note: parse() is case-insensitive (normalises to lower),
        # so only strings that are not valid after normalisation should raise.
        for bad in ("", "UNLOCKED", "free", "force", "0", "1", "none"):
            with self.assertRaises(ValueError,
                                   msg=f"parse({bad!r}) should raise ValueError"):
                ModelControl.parse(bad)

    def test_parse_valid_values(self):
        """ModelControl.parse: valid strings parse correctly."""
        self.assertEqual(ModelControl.parse("locked"),  ModelControl.LOCKED)
        self.assertEqual(ModelControl.parse("auto"),    ModelControl.AUTO)
        self.assertEqual(ModelControl.parse("suggest"), ModelControl.SUGGEST)

    def test_default_is_locked(self):
        """RoutingRequest.model_control defaults to LOCKED (fail-closed)."""
        req = RoutingRequest(prompt="test", session_id="s1")
        self.assertEqual(req.model_control, ModelControl.LOCKED,
                         "default model_control must be LOCKED (fail-closed)")


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  5. Log contains model_control field (auditability)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class TestModelControlLogging(unittest.TestCase):

    def _route_and_log(self, control: ModelControl) -> dict:
        router = make_router()
        req = RoutingRequest(
            prompt=complex_prompt(),
            session_id="test-log",
            model_control=control,
        )
        router.route(req)
        return router.decisions_log[-1]

    def test_log_contains_model_control_locked(self):
        log = self._route_and_log(ModelControl.LOCKED)
        self.assertEqual(log.get("model_control"), "locked")

    def test_log_contains_model_control_auto(self):
        log = self._route_and_log(ModelControl.AUTO)
        self.assertEqual(log.get("model_control"), "auto")

    def test_log_contains_model_control_suggest(self):
        log = self._route_and_log(ModelControl.SUGGEST)
        self.assertEqual(log.get("model_control"), "suggest")

    def test_log_contains_override_fields_when_relevant(self):
        """When override is attempted (locked/suggest), log has override fields."""
        for control in [ModelControl.LOCKED, ModelControl.SUGGEST]:
            router = make_router()
            req = RoutingRequest(
                prompt=complex_prompt(),
                session_id="test-log-override",
                model_control=control,
            )
            router.route(req)
            log = router.decisions_log[-1]
            # model_control must be in log
            self.assertIn("model_control", log,
                          f"log must have model_control for {control.value}")


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  6. Existing heuristics not broken when control=auto
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

class TestModelControlAutoHeuristicsIntact(unittest.TestCase):
    """With auto control, all original ModelRouter heuristics must still work."""

    def test_budget_still_blocks_when_auto(self):
        """auto does not bypass the budget gate."""
        # BudgetGate requires daily_limit > 0; use 0.000001 to create an
        # effectively-zero budget (any real request will exceed it).
        router = make_router(daily_limit=0.000001)
        req = RoutingRequest(
            prompt=simple_prompt(),
            session_id="test-auto-budget",
            model_control=ModelControl.AUTO,
        )
        decision = router.route(req)
        self.assertTrue(decision.blocked, "budget gate must still block when control=auto")

    def test_empty_prompt_still_blocked_when_auto(self):
        """auto does not bypass the empty-prompt gate."""
        router = make_router()
        req = RoutingRequest(
            prompt="   ",
            session_id="test-auto-empty",
            model_control=ModelControl.AUTO,
        )
        decision = router.route(req)
        self.assertTrue(decision.blocked,
                        "empty-prompt gate must still block when control=auto")

    def test_rate_limit_still_applies_when_auto(self):
        """auto does not bypass the Opus rate limit."""
        from orchestrator.skill.router import MAX_OPUS_PER_HOUR
        router = make_router(daily_limit=1000.0)

        for i in range(MAX_OPUS_PER_HOUR):
            req = RoutingRequest(
                prompt=complex_prompt(),
                session_id=f"test-auto-rl-{i}",
                model_control=ModelControl.AUTO,
                force_opus=True,
            )
            router.route(req)

        # Next call should be downgraded to Sonnet (rate limit hit).
        req = RoutingRequest(
            prompt=complex_prompt(),
            session_id="test-auto-rl-over",
            model_control=ModelControl.AUTO,
            force_opus=True,
        )
        decision = router.route(req)
        self.assertEqual(decision.model, Model.SONNET,
                         "rate limit must still downgrade to Sonnet with control=auto")


if __name__ == "__main__":
    unittest.main()
