"""
orchestrator/client.py — Integration client: SkillRouter → Tracking Server.

Connects the Python-side SkillRouter to the Go tracking server (POST /track).
When the SkillRouter decides to activate, this client:
  1. Sends a SkillEvent with activation_reason + activation_phase
  2. Uses trigger="real_usage_client" (required by server-side gate)
  3. Fail-closed: if tracking fails → returns error, caller MUST abort

Usage:
    from orchestrator.client import SkillTrackingClient
    from orchestrator.skill_router import SkillRouter, ActivationRequest

    router = SkillRouter()
    client = SkillTrackingClient("http://localhost:9100", "my-session", "auto")

    request = ActivationRequest(text="optimize this code", session_id="my-session")
    decision = router.evaluate(request)

    if decision.activated:
        client.track_activation(decision, input_text="...", output_text="...")
"""

from __future__ import annotations

import json
import logging
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Optional

try:
    import urllib.request
    import urllib.error
except ImportError:
    pass  # Should never happen in Python 3

from orchestrator.skill_router import ActivationDecision

logger = logging.getLogger("orbit.client")

# ---------------------------------------------------------------------------
# Token estimation (mirrors Go EstimateTokens)
# ---------------------------------------------------------------------------

def estimate_tokens(text: str) -> int:
    """Conservative token estimate: len(text) / 4, min 1."""
    n = max(len(text) // 4, 1)
    return n


# ---------------------------------------------------------------------------
# Error types
# ---------------------------------------------------------------------------

class TrackingError(Exception):
    """Raised when tracking fails. Caller MUST abort (fail-closed)."""
    pass


# ---------------------------------------------------------------------------
# SkillTrackingClient
# ---------------------------------------------------------------------------

@dataclass
class TrackingResult:
    """Result of a tracking attempt."""
    success: bool
    status_code: int = 0
    error: Optional[str] = None
    event_payload: Optional[dict] = None


class SkillTrackingClient:
    """
    Fail-closed client for sending SkillRouter decisions to the tracking server.

    Pattern:
      - SkillRouter.evaluate() returns ActivationDecision
      - If activated → track_activation() sends event to Go server
      - If tracking fails → raises TrackingError (fail-closed)
      - If not activated → no event sent (no noise)

    The Go server enforces:
      - trigger must contain "real_usage" (server-side gate)
      - rate limit per session_id (2s interval)
      - All 16 Prometheus metrics updated atomically
    """

    def __init__(
        self,
        tracking_url: str,
        session_id: str,
        mode: str = "auto",
        timeout_seconds: float = 5.0,
    ) -> None:
        if not tracking_url:
            raise ValueError("tracking_url is required (fail-closed)")
        if not session_id:
            raise ValueError("session_id is required (fail-closed)")
        if mode not in ("auto", "suggest", "off"):
            raise ValueError(f"invalid mode {mode!r} (want auto|suggest|off)")

        self._track_url = tracking_url.rstrip("/") + "/track"
        self._session_id = session_id
        self._mode = mode
        self._timeout = timeout_seconds
        self._events_sent: int = 0

    # ── Public API ───────────────────────────────────────────────

    def track_activation(
        self,
        decision: ActivationDecision,
        input_text: str = "",
        output_text: str = "",
    ) -> TrackingResult:
        """
        Send an activation event to the tracking server.

        Fail-closed: raises TrackingError on any failure.
        Caller MUST handle this error and abort the skill activation.

        Args:
            decision: ActivationDecision from SkillRouter.evaluate()
            input_text: the user's input (for waste estimation)
            output_text: the skill's output (for token savings)

        Returns:
            TrackingResult on success.

        Raises:
            TrackingError: if tracking fails for any reason.
        """
        if not decision.activated:
            logger.debug("decision not activated — no event to send")
            return TrackingResult(success=True, error="not_activated")

        # Build the primary signal name from decision.signals
        reason = self._extract_reason(decision)
        phase = decision.phase if isinstance(decision.phase, str) else decision.phase.value

        event = self._build_event(
            reason=reason,
            phase=phase,
            input_text=input_text,
            output_text=output_text,
        )

        return self._send_event(event)

    def track_raw_usage(
        self,
        input_text: str,
        output_text: str,
    ) -> TrackingResult:
        """
        Send a raw real-usage event (no SkillRouter decision involved).
        Useful for tracking prompt executions without skill activation context.

        Fail-closed: raises TrackingError on any failure.
        """
        event = self._build_event(
            reason="",
            phase="",
            input_text=input_text,
            output_text=output_text,
        )
        return self._send_event(event)

    @property
    def events_sent(self) -> int:
        """Number of events successfully sent."""
        return self._events_sent

    # ── Internal ─────────────────────────────────────────────────

    def _build_event(
        self,
        reason: str,
        phase: str,
        input_text: str,
        output_text: str,
    ) -> dict:
        """Build a SkillEvent payload matching the Go struct."""
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ")
        event: dict = {
            "event_type": "activation",
            "timestamp": now,
            "session_id": self._session_id,
            "mode": self._mode,
            "trigger": "real_usage_client",
            "estimated_waste": float(estimate_tokens(input_text)) if input_text else 0.0,
            "actions_suggested": 1,
            "actions_applied": 1,
            "impact_estimated_tokens": estimate_tokens(output_text) if output_text else 0,
        }
        # Only include SkillRouter metadata when present
        if reason:
            event["activation_reason"] = reason
        if phase:
            event["activation_phase"] = phase

        return event

    def _send_event(self, event: dict) -> TrackingResult:
        """POST the event to the tracking server. Fail-closed."""
        payload = json.dumps(event).encode("utf-8")

        try:
            req = urllib.request.Request(
                self._track_url,
                data=payload,
                headers={"Content-Type": "application/json"},
                method="POST",
            )
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                status = resp.getcode()
                if status < 200 or status >= 300:
                    msg = f"tracking server returned HTTP {status}"
                    logger.critical(msg)
                    raise TrackingError(msg)

                self._events_sent += 1
                logger.info(
                    "event sent: session=%s reason=%s phase=%s",
                    self._session_id,
                    event.get("activation_reason", ""),
                    event.get("activation_phase", ""),
                )
                return TrackingResult(
                    success=True,
                    status_code=status,
                    event_payload=event,
                )

        except TrackingError:
            raise  # re-raise our own errors
        except urllib.error.URLError as e:
            msg = f"tracking POST failed: {e}"
            logger.critical(msg)
            raise TrackingError(msg) from e
        except Exception as e:
            msg = f"tracking POST unexpected error: {e}"
            logger.critical(msg)
            raise TrackingError(msg) from e

    @staticmethod
    def _extract_reason(decision: ActivationDecision) -> str:
        """
        Extract the primary activation reason from the decision signals.
        Uses the first signal name, normalized to snake_case.
        """
        if not decision.signals:
            return "unknown"

        # Use the first signal (highest-weight or first detected)
        raw = decision.signals[0]

        # Normalize: lowercase, replace spaces/hyphens with underscores
        reason = raw.lower().replace(" ", "_").replace("-", "_")

        # Trim to a safe metric label length
        if len(reason) > 64:
            reason = reason[:64]

        return reason
