"""
orchestrator/budget.py — Fail-closed budget gate.

Controls how much money can be spent on model calls.
If the gate cannot verify budget, the call is BLOCKED.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone


@dataclass
class CostEstimate:
    """Immutable cost estimate for a single call."""

    model: str
    input_tokens: int
    output_tokens: int
    input_cost_usd: float
    output_cost_usd: float
    total_usd: float

    def __post_init__(self) -> None:
        # Sanity: costs can never be negative.
        if self.total_usd < 0:
            raise ValueError(f"Cost cannot be negative: {self.total_usd}")


class BudgetGate:
    """
    Fail-closed budget controller.

    Usage:
        gate = BudgetGate(daily_limit_usd=5.0)
        if gate.can_spend(0.03):
            # proceed
            gate.spend(0.03)
        else:
            # BLOCKED

    Invariants:
        - remaining is never negative
        - can_spend(x) returns False if x > remaining
        - spend(x) raises if x > remaining (fail-closed)
        - reset() restores the full daily budget
    """

    def __init__(self, daily_limit_usd: float) -> None:
        if daily_limit_usd <= 0:
            raise ValueError(f"Budget must be positive: {daily_limit_usd}")
        self._daily_limit = daily_limit_usd
        self._spent: float = 0.0
        self._transactions: list[dict] = []
        self._created_at = datetime.now(timezone.utc).isoformat()

    # ── Properties ───────────────────────────────────────────────

    @property
    def daily_limit(self) -> float:
        return self._daily_limit

    @property
    def spent(self) -> float:
        return round(self._spent, 8)

    @property
    def remaining(self) -> float:
        return round(max(0.0, self._daily_limit - self._spent), 8)

    @property
    def utilization_pct(self) -> float:
        """Budget utilization as percentage (0–100)."""
        if self._daily_limit == 0:
            return 100.0
        return round((self._spent / self._daily_limit) * 100, 2)

    @property
    def transactions(self) -> list[dict]:
        return list(self._transactions)

    # ── Public API ───────────────────────────────────────────────

    def can_spend(self, amount_usd: float) -> bool:
        """Check if the budget allows this spend. Pure read."""
        if amount_usd < 0:
            return False
        return amount_usd <= self.remaining

    def spend(self, amount_usd: float) -> None:
        """Record a spend. Raises ValueError if budget exceeded (fail-closed)."""
        if amount_usd < 0:
            raise ValueError(f"Spend amount cannot be negative: {amount_usd}")
        if amount_usd > self.remaining:
            raise ValueError(
                f"Budget exceeded: need ${amount_usd:.6f}, "
                f"have ${self.remaining:.6f}"
            )
        self._spent += amount_usd
        self._transactions.append({
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "amount_usd": round(amount_usd, 8),
            "remaining_usd": self.remaining,
        })

    def reserve(self, amount_usd: float) -> BudgetReservation:
        """Reserve budget for a pending call. Returns a reservation that can
        be committed or released.

        Fail-closed: if reservation can't be created, raises ValueError.
        """
        if not self.can_spend(amount_usd):
            raise ValueError(
                f"Cannot reserve: need ${amount_usd:.6f}, "
                f"have ${self.remaining:.6f}"
            )
        self._spent += amount_usd
        return BudgetReservation(gate=self, amount=amount_usd)

    def _release(self, amount_usd: float) -> None:
        """Release a reservation (called by BudgetReservation)."""
        self._spent = max(0.0, self._spent - amount_usd)

    def reset(self) -> None:
        """Reset budget for a new day."""
        self._spent = 0.0
        self._transactions.clear()

    def to_dict(self) -> dict:
        return {
            "daily_limit_usd": self._daily_limit,
            "spent_usd": self.spent,
            "remaining_usd": self.remaining,
            "utilization_pct": self.utilization_pct,
            "transaction_count": len(self._transactions),
            "created_at": self._created_at,
        }


class BudgetReservation:
    """
    A budget reservation that can be committed or released.

    This prevents race conditions in async scenarios: the budget is
    "held" during model execution and then finalized.
    """

    def __init__(self, gate: BudgetGate, amount: float) -> None:
        self._gate = gate
        self._amount = amount
        self._committed = False
        self._released = False

    @property
    def amount(self) -> float:
        return self._amount

    def commit(self, actual_amount: float | None = None) -> None:
        """Finalize the reservation. Optionally adjust to actual cost."""
        if self._committed or self._released:
            raise RuntimeError("Reservation already finalized")
        self._committed = True

        if actual_amount is not None and actual_amount != self._amount:
            # Release the difference or charge extra.
            diff = self._amount - actual_amount
            if diff > 0:
                self._gate._release(diff)
            elif diff < 0:
                self._gate.spend(abs(diff))

    def release(self) -> None:
        """Cancel the reservation, returning budget."""
        if self._committed or self._released:
            raise RuntimeError("Reservation already finalized")
        self._released = True
        self._gate._release(self._amount)

    def __del__(self) -> None:
        """Safety net: if reservation was never finalized, release it."""
        if not self._committed and not self._released:
            try:
                self._gate._release(self._amount)
            except Exception:
                pass  # Best-effort during GC.
