"""tests/test_shadow_client.py — shadow mode invariants for SkillTrackingClient.

Invariants covered:
  1. track_activation() is disallowed in shadow mode (must raise).
  2. observe_decision() sends an event with source=real_shadow and
     activation_possible reflecting the decision.
  3. Shadow tracking failures are SWALLOWED — caller never sees an exception
     even when the server is unreachable or rejects the payload.
  4. A non-activated decision still produces a shadow observation
     (counterfactual measurement).
  5. observe_decision() requires shadow_mode=True.
"""
from __future__ import annotations

import json
import os
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from orchestrator.client import SkillTrackingClient, TrackingError  # noqa: E402
from orchestrator.skill_router import (  # noqa: E402
    ActivationDecision,
    Phase,
)


# ── Test HTTP server ────────────────────────────────────────────────────

class _CapturingHandler(BaseHTTPRequestHandler):
    # Populated by the test via class attributes.
    received: list = []
    status_code: int = 200

    def do_POST(self):  # noqa: N802 — required by BaseHTTPRequestHandler
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length).decode("utf-8")
        try:
            payload = json.loads(body)
        except json.JSONDecodeError:
            payload = {"__malformed__": body}
        type(self).received.append(payload)
        self.send_response(type(self).status_code)
        self.end_headers()
        if 200 <= type(self).status_code < 300:
            self.wfile.write(json.dumps({"status": "ok", "event_id": "x" * 64}).encode())
        else:
            self.wfile.write(json.dumps({"error": "rejected"}).encode())

    def log_message(self, *_a, **_kw):  # quiet
        pass


class _Server:
    def __init__(self, status_code: int = 200):
        _CapturingHandler.received = []
        _CapturingHandler.status_code = status_code
        self.httpd = HTTPServer(("127.0.0.1", 0), _CapturingHandler)
        self.port = self.httpd.server_address[1]
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self.thread.start()

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.port}"

    @property
    def received(self) -> list:
        return _CapturingHandler.received

    def stop(self):
        self.httpd.shutdown()
        self.httpd.server_close()


def _decision(activated: bool) -> ActivationDecision:
    return ActivationDecision(
        activated=activated,
        score=3 if activated else 0,
        threshold=2,
        signals=["explicit_activation_command"] if activated else [],
        phase=Phase.EXPLORATION,
    )


# ── Tests ───────────────────────────────────────────────────────────────

class TestShadowTrackingClient(unittest.TestCase):
    def test_track_activation_disallowed_in_shadow_mode(self):
        client = SkillTrackingClient(
            "http://127.0.0.1:1",
            "sess-x",
            shadow_mode=True,
        )
        with self.assertRaises(RuntimeError):
            client.track_activation(_decision(True), "hi", "hi")

    def test_observe_decision_requires_shadow_mode(self):
        client = SkillTrackingClient(
            "http://127.0.0.1:1",
            "sess-x",
            shadow_mode=False,
        )
        with self.assertRaises(RuntimeError):
            client.observe_decision(_decision(True))

    def test_observe_decision_tags_source_and_possible(self):
        server = _Server(status_code=200)
        try:
            client = SkillTrackingClient(
                server.url,
                "sess-shadow-1",
                shadow_mode=True,
            )
            result = client.observe_decision(
                _decision(activated=True),
                input_text="optimize this",
                output_text="use indexes",
            )
            self.assertTrue(result.success)
            self.assertEqual(len(server.received), 1)
            ev = server.received[0]
            self.assertEqual(ev["source"], "real_shadow")
            self.assertTrue(ev["activation_possible"])
            self.assertEqual(ev["activation_reason"], "explicit_activation_command")
            self.assertEqual(ev["activation_phase"], "exploration")
        finally:
            server.stop()

    def test_observe_non_activated_decision_still_sends_event(self):
        server = _Server(status_code=200)
        try:
            client = SkillTrackingClient(
                server.url,
                "sess-shadow-2",
                shadow_mode=True,
            )
            result = client.observe_decision(_decision(activated=False))
            self.assertTrue(result.success)
            self.assertEqual(len(server.received), 1)
            ev = server.received[0]
            self.assertEqual(ev["source"], "real_shadow")
            self.assertFalse(ev["activation_possible"])
        finally:
            server.stop()

    def test_tracking_failure_is_swallowed(self):
        """Shadow observations NEVER raise on HTTP failure."""
        server = _Server(status_code=500)
        try:
            client = SkillTrackingClient(
                server.url,
                "sess-shadow-3",
                shadow_mode=True,
            )
            # Must NOT raise.
            result = client.observe_decision(_decision(True))
            self.assertFalse(result.success)
            self.assertIsNotNone(result.error)
            self.assertEqual(client.shadow_observations_dropped, 1)
            self.assertEqual(client.shadow_observations_sent, 0)
        finally:
            server.stop()

    def test_unreachable_server_is_swallowed(self):
        # Bind to a port that's closed — connection refused.
        client = SkillTrackingClient(
            "http://127.0.0.1:1",  # almost certainly closed
            "sess-shadow-4",
            shadow_mode=True,
            timeout_seconds=1.0,
        )
        try:
            result = client.observe_decision(_decision(True))
        except TrackingError:
            self.fail("shadow observation must not raise on network failure")
        self.assertFalse(result.success)
        self.assertIsNotNone(result.error)
        self.assertEqual(client.shadow_observations_dropped, 1)


if __name__ == "__main__":
    unittest.main()
