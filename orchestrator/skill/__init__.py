"""
orchestrator.skill — domínio isolado da Orbit Skill (model + budget routing).

Este pacote concentra a lógica de roteamento de modelo e controle de
budget que pertence à skill `orbit-engine`, NÃO ao CLI core (`orbit run`).
A separação é proposital: o CLI Go é o produto principal e não depende
deste pacote. A skill consome este código via `from orchestrator.skill
import ModelRouter, BudgetGate`.

Re-exports preservam o contrato público anterior (`from orchestrator
import BudgetGate` continua válido via orchestrator/__init__.py).
"""

from orchestrator.skill.budget import BudgetGate, BudgetReservation, CostEstimate
from orchestrator.skill.router import (
    ExecutionResult,
    Model,
    ModelControl,
    ModelRouter,
    RoutingDecision,
    RoutingRequest,
)

__all__ = [
    "BudgetGate",
    "BudgetReservation",
    "CostEstimate",
    "ExecutionResult",
    "Model",
    "ModelControl",
    "ModelRouter",
    "RoutingDecision",
    "RoutingRequest",
]
