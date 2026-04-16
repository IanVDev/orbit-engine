"""
tests/test_integration.py — Integration tests: SkillRouter → Client → Go Tracking Server.

Tests the full pipeline:
  1. SkillRouter.evaluate() produces an ActivationDecision
  2. SkillTrackingClient.track_activation() sends event via POST /track
  3. Go server records Prometheus metrics (verified via /metrics endpoint)

Requires the tracking server running locally. Skip if unavailable.

Run: python -m pytest tests/test_integration.py -v
"""

from __future__ import annotations

import json
import os
import unittest
from unittest.mock import patch

from orchestrator.client import (
    SkillTrackingClient,
    TrackingError,
    TrackingResult,
    estimate_tokens,
)
from orchestrator.skill_router import (
    ActivationDecision,
    ActivationRequest,
    Phase,
    SkillRouter,
)


# ---------------------------------------------------------------------------
# Unit tests — no server required
# ---------------------------------------------------------------------------


class TestEstimateTokens(unittest.TestCase):
    """Token estimation mirrors Go's EstimateTokens."""

    def test_empty_string(self):
        self.assertEqual(estimate_tokens(""), 1)

    def test_short_string(self):
        self.assertEqual(estimate_tokens("abc"), 1)

    def test_normal_string(self):
        text = "a" * 100
        self.assertEqual(estimate_tokens(text), 25)

    def test_long_string(self):
        text = "a" * 4000
        self.assertEqual(estimate_tokens(text), 1000)


class TestClientValidation(unittest.TestCase):
    """SkillTrackingClient constructor validation (fail-closed)."""

    def test_empty_url_rejected(self):
        with self.assertRaises(ValueError):
            SkillTrackingClient("", "sess-1")

    def test_empty_session_rejected(self):
        with self.assertRaises(ValueError):
            SkillTrackingClient("http://localhost:9100", "")

    def test_invalid_mode_rejected(self):
        with self.assertRaises(ValueError):
            SkillTrackingClient("http://localhost:9100", "sess-1", mode="invalid")

    def test_valid_modes_accepted(self):
        for mode in ("auto", "suggest", "off"):
            c = SkillTrackingClient("http://localhost:9100", "sess-1", mode=mode)
            self.assertEqual(c._mode, mode)


class TestClientNotActivated(unittest.TestCase):
    """Client should not send events for non-activated decisions."""

    def test_no_event_for_inactive_decision(self):
        client = SkillTrackingClient("http://localhost:9100", "sess-test")
        decision = ActivationDecision(activated=False, score=0)
        result = client.track_activation(decision)
        self.assertTrue(result.success)
        self.assertEqual(result.error, "not_activated")
        self.assertEqual(client.events_sent, 0)


class TestClientFailClosed(unittest.TestCase):
    """Client must raise TrackingError on connection failure."""

    def test_connection_refused_raises(self):
        # Port 1 is almost certainly not listening
        client = SkillTrackingClient("http://127.0.0.1:1", "sess-fail", timeout_seconds=1.0)
        decision = ActivationDecision(
            activated=True,
            score=3,
            signals=["explicit_activation_command"],
            phase=Phase.ANALYSIS,
        )
        with self.assertRaises(TrackingError):
            client.track_activation(decision, "input text", "output text")

    def test_events_sent_not_incremented_on_failure(self):
        client = SkillTrackingClient("http://127.0.0.1:1", "sess-fail-2", timeout_seconds=1.0)
        decision = ActivationDecision(
            activated=True,
            score=3,
            signals=["diagnosis_block"],
            phase=Phase.EXPLORATION,
        )
        try:
            client.track_activation(decision)
        except TrackingError:
            pass
        self.assertEqual(client.events_sent, 0)


class TestReasonExtraction(unittest.TestCase):
    """_extract_reason should normalize signal names for metric labels."""

    def test_single_signal(self):
        decision = ActivationDecision(
            activated=True, signals=["explicit_activation_command"], phase=Phase.ANALYSIS
        )
        reason = SkillTrackingClient._extract_reason(decision)
        self.assertEqual(reason, "explicit_activation_command")

    def test_spaces_replaced(self):
        decision = ActivationDecision(
            activated=True, signals=["diagnosis block"], phase=Phase.ANALYSIS
        )
        reason = SkillTrackingClient._extract_reason(decision)
        self.assertEqual(reason, "diagnosis_block")

    def test_empty_signals(self):
        decision = ActivationDecision(activated=True, signals=[], phase=Phase.ANALYSIS)
        reason = SkillTrackingClient._extract_reason(decision)
        self.assertEqual(reason, "unknown")

    def test_long_reason_truncated(self):
        decision = ActivationDecision(
            activated=True, signals=["a" * 100], phase=Phase.ANALYSIS
        )
        reason = SkillTrackingClient._extract_reason(decision)
        self.assertLessEqual(len(reason), 64)


class TestEventPayload(unittest.TestCase):
    """Verify the JSON payload structure matches Go SkillEvent."""

    def test_payload_has_required_fields(self):
        client = SkillTrackingClient("http://localhost:9100", "sess-payload")
        event = client._build_event(
            reason="complexity_keywords",
            phase="analysis",
            input_text="hello world",
            output_text="optimized output" * 10,
        )
        required = {
            "event_type", "timestamp", "session_id", "mode", "trigger",
            "estimated_waste", "actions_suggested", "actions_applied",
            "impact_estimated_tokens", "activation_reason", "activation_phase",
        }
        self.assertTrue(required.issubset(set(event.keys())), f"Missing: {required - set(event.keys())}")

    def test_trigger_is_real_usage_client(self):
        client = SkillTrackingClient("http://localhost:9100", "sess-trigger")
        event = client._build_event("test", "analysis", "in", "out")
        self.assertEqual(event["trigger"], "real_usage_client")

    def test_no_reason_fields_when_empty(self):
        client = SkillTrackingClient("http://localhost:9100", "sess-no-reason")
        event = client._build_event("", "", "in", "out")
        self.assertNotIn("activation_reason", event)
        self.assertNotIn("activation_phase", event)


class TestFullPipeline(unittest.TestCase):
    """Integration: SkillRouter → Client → (mocked) Server."""

    def test_router_to_client_pipeline(self):
        """SkillRouter activate → Client builds correct event."""
        router = SkillRouter(threshold=2)
        request = ActivationRequest(
            text="optimize this code, check byte-idêntico diff exaustivo",
            session_id="pipeline-test",
            turn_number=8,
            turn_count=8,
        )
        decision = router.evaluate(request)
        self.assertTrue(decision.activated, f"Expected activation, got {decision}")

        # Client should build event with reason + phase
        client = SkillTrackingClient("http://localhost:9100", "pipeline-test")
        event = client._build_event(
            reason=SkillTrackingClient._extract_reason(decision),
            phase=decision.phase.value if hasattr(decision.phase, 'value') else str(decision.phase),
            input_text="optimize this code",
            output_text="optimized result " * 20,
        )
        self.assertEqual(event["trigger"], "real_usage_client")
        self.assertIn("activation_reason", event)
        self.assertIn("activation_phase", event)
        self.assertGreater(event["impact_estimated_tokens"], 0)

    def test_router_no_activate_no_event(self):
        """SkillRouter does not activate → Client sends nothing."""
        router = SkillRouter(threshold=100)  # impossibly high threshold
        request = ActivationRequest(
            text="thanks, looks good",
            session_id="no-activate",
        )
        decision = router.evaluate(request)
        self.assertFalse(decision.activated)

        client = SkillTrackingClient("http://localhost:9100", "no-activate")
        result = client.track_activation(decision)
        self.assertTrue(result.success)
        self.assertEqual(result.error, "not_activated")
        self.assertEqual(client.events_sent, 0)


# ---------------------------------------------------------------------------
# Live integration test (skip if server not running)
# ---------------------------------------------------------------------------

TRACKING_URL = os.environ.get("ORBIT_TRACKING_URL", "http://localhost:9100")


def _server_available() -> bool:
    """Check if the tracking server is reachable."""
    try:
        import urllib.request
        req = urllib.request.Request(f"{TRACKING_URL}/health", method="GET")
        with urllib.request.urlopen(req, timeout=2):
            return True
    except Exception:
        return False


@unittest.skipUnless(
    _server_available(),
    f"Tracking server not available at {TRACKING_URL}",
)
class TestLiveIntegration(unittest.TestCase):
    """Live tests against a running tracking server."""

    def test_send_activation_event(self):
        """Send a real activation event and verify success."""
        import uuid
        session = f"live-test-{uuid.uuid4().hex[:8]}"
        client = SkillTrackingClient(TRACKING_URL, session, "auto")

        decision = ActivationDecision(
            activated=True,
            score=3,
            signals=["explicit_activation_command"],
            phase=Phase.ANALYSIS,
        )
        result = client.track_activation(
            decision,
            input_text="test input for live integration",
            output_text="test output with optimized result " * 5,
        )
        self.assertTrue(result.success)
        self.assertIn(result.status_code, (200, 201, 202))
        self.assertEqual(client.events_sent, 1)

    def test_metrics_endpoint_has_new_metrics(self):
        """Verify /metrics contains the new SkillRouter metrics."""
        import urllib.request
        req = urllib.request.Request(f"{TRACKING_URL}/metrics", method="GET")
        with urllib.request.urlopen(req, timeout=5) as resp:
            body = resp.read().decode("utf-8")

        # These metrics should exist after any activation event
        for metric in ["orbit_skill_activations_total", "orbit_last_event_timestamp"]:
            self.assertIn(metric, body, f"{metric} not in /metrics output")


if __name__ == "__main__":
    unittest.main()
