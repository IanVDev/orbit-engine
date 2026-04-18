"""
orchestrator — Skill domain root for Orbit.

Núcleo do CLI Orbit (orbit run / doctor / snapshot / guidance) é Go puro
e não depende deste pacote. O Python concentra apenas o domínio da
skill orbit-engine: model routing (Sonnet default / Opus exceção),
budget gating fail-closed, e a fachada do compact_guard.

Submódulos:
  orchestrator.skill        — ModelRouter, BudgetGate, ModelControl
  orchestrator.skill_router — SkillRouter (ativação determinística)
  orchestrator.client       — SkillTrackingClient (skill → tracking-server)
  orchestrator.compact_guard — façade do script bash (LLM compact gate)

Re-exports neste namespace mantêm compatibilidade com importações
existentes (`from orchestrator import BudgetGate, ModelRouter, ...`).
"""

from orchestrator.client import SkillTrackingClient, TrackingError, TrackingResult
from orchestrator.skill import (
    BudgetGate,
    CostEstimate,
    Model,
    ModelRouter,
    RoutingDecision,
    RoutingRequest,
)
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
