"""
orchestrator — Model + Skill routing engine for Orbit.

ModelRouter: Sonnet is the default. Opus is the exception.
SkillRouter: NOT activated is the default. Activation is the exception.
SkillTrackingClient: SkillRouter → Tracking Server (fail-closed).
"""

from orchestrator.budget import BudgetGate, CostEstimate
from orchestrator.client import SkillTrackingClient, TrackingError, TrackingResult
from orchestrator.router import Model, ModelRouter, RoutingDecision, RoutingRequest
from orchestrator.skill_router import (
    ActivationDecision,
    ActivationRequest,
    Phase,
    SkillRouter,
)

__all__ = [
    "ActivationDecision",
    "ActivationRequest",
    "BudgetGate",
    "CostEstimate",
    "Model",
    "ModelRouter",
    "Phase",
    "RoutingDecision",
    "RoutingRequest",
    "SkillRouter",
    "SkillTrackingClient",
    "TrackingError",
    "TrackingResult",
]
