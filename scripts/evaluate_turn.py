#!/usr/bin/env python3
"""evaluate_turn.py — run SkillRouter.evaluate() over a JSONL fixture file.

Each input line is a JSON object with at least a "text" field. Output is
one JSON decision per line (PII-safe: raw text is never echoed back).

Usage:
    scripts/evaluate_turn.py scripts/fixtures/activation_turns.jsonl

Exit codes:
    0  at least one fixture was evaluated
    2  fixture file missing or unreadable
    3  SkillRouter import failed (fail-closed)
"""
from __future__ import annotations

import json
import os
import sys
from typing import Any

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if REPO_ROOT not in sys.path:
    sys.path.insert(0, REPO_ROOT)

try:
    from orchestrator.skill_router import ActivationRequest, SkillRouter
except Exception as exc:
    print(f"evaluate_turn: cannot import SkillRouter: {exc}", file=sys.stderr)
    sys.exit(3)


def _build_request(raw: dict[str, Any]) -> ActivationRequest:
    return ActivationRequest(
        text=raw.get("text", ""),
        session_id=str(raw.get("session_id", "sim-fx-unknown")),
        turn_number=int(raw.get("turn_number", 1)),
        turn_count=int(raw.get("turn_count", 1)),
        force_activate=bool(raw.get("force_activate", False)),
    )


def _decision_to_line(req: ActivationRequest, decision) -> dict[str, Any]:
    return {
        "session_id": req.session_id,
        "turn_number": req.turn_number,
        "turn_count": req.turn_count,
        "text_len": len(req.text),
        "activated": decision.activated,
        "score": decision.score,
        "threshold": decision.threshold,
        "signals": decision.signals,
        "phase": decision.phase.value,
        "suppressed": decision.suppressed,
        "suppression_reason": decision.suppression_reason,
    }


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: evaluate_turn.py <fixtures.jsonl>", file=sys.stderr)
        return 2
    path = argv[1]
    if not os.path.isfile(path):
        print(f"evaluate_turn: fixture file not found: {path}", file=sys.stderr)
        return 2

    router = SkillRouter()
    processed = 0

    with open(path, "r", encoding="utf-8") as f:
        for line_no, raw_line in enumerate(f, 1):
            line = raw_line.strip()
            if not line or line.startswith("#"):
                continue
            try:
                fixture = json.loads(line)
            except json.JSONDecodeError as exc:
                print(
                    f"evaluate_turn: line {line_no}: invalid JSON ({exc})",
                    file=sys.stderr,
                )
                continue
            req = _build_request(fixture)
            decision = router.evaluate(req)
            print(json.dumps(_decision_to_line(req, decision), ensure_ascii=False))
            processed += 1

    if processed == 0:
        print("evaluate_turn: no fixtures processed", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
