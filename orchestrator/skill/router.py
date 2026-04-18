"""
orchestrator/router.py — Model routing engine (fail-closed).

Decides which model to use for a given task. Sonnet is the default.
Opus is an exception that requires justification.

Fail-closed: if anything goes wrong during analysis, the router
chooses Sonnet (cheapest) and logs the failure.
"""

from __future__ import annotations

import json
import re
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Optional

from orchestrator.skill.budget import BudgetGate, CostEstimate


# ── Models ───────────────────────────────────────────────────────────

class Model(str, Enum):
    SONNET = "sonnet"
    OPUS = "opus"


# ── Model Control ────────────────────────────────────────────────────

class ModelControl(str, Enum):
    """Explicit permission policy for model overrides.

    Design: fail-closed. Unknown values raise ValueError at construction time
    so the process refuses to start rather than silently allowing overrides.

    locked  — never change the model (default / most restrictive).
    auto    — apply existing heuristics; overrides are permitted.
    suggest — compute what would change, return suggestion, do NOT override.
    """
    LOCKED  = "locked"
    AUTO    = "auto"
    SUGGEST = "suggest"

    @classmethod
    def parse(cls, value: str) -> "ModelControl":
        """Fail-closed parser. Raises ValueError for unknown strings."""
        try:
            return cls(value.lower())
        except ValueError:
            valid = ", ".join(m.value for m in cls)
            raise ValueError(
                f"model_control: unknown value {value!r} — valid values are {valid}"
            )


# Pricing per 1M tokens (USD). Update when Anthropic changes prices.
MODEL_PRICING = {
    Model.SONNET: {"input": 3.00, "output": 15.00},
    Model.OPUS:   {"input": 15.00, "output": 75.00},
}

# Max output tokens heuristic: min(input * ratio, cap)
OUTPUT_RATIO = 0.8
MAX_OUTPUT_TOKENS = 8192

# Escalation threshold: sum of weights must reach this to choose Opus.
ESCALATION_THRESHOLD = 2

# Rate limit: max Opus calls per hour before automatic downgrade.
MAX_OPUS_PER_HOUR = 10

# Max context tokens before blocking.
MAX_CONTEXT_TOKENS = 200_000


# ── Escalation signals ───────────────────────────────────────────────

# Keywords that suggest multi-step reasoning (criterion 1).
COMPLEXITY_PATTERNS = [
    r"\b(analis[ae]|compar[ae]|avali[ae]|arquitet[ae]|design|refactor|migra)\b",
    r"\b(trade-?off|prós?\s+e\s+contras?|vantagens?\s+e\s+desvantagens?)\b",
    r"\b(estrat[eé]gi[ac]|sistema|pipeline|orquestr)\b",
]

# Keywords that suggest critical tasks (criterion 5).
CRITICAL_PATTERNS = [
    r"\b(produ[çc][aã]o|deploy|release|security|segurança)\b",
    r"\b(contrato|breaking.?change|migration)\b",
    r"\b(fail-?closed|invariant[e]?)\b",
]


# ── Data structures ──────────────────────────────────────────────────

@dataclass
class RoutingRequest:
    """Input to the router."""

    prompt: str
    session_id: str
    task_id: str = ""
    force_opus: bool = False           # explicit override [opus]
    context_tokens: int = 0            # pre-counted if available
    previous_sonnet_failure: bool = False  # this task already failed on Sonnet
    model_control: ModelControl = ModelControl.LOCKED  # override policy (default: locked)


@dataclass
class RoutingDecision:
    """Output of the router — immutable once created."""

    model: Model
    blocked: bool = False
    block_reason: Optional[str] = None
    escalation_score: int = 0
    escalation_reasons: list[str] = field(default_factory=list)
    estimated_cost: Optional[CostEstimate] = None
    # model_control fields — populated when an override was evaluated.
    model_control: ModelControl = ModelControl.LOCKED
    model_override_requested: Optional[str] = None   # model that would have been chosen
    model_override_applied: bool = False              # True only when control=auto + override
    model_override_reason: Optional[str] = None      # why the override was/wasn't applied
    model_override_confidence: float = 0.0           # 0.0–1.0
    timestamp: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )

    def to_dict(self) -> dict:
        d = {
            "model": self.model.value,
            "blocked": self.blocked,
            "block_reason": self.block_reason,
            "escalation_score": self.escalation_score,
            "escalation_reasons": self.escalation_reasons,
            "estimated_cost_usd": (
                self.estimated_cost.total_usd if self.estimated_cost else None
            ),
            "timestamp": self.timestamp,
        }
        # Only include override fields when relevant (reduces log noise).
        if self.model_override_requested is not None:
            d.update({
                "model_control": self.model_control.value,
                "model_override_requested": self.model_override_requested,
                "model_override_applied": self.model_override_applied,
                "model_override_reason": self.model_override_reason,
                "model_override_confidence": self.model_override_confidence,
            })
        return d


@dataclass
class ExecutionResult:
    """Result after calling the model — captures actual usage."""

    decision: RoutingDecision
    actual_input_tokens: int = 0
    actual_output_tokens: int = 0
    actual_cost_usd: float = 0.0
    duration_ms: int = 0
    response: str = ""
    error: Optional[str] = None

    @property
    def cost_drift_pct(self) -> float:
        """How far actual cost deviated from estimate (positive = over)."""
        est = self.decision.estimated_cost
        if not est or est.total_usd == 0:
            return 0.0
        return ((self.actual_cost_usd - est.total_usd) / est.total_usd) * 100


# ── Router ───────────────────────────────────────────────────────────

class ModelRouter:
    """
    Fail-closed model router.

    Usage:
        router = ModelRouter(budget=BudgetGate(daily_limit_usd=5.0))
        decision = router.route(request)
        if decision.blocked:
            print(decision.block_reason)
        else:
            # call decision.model
            result = router.record_execution(decision, actual_in, actual_out, ms)
    """

    def __init__(self, budget: BudgetGate) -> None:
        self._budget = budget
        self._opus_calls_this_hour: list[float] = []  # timestamps
        self._decisions_log: list[dict] = []

    # ── Public API ───────────────────────────────────────────────

    def route(self, req: RoutingRequest) -> RoutingDecision:
        """Analyze the request and decide which model to use.

        Fail-closed: any exception during analysis → Sonnet + log.
        """
        try:
            return self._route_internal(req)
        except Exception as exc:
            # Fail-closed: default to cheapest model.
            decision = RoutingDecision(
                model=Model.SONNET,
                escalation_score=0,
                escalation_reasons=[f"fail-closed: analysis error — {exc}"],
            )
            self._log_decision(req, decision)
            return decision

    def record_execution(
        self,
        decision: RoutingDecision,
        actual_input_tokens: int,
        actual_output_tokens: int,
        duration_ms: int,
        response: str = "",
        error: Optional[str] = None,
    ) -> ExecutionResult:
        """Record actual usage after model call. Updates budget."""
        pricing = MODEL_PRICING[decision.model]
        actual_cost = (
            (actual_input_tokens / 1_000_000 * pricing["input"])
            + (actual_output_tokens / 1_000_000 * pricing["output"])
        )

        self._budget.spend(actual_cost)

        result = ExecutionResult(
            decision=decision,
            actual_input_tokens=actual_input_tokens,
            actual_output_tokens=actual_output_tokens,
            actual_cost_usd=actual_cost,
            duration_ms=duration_ms,
            response=response,
            error=error,
        )

        # Update the log entry with actual results.
        if self._decisions_log:
            last = self._decisions_log[-1]
            last["actual_input_tokens"] = actual_input_tokens
            last["actual_output_tokens"] = actual_output_tokens
            last["actual_cost_usd"] = round(actual_cost, 6)
            last["cost_drift_pct"] = round(result.cost_drift_pct, 1)
            last["duration_ms"] = duration_ms
            if error:
                last["error"] = error

        return result

    @property
    def decisions_log(self) -> list[dict]:
        """Read-only access to the decision log."""
        return list(self._decisions_log)

    @property
    def budget(self) -> BudgetGate:
        return self._budget

    # ── Internal logic ───────────────────────────────────────────

    def _route_internal(self, req: RoutingRequest) -> RoutingDecision:
        # Gate 0: empty prompt → BLOCK.
        if not req.prompt or not req.prompt.strip():
            decision = RoutingDecision(
                model=Model.SONNET,
                blocked=True,
                block_reason="empty prompt (fail-closed)",
                model_control=req.model_control,
            )
            self._log_decision(req, decision)
            return decision

        # Estimate tokens.
        input_tokens = req.context_tokens or self._estimate_tokens(req.prompt)
        output_tokens = min(int(input_tokens * OUTPUT_RATIO), MAX_OUTPUT_TOKENS)

        # Gate 1: context too large → BLOCK.
        if input_tokens > MAX_CONTEXT_TOKENS:
            decision = RoutingDecision(
                model=Model.SONNET,
                blocked=True,
                block_reason=(
                    f"context exceeds limit ({input_tokens:,} > {MAX_CONTEXT_TOKENS:,})"
                ),
                model_control=req.model_control,
            )
            self._log_decision(req, decision)
            return decision

        # Calculate escalation score.
        score = 0
        reasons: list[str] = []

        # Criterion 1: multi-step reasoning keywords.
        if self._matches_patterns(req.prompt, COMPLEXITY_PATTERNS):
            score += 1
            reasons.append("complexity_keywords")

        # Criterion 2: large context.
        if input_tokens > 8_000:
            score += 1
            reasons.append(f"large_context ({input_tokens:,} tokens)")

        # Criterion 3: previous Sonnet failure (weight=2).
        if req.previous_sonnet_failure:
            score += 2
            reasons.append("previous_sonnet_failure")

        # Criterion 4: explicit override (weight=3 → automatic).
        if req.force_opus:
            score += 3
            reasons.append("explicit_override")

        # Criterion 5: critical task.
        if self._matches_patterns(req.prompt, CRITICAL_PATTERNS):
            score += 1
            reasons.append("critical_task_keywords")

        # Choose model based on score (before model_control enforcement).
        heuristic_choice = Model.OPUS if score >= ESCALATION_THRESHOLD else Model.SONNET

        # ── Model Control enforcement ────────────────────────────────────
        chosen, override_decision = self._apply_model_control(
            req, heuristic_choice, score, reasons
        )
        # If locked and override was attempted, log and return blocked.
        # suggest mode does NOT block — it suppresses the override silently.
        is_locked_block = (
            not override_decision["allowed"]
            and override_decision["override_attempted"]
            and req.model_control == ModelControl.LOCKED
        )
        if is_locked_block:
            decision = RoutingDecision(
                model=Model.SONNET,
                blocked=True,
                block_reason=f"model_override_blocked: {override_decision['reason']}",
                escalation_score=score,
                escalation_reasons=reasons,
                model_control=req.model_control,
                model_override_requested=heuristic_choice.value,
                model_override_applied=False,
                model_override_reason=override_decision["reason"],
                model_override_confidence=override_decision["confidence"],
            )
            self._log_decision(req, decision)
            self._emit_override_log(req, decision, heuristic_choice)
            return decision

        # Estimate cost for chosen model.
        estimate = self._estimate_cost(chosen, input_tokens, output_tokens)

        # Gate 2: budget check.
        if not self._budget.can_spend(estimate.total_usd):
            if chosen == Model.OPUS:
                # Try downgrade to Sonnet.
                sonnet_estimate = self._estimate_cost(
                    Model.SONNET, input_tokens, output_tokens
                )
                if self._budget.can_spend(sonnet_estimate.total_usd):
                    chosen = Model.SONNET
                    estimate = sonnet_estimate
                    reasons.append("downgraded_budget_insufficient_for_opus")
                else:
                    decision = RoutingDecision(
                        model=Model.SONNET,
                        blocked=True,
                        block_reason=(
                            f"budget exceeded (need ${estimate.total_usd:.4f}, "
                            f"have ${self._budget.remaining:.4f})"
                        ),
                        escalation_score=score,
                        escalation_reasons=reasons,
                        estimated_cost=estimate,
                        model_control=req.model_control,
                    )
                    self._log_decision(req, decision)
                    return decision
            else:
                decision = RoutingDecision(
                    model=Model.SONNET,
                    blocked=True,
                    block_reason=(
                        f"budget exceeded (need ${estimate.total_usd:.4f}, "
                        f"have ${self._budget.remaining:.4f})"
                    ),
                    escalation_score=score,
                    escalation_reasons=reasons,
                    estimated_cost=estimate,
                    model_control=req.model_control,
                )
                self._log_decision(req, decision)
                return decision

        # Gate 3: Opus rate limit.
        if chosen == Model.OPUS:
            self._prune_old_calls()
            if len(self._opus_calls_this_hour) >= MAX_OPUS_PER_HOUR:
                chosen = Model.SONNET
                estimate = self._estimate_cost(
                    Model.SONNET, input_tokens, output_tokens
                )
                reasons.append("rate_limited_opus_downgrade")

        # Track Opus call for rate limiting.
        if chosen == Model.OPUS:
            self._opus_calls_this_hour.append(time.time())

        decision = RoutingDecision(
            model=chosen,
            escalation_score=score,
            escalation_reasons=reasons,
            estimated_cost=estimate,
            model_control=req.model_control,
            model_override_requested=heuristic_choice.value if heuristic_choice != chosen else None,
            model_override_applied=override_decision["applied"],
            model_override_reason=override_decision["reason"] if override_decision["override_attempted"] else None,
            model_override_confidence=override_decision["confidence"],
        )
        self._log_decision(req, decision)
        if override_decision["override_attempted"]:
            self._emit_override_log(req, decision, heuristic_choice)
        return decision

    def _apply_model_control(
        self,
        req: RoutingRequest,
        heuristic_choice: Model,
        score: int,
        reasons: list[str],
    ) -> tuple[Model, dict]:
        """
        Enforce the model_control policy.

        Returns (chosen_model, override_info_dict).

        override_info_dict keys:
          override_attempted: bool  — True when heuristic would change model
          allowed:            bool  — True when control permits the change
          applied:            bool  — True when override actually took effect
          reason:             str   — human-readable explanation
          confidence:         float — 0.0–1.0 routing certainty
        """
        # Baseline model is always Sonnet.
        would_change = heuristic_choice != Model.SONNET

        if not would_change:
            # No override needed — pass-through regardless of control.
            return heuristic_choice, {
                "override_attempted": False,
                "allowed": True,
                "applied": False,
                "reason": "no_override_required",
                "confidence": 1.0,
            }

        ctrl = req.model_control

        if ctrl == ModelControl.LOCKED:
            # Fail-closed: never change the model.
            return Model.SONNET, {
                "override_attempted": True,
                "allowed": False,
                "applied": False,
                "reason": "blocked_by_locked_control",
                "confidence": 1.0,
            }

        if ctrl == ModelControl.AUTO:
            # Heuristics apply; override is permitted.
            return heuristic_choice, {
                "override_attempted": True,
                "allowed": True,
                "applied": True,
                "reason": "override_permitted_by_auto_control",
                "confidence": 1.0,
            }

        if ctrl == ModelControl.SUGGEST:
            # Compute what would change but do not apply — return Sonnet.
            return Model.SONNET, {
                "override_attempted": True,
                "allowed": False,
                "applied": False,
                "reason": "override_suppressed_suggest_only",
                "confidence": 0.8,
            }

        # Unknown control value — should never reach here after parse validation,
        # but guard defensively (fail-closed).
        raise ValueError(f"model_control: unknown control {ctrl!r} in _apply_model_control")

    # ── Helpers ──────────────────────────────────────────────────

    @staticmethod
    def _estimate_tokens(text: str) -> int:
        """Rough token estimate: ~4 chars per token for English/Portuguese."""
        return max(1, len(text) // 4)

    @staticmethod
    def _estimate_cost(
        model: Model, input_tokens: int, output_tokens: int
    ) -> CostEstimate:
        pricing = MODEL_PRICING[model]
        input_cost = input_tokens / 1_000_000 * pricing["input"]
        output_cost = output_tokens / 1_000_000 * pricing["output"]
        return CostEstimate(
            model=model.value,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            input_cost_usd=input_cost,
            output_cost_usd=output_cost,
            total_usd=input_cost + output_cost,
        )

    @staticmethod
    def _matches_patterns(text: str, patterns: list[str]) -> bool:
        lower = text.lower()
        return any(re.search(p, lower) for p in patterns)

    def _prune_old_calls(self) -> None:
        cutoff = time.time() - 3600  # 1 hour
        self._opus_calls_this_hour = [
            t for t in self._opus_calls_this_hour if t > cutoff
        ]

    def _log_decision(self, req: RoutingRequest, decision: RoutingDecision) -> None:
        entry = {
            "timestamp": decision.timestamp,
            "session_id": req.session_id,
            "task_id": req.task_id,
            "prompt_tokens_estimated": (
                req.context_tokens or self._estimate_tokens(req.prompt)
            ),
            "model_control": req.model_control.value,
            **decision.to_dict(),
        }
        self._decisions_log.append(entry)

    def _emit_override_log(
        self, req: RoutingRequest, decision: RoutingDecision, heuristic_choice: Model
    ) -> None:
        """Emit a structured JSONL log line for every model override evaluation."""
        entry = {
            "timestamp": decision.timestamp,
            "event": "model_override",
            "session_id": req.session_id,
            "task_id": req.task_id,
            "control": req.model_control.value,
            "from": Model.SONNET.value,        # baseline (before any change)
            "to": heuristic_choice.value,       # what heuristics requested
            "chosen": decision.model.value,     # what was actually used
            "reason": decision.model_override_reason or "no_override",
            "confidence": decision.model_override_confidence,
            "override_applied": decision.model_override_applied,
            "override_allowed": decision.model_override_applied or (
                decision.model_control == ModelControl.AUTO
            ),
        }
        # Structured JSONL — same format as other orbit-engine log events.
        import sys
        print(json.dumps(entry), file=sys.stderr, flush=True)
