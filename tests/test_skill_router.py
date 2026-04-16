"""
tests/test_skill_router.py — Anti-regression tests for the SkillRouter.

Mirrors the structure of test_orchestrator.py for ModelRouter.

Invariants tested:
  1.  Simple/trivial text → NOT activated
  2.  Text with 2+ signals → activated
  3.  Empty text → NOT activated (suppressed)
  4.  Trivial message ("thanks") → NOT activated (suppressed)
  5.  DIAGNOSIS block → automatic activation (weight=3)
  6.  Explicit command ("analyze cost") → automatic activation
  7.  Force override → activated
  8.  Crash during analysis → NOT activated (fail-closed)
  9.  Every call generates a log entry
  10. Log contains required fields (score, threshold, signals, phase)
  11. Phase inferred correctly from turn count
  12. Multiple activations allowed
  13. Waste patterns detected and capped
  14. Audit terms + structured analysis → activated (1+1 ≥ 2)
  15. Custom threshold respected
"""

from __future__ import annotations

import sys
import os
import unittest

# Ensure project root is on path.
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from orchestrator.skill_router import (
    DEFAULT_ACTIVATION_THRESHOLD,
    ActivationDecision,
    ActivationRequest,
    Phase,
    SkillRouter,
)


# ── Helpers ──────────────────────────────────────────────────────────

def make_router(threshold: int = DEFAULT_ACTIVATION_THRESHOLD) -> SkillRouter:
    return SkillRouter(threshold=threshold)


def simple_request(
    text: str = "Fix the typo in README",
    turn_count: int = 1,
    **kwargs,
) -> ActivationRequest:
    return ActivationRequest(
        text=text,
        session_id="test-session",
        turn_number=turn_count,
        turn_count=turn_count,
        **kwargs,
    )


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Default: NOT activated
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestSkillDefaultInactive(unittest.TestCase):
    """Invariant: simple/trivial text never activates the skill."""

    def test_simple_prompt_not_activated(self):
        router = make_router()
        decision = router.evaluate(simple_request("Fix the typo in README"))
        self.assertFalse(decision.activated)
        self.assertEqual(decision.score, 0)

    def test_short_code_fix_not_activated(self):
        router = make_router()
        decision = router.evaluate(simple_request("Add a comma after line 42"))
        self.assertFalse(decision.activated)

    def test_casual_question_not_activated(self):
        router = make_router()
        decision = router.evaluate(simple_request("What does reduce() do?"))
        self.assertFalse(decision.activated)

    def test_first_message_not_activated(self):
        """First message of session with no history → no activation."""
        router = make_router()
        decision = router.evaluate(simple_request(
            "I want to build a REST API", turn_count=1,
        ))
        self.assertFalse(decision.activated)


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Suppression gates
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestSkillSuppression(unittest.TestCase):
    """Gates that prevent activation for empty/trivial input."""

    def test_empty_text_suppressed(self):
        router = make_router()
        decision = router.evaluate(simple_request(""))
        self.assertFalse(decision.activated)
        self.assertTrue(decision.suppressed)
        self.assertEqual(decision.suppression_reason, "empty text")

    def test_whitespace_only_suppressed(self):
        router = make_router()
        decision = router.evaluate(simple_request("   \n\t  "))
        self.assertFalse(decision.activated)
        self.assertTrue(decision.suppressed)

    def test_thanks_suppressed(self):
        router = make_router()
        decision = router.evaluate(simple_request("thanks"))
        self.assertFalse(decision.activated)
        self.assertTrue(decision.suppressed)
        self.assertEqual(decision.suppression_reason, "trivial_message")

    def test_obrigado_suppressed(self):
        router = make_router()
        decision = router.evaluate(simple_request("obrigado"))
        self.assertFalse(decision.activated)
        self.assertTrue(decision.suppressed)

    def test_looks_good_suppressed(self):
        router = make_router()
        decision = router.evaluate(simple_request("looks good"))
        self.assertFalse(decision.activated)
        self.assertTrue(decision.suppressed)

    def test_ok_suppressed(self):
        router = make_router()
        decision = router.evaluate(simple_request("ok"))
        self.assertFalse(decision.activated)
        self.assertTrue(decision.suppressed)


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Signal detection → activation
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestSkillActivationSignals(unittest.TestCase):
    """Text with sufficient signals should activate."""

    def test_diagnosis_block_activates(self):
        """Signal 1: DIAGNOSIS present → weight 3 → automatic."""
        router = make_router()
        decision = router.evaluate(simple_request(
            "DIAGNOSIS\n- repeated edits to the same file\nRisk: medium"
        ))
        self.assertTrue(decision.activated)
        self.assertGreaterEqual(decision.score, DEFAULT_ACTIVATION_THRESHOLD)
        self.assertIn("diagnosis_block_present", decision.signals)

    def test_explicit_analyze_cost_activates(self):
        """Signal 5: explicit command → weight 3 → automatic."""
        router = make_router()
        decision = router.evaluate(simple_request("analyze cost"))
        self.assertTrue(decision.activated)
        self.assertIn("explicit_activation_command", decision.signals)

    def test_explicit_slash_command_activates(self):
        router = make_router()
        decision = router.evaluate(simple_request("/analyze-cost"))
        self.assertTrue(decision.activated)

    def test_explicit_optimize_this_activates(self):
        router = make_router()
        decision = router.evaluate(simple_request("optimize this"))
        self.assertTrue(decision.activated)

    def test_explicit_is_this_optimal_activates(self):
        router = make_router()
        decision = router.evaluate(simple_request("is this optimal?"))
        self.assertTrue(decision.activated)

    def test_apply_orbit_engine_activates(self):
        router = make_router()
        decision = router.evaluate(simple_request(
            "Before answering, apply orbit-engine"
        ))
        self.assertTrue(decision.activated)

    def test_audit_plus_structured_activates(self):
        """Signal 4 (audit) + Signal 3 (structured) → 1+1=2 ≥ threshold."""
        router = make_router()
        decision = router.evaluate(simple_request(
            "Faça um diff exaustivo e apresente numa matriz de comparação"
        ))
        self.assertTrue(decision.activated)
        self.assertGreaterEqual(decision.score, 2)
        self.assertIn("audit_precision_terms", decision.signals)
        self.assertIn("structured_analysis", decision.signals)

    def test_deterministic_plus_audit_activates(self):
        """Signal 2 (deterministic) + Signal 4 (audit) → 1+1=2 ≥ threshold."""
        router = make_router()
        decision = router.evaluate(simple_request(
            "Quero 0% suposição. Verificação byte-idêntico."
        ))
        self.assertTrue(decision.activated)
        self.assertIn("deterministic_language", decision.signals)
        self.assertIn("audit_precision_terms", decision.signals)

    def test_single_weak_signal_not_enough(self):
        """One signal with weight=1 alone → score 1 < threshold 2."""
        router = make_router()
        decision = router.evaluate(simple_request(
            "Mostra isso numa tabela comparativa"
        ))
        # structured_analysis = weight 1, alone not enough
        self.assertFalse(decision.activated)
        self.assertEqual(decision.score, 1)

    def test_deterministic_alone_not_enough(self):
        router = make_router()
        decision = router.evaluate(simple_request(
            "Quero abordagem determinística nesta função"
        ))
        self.assertFalse(decision.activated)
        self.assertEqual(decision.score, 1)


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Override
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestSkillOverride(unittest.TestCase):
    """Forced activation bypasses heuristics."""

    def test_force_activate(self):
        router = make_router()
        decision = router.evaluate(simple_request(
            "Fix the typo", force_activate=True,
        ))
        self.assertTrue(decision.activated)
        self.assertIn("force_activate_override", decision.signals)

    def test_force_activate_on_trivial(self):
        """Force even overrides the trivial-message check (text is not trivial)."""
        router = make_router()
        decision = router.evaluate(simple_request(
            "Do something", force_activate=True,
        ))
        self.assertTrue(decision.activated)


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Fail-closed
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestSkillFailClosed(unittest.TestCase):
    """Error during analysis → NOT activated. Never fail-open."""

    def test_analysis_error_not_activated(self):
        router = make_router()

        # Monkey-patch to force an exception.
        def exploding(req):
            raise RuntimeError("simulated crash")

        router._evaluate_internal = exploding

        decision = router.evaluate(simple_request("anything"))
        self.assertFalse(decision.activated)
        self.assertTrue(any("fail-closed" in s for s in decision.signals))

    def test_analysis_error_still_logged(self):
        router = make_router()

        def exploding(req):
            raise ValueError("boom")

        router._evaluate_internal = exploding

        router.evaluate(simple_request("test"))
        self.assertEqual(len(router.decisions_log), 1)
        self.assertFalse(router.decisions_log[0]["activated"])


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Logging
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestSkillLogging(unittest.TestCase):
    """Every evaluation must produce a log entry — no exceptions."""

    def test_every_call_logged(self):
        router = make_router()
        for i in range(7):
            router.evaluate(simple_request(f"Message {i}"))
        self.assertEqual(len(router.decisions_log), 7)

    def test_log_contains_required_fields(self):
        router = make_router()
        router.evaluate(simple_request("Test message"))
        log = router.decisions_log[0]
        required = {
            "timestamp", "session_id", "turn_number", "turn_count",
            "activated", "score", "threshold", "signals", "phase",
            "metric_labels",
        }
        for f in required:
            self.assertIn(f, log, f"Missing field: {f}")

    def test_suppressed_call_logged(self):
        router = make_router()
        router.evaluate(simple_request(""))  # empty → suppressed
        self.assertEqual(len(router.decisions_log), 1)
        self.assertFalse(router.decisions_log[0]["activated"])

    def test_activated_call_logged(self):
        router = make_router()
        router.evaluate(simple_request("analyze cost"))
        self.assertEqual(len(router.decisions_log), 1)
        self.assertTrue(router.decisions_log[0]["activated"])

    def test_metric_labels_present(self):
        """Log must include metric_labels for Prometheus integration."""
        router = make_router()
        router.evaluate(simple_request("Diff exaustivo numa matriz"))
        log = router.decisions_log[0]
        labels = log["metric_labels"]
        self.assertIn("reason", labels)
        self.assertIn("phase", labels)
        self.assertIn(labels["phase"], ["exploration", "analysis", "consolidation"])


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Phase inference
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestPhaseInference(unittest.TestCase):
    """Phase inferred from turn count."""

    def test_turn_1_is_exploration(self):
        router = make_router()
        decision = router.evaluate(simple_request("Start", turn_count=1))
        self.assertEqual(decision.phase, Phase.EXPLORATION)

    def test_turn_5_is_exploration(self):
        router = make_router()
        decision = router.evaluate(simple_request("Still early", turn_count=5))
        self.assertEqual(decision.phase, Phase.EXPLORATION)

    def test_turn_6_is_analysis(self):
        router = make_router()
        decision = router.evaluate(simple_request("Working", turn_count=6))
        self.assertEqual(decision.phase, Phase.ANALYSIS)

    def test_turn_15_is_analysis(self):
        router = make_router()
        decision = router.evaluate(simple_request("Deep work", turn_count=15))
        self.assertEqual(decision.phase, Phase.ANALYSIS)

    def test_turn_16_is_consolidation(self):
        router = make_router()
        decision = router.evaluate(simple_request("Wrapping up", turn_count=16))
        self.assertEqual(decision.phase, Phase.CONSOLIDATION)

    def test_turn_50_is_consolidation(self):
        router = make_router()
        decision = router.evaluate(simple_request("Very long session", turn_count=50))
        self.assertEqual(decision.phase, Phase.CONSOLIDATION)


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Multiple activations
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestMultipleActivations(unittest.TestCase):
    """Multiple activations allowed in the same session."""

    def test_multiple_activations_counted(self):
        router = make_router()
        router.evaluate(simple_request("analyze cost"))
        router.evaluate(simple_request("is this optimal?"))
        router.evaluate(simple_request("Fix typo"))  # not activated
        router.evaluate(simple_request("/analyze-cost"))
        self.assertEqual(router.activation_count, 3)

    def test_activation_count_excludes_non_activated(self):
        router = make_router()
        for _ in range(10):
            router.evaluate(simple_request("Simple task"))
        self.assertEqual(router.activation_count, 0)


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Waste pattern detection
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestWastePatterns(unittest.TestCase):
    """Waste pattern signals are detected and capped at 2."""

    def test_single_waste_pattern_weight_1(self):
        router = make_router()
        decision = router.evaluate(simple_request(
            "Estou vendo um correction chain neste histórico"
        ))
        # waste_patterns = weight 1, alone < threshold 2 → not activated
        self.assertFalse(decision.activated)
        self.assertEqual(decision.score, 1)

    def test_two_waste_patterns_activates(self):
        """Two waste patterns = weight 2 ≥ threshold."""
        router = make_router()
        decision = router.evaluate(simple_request(
            "Vejo correction chain e repeated edit no histórico"
        ))
        self.assertTrue(decision.activated)
        self.assertGreaterEqual(decision.score, 2)

    def test_waste_patterns_capped_at_2(self):
        """Even with 4 waste patterns, max contribution is 2."""
        router = make_router()
        decision = router.evaluate(simple_request(
            "Correction chain, repeated edit, exploratory search, "
            "validation theater — tudo nessa sessão"
        ))
        # 4 patterns found, but capped at 2
        self.assertTrue(decision.activated)
        self.assertTrue(any("4 found, 2 counted" in s for s in decision.signals))


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Custom threshold
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestCustomThreshold(unittest.TestCase):
    """Threshold is configurable — not hardcoded."""

    def test_higher_threshold_harder_to_activate(self):
        router = make_router(threshold=5)
        decision = router.evaluate(simple_request(
            "Diff exaustivo numa matriz de comparação"
        ))
        # audit (1) + structured (1) = 2 < 5
        self.assertFalse(decision.activated)
        self.assertEqual(decision.threshold, 5)

    def test_lower_threshold_easier_to_activate(self):
        router = make_router(threshold=1)
        decision = router.evaluate(simple_request(
            "Mostra numa tabela comparativa"
        ))
        # structured (1) ≥ 1
        self.assertTrue(decision.activated)
        self.assertEqual(decision.threshold, 1)

    def test_threshold_in_decision(self):
        """Decision always reports the active threshold."""
        router = make_router(threshold=7)
        decision = router.evaluate(simple_request("anything"))
        self.assertEqual(decision.threshold, 7)


# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
#  Integration: full evaluation cycle
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


class TestFullCycle(unittest.TestCase):
    """End-to-end: mixed activations and non-activations."""

    def test_realistic_session(self):
        router = make_router()

        # Turn 1: simple greeting → no activation
        d1 = router.evaluate(simple_request("Hello, let's start", turn_count=1))
        self.assertFalse(d1.activated)
        self.assertEqual(d1.phase, Phase.EXPLORATION)

        # Turn 3: simple task → no activation
        d2 = router.evaluate(simple_request("Fix the bug in line 42", turn_count=3))
        self.assertFalse(d2.activated)

        # Turn 7: user asks for analysis → analyze cost
        d3 = router.evaluate(simple_request("analyze cost", turn_count=7))
        self.assertTrue(d3.activated)
        self.assertEqual(d3.phase, Phase.ANALYSIS)

        # Turn 10: normal work → no activation
        d4 = router.evaluate(simple_request("Add error handling", turn_count=10))
        self.assertFalse(d4.activated)

        # Turn 12: audit + structured → activation via signals
        d5 = router.evaluate(simple_request(
            "Faz diff exaustivo linha-a-linha e mostra numa matriz", turn_count=12,
        ))
        self.assertTrue(d5.activated)
        self.assertEqual(d5.phase, Phase.ANALYSIS)

        # Turn 20: consolidation
        d6 = router.evaluate(simple_request(
            "Looks good, let's wrap up", turn_count=20,
        ))
        self.assertFalse(d6.activated)
        self.assertEqual(d6.phase, Phase.CONSOLIDATION)

        # Verify totals.
        self.assertEqual(router.activation_count, 2)
        self.assertEqual(len(router.decisions_log), 6)


if __name__ == "__main__":
    unittest.main()
