"""
orchestrator/skill_router.py — Skill activation router (fail-closed).

Decides whether the orbit-engine skill should activate for a given
conversation turn. Follows the EXACT same pattern as ModelRouter:
  - score-based signals
  - configurable threshold
  - deterministic decision
  - fail-closed (error → NOT activated)
  - mandatory logging

Default: skill does NOT activate. Activation is an exception that
requires signal evidence.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Optional


# ── Activation threshold ─────────────────────────────────────────────

# Sum of signal weights must reach this to activate the skill.
DEFAULT_ACTIVATION_THRESHOLD = 2


# ── Session phases ───────────────────────────────────────────────────

class Phase(str, Enum):
    """Session phase — inferred from turn count and content."""
    EXPLORATION = "exploration"   # turns 1-5: user setting context
    ANALYSIS = "analysis"        # turns 6-15: active problem-solving
    CONSOLIDATION = "consolidation"  # turns 16+: wrapping up / reviewing


# Phase boundaries (turn count).
_ANALYSIS_START = 6
_CONSOLIDATION_START = 16


# ── Activation signals ───────────────────────────────────────────────

# Signal 1: Presence of "DIAGNOSIS" block (weight=3 → automatic).
# If the text already contains a DIAGNOSIS block, the skill already fired
# or the user is requesting one explicitly.
DIAGNOSIS_PATTERN = r"(?m)^DIAGNOSIS\b"

# Signal 2: Deterministic language — zero-tolerance phrasing (weight=1).
DETERMINISTIC_PATTERNS = [
    r"\b0%\s*suposi[çc][aã]o\b",
    r"\bzero\s*suposi[çc][oõ]es?\b",
    r"\bsem\s+suposi[çc][oõ]es?\b",
    r"\bnenhuma?\s+suposi[çc][aã]o\b",
    r"\bdetermin[ií]stic[oa]\b",
    r"\bexatamente\s+como\b",
]

# Signal 3: Structured analysis — matrices, tables, comparisons (weight=1).
STRUCTURED_PATTERNS = [
    r"\bmatriz\b",
    r"\btabela\b.*\bcompar",
    r"\|\s*\w+\s*\|",               # markdown table row
    r"\b(prós?\s+e\s+contras?|vantagens?\s+e\s+desvantagens?)\b",
    r"\bgrade\s+de\s+avalia[çc][aã]o\b",
]

# Signal 4: Audit-precision terms (weight=1).
AUDIT_PATTERNS = [
    r"\bbyte[- ]?id[eê]ntic[oa]\b",
    r"\bdiff\s+exaustiv[oa]\b",
    r"\blinha[- ]?a[- ]?linha\b",
    r"\bchar[- ]?a[- ]?char\b",
    r"\baudit(?:oria|or)?\b",
    r"\bbit[- ]?a[- ]?bit\b",
    r"\bverifica[çc][aã]o\s+exaustiva\b",
]

# Signal 5: Explicit activation commands from SKILL.md (weight=3 → automatic).
EXPLICIT_ACTIVATION_PATTERNS = [
    r"\banalyze[- ]?cost\b",
    r"/analyze[- ]?cost\b",
    r"\bhow\s+efficient\s+is\s+this\b",
    r"\boptimize\s+this\b",
    r"\bis\s+this\s+optimal\b",
    r"\bapply\s+orbit[- ]?engine\b",
]

# Signal 6: Observable waste patterns from SKILL.md (weight=1 each, max 2).
WASTE_PATTERNS = [
    r"\b(correction\s+chain|corre[çc][oõ]es?\s+em\s+cadeia)\b",
    r"\b(repeated\s+edit|edi[çc][oõ]es?\s+repetidas?)\b",
    r"\b(exploratory\s+search|busca\s+explorat[oó]ria)\b",
    r"\b(validation\s+theater|teatro\s+de\s+valida[çc][aã]o)\b",
    r"\b(context\s+accumulation|acúmulo\s+de\s+contexto)\b",
]


# ── Suppression signals (anti-activation) ────────────────────────────

# If the text matches these, skill should NOT activate (from SKILL.md).
SUPPRESSION_PATTERNS = [
    r"^(thanks|obrigado|looks\s+good|parece\s+bom|ok|valeu)\s*[.!]?\s*$",
]


# ── Data structures ──────────────────────────────────────────────────

@dataclass
class ActivationRequest:
    """Input to the skill router — mirrors RoutingRequest from ModelRouter."""

    text: str                          # conversation turn text
    session_id: str
    turn_number: int = 1               # 1-indexed
    turn_count: int = 1                # total turns so far
    force_activate: bool = False       # explicit override


@dataclass
class ActivationDecision:
    """Output of the skill router — mirrors RoutingDecision from ModelRouter."""

    activated: bool
    score: int = 0
    threshold: int = DEFAULT_ACTIVATION_THRESHOLD
    signals: list[str] = field(default_factory=list)
    phase: Phase = Phase.EXPLORATION
    suppressed: bool = False
    suppression_reason: Optional[str] = None
    timestamp: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )

    def to_dict(self) -> dict:
        return {
            "activated": self.activated,
            "score": self.score,
            "threshold": self.threshold,
            "signals": self.signals,
            "phase": self.phase.value,
            "suppressed": self.suppressed,
            "suppression_reason": self.suppression_reason,
            "timestamp": self.timestamp,
        }


# ── Skill Router ─────────────────────────────────────────────────────

class SkillRouter:
    """
    Fail-closed skill activation router.

    Pattern: identical to ModelRouter.
      - Default: NOT activated (cheapest option).
      - Activation requires signal evidence meeting threshold.
      - Error during analysis → NOT activated (fail-closed).
      - Every decision logged — no exceptions.

    Usage:
        router = SkillRouter(threshold=2)
        decision = router.evaluate(request)
        if decision.activated:
            # run orbit-engine skill
        # decision is logged regardless
    """

    def __init__(self, threshold: int = DEFAULT_ACTIVATION_THRESHOLD) -> None:
        self._threshold = threshold
        self._decisions_log: list[dict] = []
        self._activation_count: int = 0

    # ── Public API ───────────────────────────────────────────────

    def evaluate(self, req: ActivationRequest) -> ActivationDecision:
        """Evaluate whether the skill should activate for this turn.

        Fail-closed: any exception → NOT activated + log.
        """
        try:
            return self._evaluate_internal(req)
        except Exception as exc:
            # Fail-closed: do NOT activate on error.
            decision = ActivationDecision(
                activated=False,
                score=0,
                threshold=self._threshold,
                signals=[f"fail-closed: analysis error — {exc}"],
                phase=self._infer_phase(req.turn_count),
            )
            self._log_decision(req, decision)
            return decision

    @property
    def decisions_log(self) -> list[dict]:
        """Read-only access to the decision log."""
        return list(self._decisions_log)

    @property
    def activation_count(self) -> int:
        """Total activations since creation."""
        return self._activation_count

    @property
    def threshold(self) -> int:
        return self._threshold

    # ── Internal logic ───────────────────────────────────────────

    def _evaluate_internal(self, req: ActivationRequest) -> ActivationDecision:
        phase = self._infer_phase(req.turn_count)

        # Gate 0: empty text → NOT activated.
        if not req.text or not req.text.strip():
            decision = ActivationDecision(
                activated=False,
                threshold=self._threshold,
                phase=phase,
                suppressed=True,
                suppression_reason="empty text",
            )
            self._log_decision(req, decision)
            return decision

        # Gate 1: suppression — trivial messages.
        text_stripped = req.text.strip()
        if self._matches_any(text_stripped, SUPPRESSION_PATTERNS):
            decision = ActivationDecision(
                activated=False,
                threshold=self._threshold,
                phase=phase,
                suppressed=True,
                suppression_reason="trivial_message",
            )
            self._log_decision(req, decision)
            return decision

        # Calculate activation score.
        score = 0
        signals: list[str] = []

        # Signal 1: DIAGNOSIS block present (weight=3 → automatic).
        if re.search(DIAGNOSIS_PATTERN, req.text):
            score += 3
            signals.append("diagnosis_block_present")

        # Signal 2: deterministic language (weight=1).
        if self._matches_any(req.text, DETERMINISTIC_PATTERNS):
            score += 1
            signals.append("deterministic_language")

        # Signal 3: structured analysis (weight=1).
        if self._matches_any(req.text, STRUCTURED_PATTERNS):
            score += 1
            signals.append("structured_analysis")

        # Signal 4: audit-precision terms (weight=1).
        if self._matches_any(req.text, AUDIT_PATTERNS):
            score += 1
            signals.append("audit_precision_terms")

        # Signal 5: explicit activation command (weight=3 → automatic).
        if self._matches_any(req.text, EXPLICIT_ACTIVATION_PATTERNS):
            score += 3
            signals.append("explicit_activation_command")

        # Signal 6: waste pattern references (weight=1 each, cap at 2).
        waste_count = sum(
            1 for p in WASTE_PATTERNS
            if re.search(p, req.text, re.IGNORECASE)
        )
        if waste_count > 0:
            capped = min(waste_count, 2)
            score += capped
            signals.append(f"waste_patterns_detected ({waste_count} found, {capped} counted)")

        # Signal 7: forced activation override (weight=3 → automatic).
        if req.force_activate:
            score += 3
            signals.append("force_activate_override")

        # Decide.
        activated = score >= self._threshold

        if activated:
            self._activation_count += 1

        decision = ActivationDecision(
            activated=activated,
            score=score,
            threshold=self._threshold,
            signals=signals,
            phase=phase,
        )
        self._log_decision(req, decision)
        return decision

    # ── Helpers ──────────────────────────────────────────────────

    @staticmethod
    def _infer_phase(turn_count: int) -> Phase:
        """Infer session phase from turn count."""
        if turn_count >= _CONSOLIDATION_START:
            return Phase.CONSOLIDATION
        if turn_count >= _ANALYSIS_START:
            return Phase.ANALYSIS
        return Phase.EXPLORATION

    @staticmethod
    def _matches_any(text: str, patterns: list[str]) -> bool:
        """Check if text matches any pattern (case-insensitive)."""
        return any(re.search(p, text, re.IGNORECASE) for p in patterns)

    def _log_decision(
        self, req: ActivationRequest, decision: ActivationDecision
    ) -> None:
        """Log every decision — no exceptions. Mirrors ModelRouter._log_decision."""
        entry = {
            "timestamp": decision.timestamp,
            "session_id": req.session_id,
            "turn_number": req.turn_number,
            "turn_count": req.turn_count,
            **decision.to_dict(),
            # Metric labels (for future Prometheus integration):
            # orbit_skill_activation_total{reason=<first_signal>, phase=<phase>}
            "metric_labels": {
                "reason": decision.signals[0] if decision.signals else "none",
                "phase": decision.phase.value,
            },
        }
        self._decisions_log.append(entry)
