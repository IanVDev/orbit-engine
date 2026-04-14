#!/usr/bin/env python3
"""
orbit-engine decision engine.

Pure-function gate logic that decides whether a skill change should be
accepted, rejected, or held for review.

Three gates:
    Gate 1 — Validation: HARD pass + SOFT score vs. baseline
    Gate 2 — Feedback: adoption metrics from real sessions
    Gate 3 — Safety: structural invariants of the skill file

Verdicts:
    ACCEPT — all gates passed, changes are safe
    REJECT — hard failure, restore backup immediately
    HOLD   — marginal or insufficient data, flag for manual review

Dependencies: Python stdlib only (json, re, hashlib, pathlib).
"""

from __future__ import annotations

import hashlib
import json
import re
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from pathlib import Path


# ---------------------------------------------------------------------------
# Verdict model
# ---------------------------------------------------------------------------

class Verdict(Enum):
    ACCEPT = "ACCEPT"
    REJECT = "REJECT"
    HOLD = "HOLD"


@dataclass
class GateResult:
    """Result from a single gate evaluation."""
    gate: str
    verdict: Verdict
    reasons: list[str] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return self.verdict == Verdict.ACCEPT

    @property
    def skipped(self) -> bool:
        return self.verdict == Verdict.HOLD and "skip" in " ".join(self.reasons).lower()


@dataclass
class DecisionResult:
    """Combined result from all gates."""
    verdict: Verdict
    gates: list[GateResult]
    reasons: list[str] = field(default_factory=list)

    def summary(self) -> str:
        lines = []
        for g in self.gates:
            icon = {"ACCEPT": "✅", "REJECT": "🔴", "HOLD": "⚠️"}[g.verdict.value]
            lines.append(f"  {g.gate}: {icon} {g.verdict.value}")
            for r in g.reasons:
                lines.append(f"    {r}")
        lines.append("")
        icon = {"ACCEPT": "✅", "REJECT": "🔴", "HOLD": "⚠️"}[self.verdict.value]
        lines.append(f"  Verdict: {icon} {self.verdict.value}")
        for r in self.reasons:
            lines.append(f"    {r}")
        return "\n".join(lines)


# ---------------------------------------------------------------------------
# Baseline
# ---------------------------------------------------------------------------

@dataclass
class Baseline:
    """Snapshot of the last accepted state."""
    timestamp: str
    tests_passed: int
    tests_total: int
    avg_score: float
    per_test_scores: dict[str, float]
    gaming_warnings: int
    skill_lines: int
    skill_hash: str

    @classmethod
    def from_file(cls, path: Path) -> "Baseline":
        data = json.loads(path.read_text(encoding="utf-8"))
        return cls(**data)

    def to_file(self, path: Path) -> None:
        data = {
            "timestamp": self.timestamp,
            "tests_passed": self.tests_passed,
            "tests_total": self.tests_total,
            "avg_score": self.avg_score,
            "per_test_scores": self.per_test_scores,
            "gaming_warnings": self.gaming_warnings,
            "skill_lines": self.skill_lines,
            "skill_hash": self.skill_hash,
        }
        path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


def compute_skill_hash(path: Path) -> str:
    """SHA-256 hash of the skill file content."""
    content = path.read_bytes()
    return f"sha256:{hashlib.sha256(content).hexdigest()[:16]}"


def count_lines(path: Path) -> int:
    return len(path.read_text(encoding="utf-8").splitlines())


# ---------------------------------------------------------------------------
# Validation results (from test runner)
# ---------------------------------------------------------------------------

@dataclass
class ValidationResults:
    """Results from running the test suite."""
    tests_passed: int
    tests_total: int
    hard_all_passed: bool
    avg_score: float
    per_test_scores: dict[str, float]
    gaming_warnings: int


# ---------------------------------------------------------------------------
# Feedback metrics (from feedback_report)
# ---------------------------------------------------------------------------

@dataclass
class FeedbackMetrics:
    """Aggregate metrics from feedback collection."""
    total_activations: int
    adoption_rate: float
    avg_time_to_action: float | None
    silence_rate: float
    pattern_recurrence_rate: float

    @classmethod
    def from_jsonl(cls, path: Path) -> "FeedbackMetrics | None":
        """Load and aggregate from JSONL file."""
        if not path.exists():
            return None

        entries = []
        with open(path, encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if line:
                    entries.append(json.loads(line))

        if not entries:
            return None

        total = len(entries)
        total_actions = sum(e.get("action_items", 0) for e in entries)
        total_adopted = sum(e.get("actions_adopted", 0) for e in entries)
        adoption = total_adopted / total_actions if total_actions > 0 else 0.0

        tta_vals = [
            e["time_to_action"] for e in entries
            if e.get("time_to_action") is not None
        ]
        avg_tta = sum(tta_vals) / len(tta_vals) if tta_vals else None

        silence = sum(1 for e in entries if e.get("silence")) / total
        recurrence = sum(1 for e in entries if e.get("pattern_recurrence")) / total

        return cls(
            total_activations=total,
            adoption_rate=adoption,
            avg_time_to_action=avg_tta,
            silence_rate=silence,
            pattern_recurrence_rate=recurrence,
        )


# ---------------------------------------------------------------------------
# Safety checks on skill file content
# ---------------------------------------------------------------------------

# Key structural elements that must be present in SKILL.md
_REQUIRED_PATTERNS = [
    (r"unsolicited long response", "Observable pattern: unsolicited long response"),
    (r"correction chain", "Observable pattern: correction chain"),
    (r"repeated edits", "Observable pattern: repeated edits"),
    (r"exploratory.*without.*plan", "Observable pattern: exploratory reading"),
    (r"weak prompt|missing constraints", "Observable pattern: weak prompt"),
    (r"large.*paste", "Observable pattern: large paste"),
]

_FORMAT_TEMPLATE_MARKERS = [
    r"^DIAGNOSIS$",
    r"^Risk:",
    r"^ACTIONS$",
    r"^DO NOT DO NOW$",
]

_SILENCE_RULE_RE = re.compile(
    r"silence|no\s*output|complete\s*silence|produce\s*no\s*output",
    re.IGNORECASE,
)

_GATING_RULES_RE = re.compile(
    r"do not recommend.*/clear|do not recommend.*/compact|gating rule",
    re.IGNORECASE,
)


def check_safety(skill_content: str, baseline: Baseline | None,
                 skill_lines: int) -> GateResult:
    """Gate 3 — Safety invariants."""
    result = GateResult(gate="Gate 3 (Safety)", verdict=Verdict.ACCEPT)

    # Check line growth
    if baseline:
        growth = (skill_lines - baseline.skill_lines) / baseline.skill_lines
        if growth > 0.20:
            result.verdict = Verdict.HOLD
            result.reasons.append(
                f"Lines: {baseline.skill_lines} → {skill_lines} "
                f"(+{growth:.0%}) — exceeds 20% growth limit"
            )
        else:
            result.reasons.append(
                f"Lines: {baseline.skill_lines} → {skill_lines} "
                f"(+{growth:.1%}) — within limit"
            )

    # Check observable patterns present
    content_lower = skill_content.lower()
    patterns_found = 0
    for pattern_re, label in _REQUIRED_PATTERNS:
        if re.search(pattern_re, content_lower):
            patterns_found += 1
        else:
            result.verdict = Verdict.REJECT
            result.reasons.append(f"Missing: {label}")

    result.reasons.append(
        f"Patterns: {patterns_found}/{len(_REQUIRED_PATTERNS)} present"
    )

    # Check format template markers
    for marker in _FORMAT_TEMPLATE_MARKERS:
        if not re.search(marker, skill_content, re.MULTILINE | re.IGNORECASE):
            result.verdict = Verdict.REJECT
            result.reasons.append(f"Format template missing: {marker}")

    if all(
        re.search(m, skill_content, re.MULTILINE | re.IGNORECASE)
        for m in _FORMAT_TEMPLATE_MARKERS
    ):
        result.reasons.append("Format template: unchanged")

    # Check silence rule
    if _SILENCE_RULE_RE.search(skill_content):
        result.reasons.append("Silence rule: present")
    else:
        result.verdict = Verdict.REJECT
        result.reasons.append("Silence rule: MISSING — would enable false positives")

    # Check gating rules
    if _GATING_RULES_RE.search(skill_content):
        result.reasons.append("Gating rules: present")
    else:
        result.verdict = Verdict.REJECT
        result.reasons.append("Gating rules: MISSING — safety regression")

    return result


# ---------------------------------------------------------------------------
# Gate evaluators
# ---------------------------------------------------------------------------

def check_validation(results: ValidationResults,
                     baseline: Baseline | None) -> GateResult:
    """Gate 1 — Validation results vs. baseline."""
    gate = GateResult(gate="Gate 1 (Validation)", verdict=Verdict.ACCEPT)

    # HARD asserts
    if not results.hard_all_passed:
        gate.verdict = Verdict.REJECT
        gate.reasons.append(
            f"HARD: {results.tests_passed}/{results.tests_total} passed — "
            f"HARD failures are non-negotiable"
        )
        return gate

    gate.reasons.append(f"HARD: {results.tests_passed}/{results.tests_total} passed")

    # SOFT score comparison
    if baseline:
        score_delta = results.avg_score - baseline.avg_score
        pct_delta = score_delta * 100

        if pct_delta < -5.0:
            gate.verdict = Verdict.REJECT
            gate.reasons.append(
                f"SOFT: {results.avg_score:.0%} avg "
                f"(baseline: {baseline.avg_score:.0%}) — "
                f"drop of {abs(pct_delta):.1f}pp exceeds 5pp threshold"
            )
        elif pct_delta < -1.0:
            gate.verdict = Verdict.HOLD
            gate.reasons.append(
                f"SOFT: {results.avg_score:.0%} avg "
                f"(baseline: {baseline.avg_score:.0%}) — "
                f"marginal drop of {abs(pct_delta):.1f}pp"
            )
        else:
            gate.reasons.append(
                f"SOFT: {results.avg_score:.0%} avg "
                f"(baseline: {baseline.avg_score:.0%}) — no regression"
            )
    else:
        gate.reasons.append(f"SOFT: {results.avg_score:.0%} avg (no baseline)")

    # Gaming warnings
    if baseline and results.gaming_warnings > baseline.gaming_warnings:
        gate.verdict = Verdict.HOLD
        gate.reasons.append(
            f"Gaming: {results.gaming_warnings} warnings "
            f"(baseline: {baseline.gaming_warnings}) — possible overfitting"
        )
    else:
        gate.reasons.append(f"Gaming: {results.gaming_warnings} warnings")

    return gate


def check_feedback(metrics: FeedbackMetrics | None) -> GateResult:
    """Gate 2 — Feedback adoption metrics."""
    gate = GateResult(gate="Gate 2 (Feedback)", verdict=Verdict.ACCEPT)

    if metrics is None:
        gate.verdict = Verdict.HOLD
        gate.reasons.append("No feedback data provided — gate skipped")
        return gate

    if metrics.total_activations < 10:
        gate.verdict = Verdict.HOLD
        gate.reasons.append(
            f"Only {metrics.total_activations} activations "
            f"(need ≥10 for confidence) — gate skipped"
        )
        return gate

    issues = []

    if metrics.adoption_rate < 0.25:
        issues.append(
            f"Adoption rate {metrics.adoption_rate:.0%} < 25% — "
            f"users aren't acting on recommendations"
        )

    if metrics.avg_time_to_action is not None and metrics.avg_time_to_action > 3.0:
        issues.append(
            f"Avg time-to-action {metrics.avg_time_to_action:.1f} turns > 3.0 — "
            f"output may be unclear"
        )

    if metrics.silence_rate > 0.50:
        issues.append(
            f"Silence rate {metrics.silence_rate:.0%} > 50% — "
            f"users are ignoring the skill"
        )

    if metrics.pattern_recurrence_rate > 0.30:
        issues.append(
            f"Pattern recurrence {metrics.pattern_recurrence_rate:.0%} > 30% — "
            f"fix isn't preventing the pattern"
        )

    if issues:
        gate.verdict = Verdict.HOLD
        gate.reasons.extend(issues)
    else:
        gate.reasons.append(
            f"Adoption {metrics.adoption_rate:.0%}, "
            f"TTA {metrics.avg_time_to_action or 'n/a'}, "
            f"silence {metrics.silence_rate:.0%}, "
            f"recurrence {metrics.pattern_recurrence_rate:.0%}"
        )

        # Strong confidence signal
        if (metrics.adoption_rate >= 0.50
                and metrics.avg_time_to_action is not None
                and metrics.avg_time_to_action <= 1.0):
            gate.reasons.append("Strong adoption signal — high confidence")

    return gate


# ---------------------------------------------------------------------------
# Decision combinator
# ---------------------------------------------------------------------------

def decide(
    validation: ValidationResults,
    baseline: Baseline | None,
    skill_content: str,
    skill_lines: int,
    feedback: FeedbackMetrics | None = None,
) -> DecisionResult:
    """Run all gates and produce a combined decision.

    Fail-closed: any ambiguity or unexpected state → HOLD.
    """
    gate1 = check_validation(validation, baseline)
    gate2 = check_feedback(feedback)
    gate3 = check_safety(skill_content, baseline, skill_lines)

    gates = [gate1, gate2, gate3]

    # Combination logic
    if gate1.verdict == Verdict.REJECT or gate3.verdict == Verdict.REJECT:
        verdict = Verdict.REJECT
        reasons = [
            r for g in gates
            for r in g.reasons
            if g.verdict == Verdict.REJECT
        ][:3]  # Limit to top 3 reasons
    elif any(g.verdict == Verdict.HOLD for g in gates):
        verdict = Verdict.HOLD
        reasons = ["One or more gates returned HOLD — manual review needed"]
    elif gate1.verdict == Verdict.ACCEPT and gate3.verdict == Verdict.ACCEPT:
        verdict = Verdict.ACCEPT
        reasons = ["All required gates passed"]
    else:
        # Fail-closed: unexpected combination
        verdict = Verdict.HOLD
        reasons = ["Unexpected gate combination — fail-closed to HOLD"]

    return DecisionResult(verdict=verdict, gates=gates, reasons=reasons)


# ---------------------------------------------------------------------------
# Convenience: create baseline from current state
# ---------------------------------------------------------------------------

def create_baseline(
    validation: ValidationResults,
    skill_path: Path,
) -> Baseline:
    """Create a baseline snapshot from current validation results."""
    return Baseline(
        timestamp=datetime.now(timezone.utc).isoformat(timespec="seconds"),
        tests_passed=validation.tests_passed,
        tests_total=validation.tests_total,
        avg_score=validation.avg_score,
        per_test_scores=validation.per_test_scores,
        gaming_warnings=validation.gaming_warnings,
        skill_lines=count_lines(skill_path),
        skill_hash=compute_skill_hash(skill_path),
    )
