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
import math
import re
from dataclasses import asdict, dataclass, field
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
    category_scores: dict[str, float] = field(default_factory=dict)

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
# Category scores — group test results by type
# ---------------------------------------------------------------------------

_CATEGORY_RULES: list[tuple[str, str]] = [
    # (substring in test name, category)
    ("CT1", "canonical"),
    ("CT2", "canonical"),
    ("CT3", "canonical"),
    ("CT4", "canonical"),
    ("Eval 11", "silence"),
    ("Eval 12", "silence"),
    ("Eval 13", "silence"),
    ("Eval 09", "gating"),
    ("Eval 10", "gating"),
    ("Format rules", "structural"),
]


def _classify_test(name: str) -> str:
    """Return the category for a test name."""
    for substr, cat in _CATEGORY_RULES:
        if substr in name:
            return cat
    return "structural"


def compute_category_scores(
    per_test_scores: dict[str, float],
) -> dict[str, float]:
    """Compute avg score per category from per-test scores."""
    buckets: dict[str, list[float]] = {}
    for name, score in per_test_scores.items():
        cat = _classify_test(name)
        buckets.setdefault(cat, []).append(score)
    return {
        cat: sum(vals) / len(vals) if vals else 0.0
        for cat, vals in sorted(buckets.items())
    }


# ---------------------------------------------------------------------------
# Evidence log — append-only JSONL
# ---------------------------------------------------------------------------

_EVIDENCE_LOG_PATH = Path(__file__).parent / "evidence_log.jsonl"


def append_evidence(result: "DecisionResult",
                    validation: "ValidationResults",
                    feedback: "FeedbackMetrics | None",
                    baseline: "Baseline | None",
                    skill_hash: str) -> None:
    """Append one decision record to evidence_log.jsonl."""
    entry = {
        "timestamp": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "verdict": result.verdict.value,
        "gates": {
            g.gate: g.verdict.value for g in result.gates
        },
        "tests_passed": validation.tests_passed,
        "tests_total": validation.tests_total,
        "avg_score": round(validation.avg_score, 4),
        "gaming_warnings": validation.gaming_warnings,
        "category_scores": {
            k: round(v, 4) for k, v in result.category_scores.items()
        },
        "skill_hash": skill_hash,
    }
    if baseline:
        entry["baseline_score"] = round(baseline.avg_score, 4)
        entry["baseline_hash"] = baseline.skill_hash
    if feedback:
        entry["feedback"] = {
            "activations": feedback.total_activations,
            "adoption": round(feedback.adoption_rate, 4),
            "tta": feedback.avg_time_to_action,
            "silence": round(feedback.silence_rate, 4),
            "recurrence": round(feedback.pattern_recurrence_rate, 4),
        }
    with open(_EVIDENCE_LOG_PATH, "a", encoding="utf-8") as fh:
        fh.write(json.dumps(entry) + "\n")


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

    # Gaming warnings — absolute + relative thresholds
    if results.gaming_warnings >= 2:
        gate.verdict = Verdict.REJECT
        gate.reasons.append(
            f"Gaming: {results.gaming_warnings} warnings ≥ 2 — "
            f"strong overfitting signal"
        )
    elif baseline and results.gaming_warnings > baseline.gaming_warnings:
        gate.verdict = Verdict.HOLD
        gate.reasons.append(
            f"Gaming: {results.gaming_warnings} warnings "
            f"(baseline: {baseline.gaming_warnings}) — possible overfitting"
        )
    else:
        gate.reasons.append(f"Gaming: {results.gaming_warnings} warnings")

    return gate


def check_feedback(metrics: FeedbackMetrics | None) -> GateResult:
    """Gate 2 — Feedback adoption metrics with confidence check.

    Confidence zones prevent borderline metrics from sneaking through:
      - n < 10: skip entirely (insufficient data)
      - n 10-29: narrower acceptance bands (HOLD on borderline)
      - n >= 30: standard thresholds apply
    """
    gate = GateResult(gate="Gate 2 (Feedback)", verdict=Verdict.ACCEPT)

    if metrics is None:
        gate.verdict = Verdict.HOLD
        gate.reasons.append("No feedback data provided — gate skipped")
        return gate

    n = metrics.total_activations

    if n < 10:
        gate.verdict = Verdict.HOLD
        gate.reasons.append(
            f"Only {n} activations "
            f"(need ≥10 for confidence) — gate skipped"
        )
        return gate

    # Confidence tier: low-n gets tighter thresholds
    low_confidence = n < 30
    adopt_threshold = 0.35 if low_confidence else 0.25
    tta_threshold = 2.5 if low_confidence else 3.0
    silence_threshold = 0.40 if low_confidence else 0.50
    recurrence_threshold = 0.25 if low_confidence else 0.30

    if low_confidence:
        gate.reasons.append(
            f"Low-confidence mode (n={n}, need ≥30 for standard) — "
            f"tighter thresholds applied"
        )

    issues: list[str] = []

    if metrics.adoption_rate < adopt_threshold:
        issues.append(
            f"Adoption rate {metrics.adoption_rate:.0%} < "
            f"{adopt_threshold:.0%} — "
            f"users aren't acting on recommendations"
        )

    if metrics.avg_time_to_action is not None and metrics.avg_time_to_action > tta_threshold:
        issues.append(
            f"Avg time-to-action {metrics.avg_time_to_action:.1f} turns > "
            f"{tta_threshold:.1f} — output may be unclear"
        )

    if metrics.silence_rate > silence_threshold:
        issues.append(
            f"Silence rate {metrics.silence_rate:.0%} > "
            f"{silence_threshold:.0%} — users are ignoring the skill"
        )

    if metrics.pattern_recurrence_rate > recurrence_threshold:
        issues.append(
            f"Pattern recurrence {metrics.pattern_recurrence_rate:.0%} > "
            f"{recurrence_threshold:.0%} — fix isn't preventing the pattern"
        )

    # Ambiguity check: when metrics are close to thresholds with low n,
    # even a "pass" is unreliable — force HOLD
    if not issues and low_confidence:
        margins = []
        margins.append(metrics.adoption_rate - adopt_threshold)
        if metrics.avg_time_to_action is not None:
            margins.append(tta_threshold - metrics.avg_time_to_action)
        margins.append(silence_threshold - metrics.silence_rate)
        margins.append(recurrence_threshold - metrics.pattern_recurrence_rate)

        min_margin = min(margins) if margins else 1.0
        if min_margin < 0.05:
            gate.verdict = Verdict.HOLD
            gate.reasons.append(
                f"Borderline metrics (min margin {min_margin:.0%}) with "
                f"n={n} — not enough confidence to ACCEPT"
            )
            return gate

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
        if (not low_confidence
                and metrics.adoption_rate >= 0.50
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

    return DecisionResult(
        verdict=verdict, gates=gates, reasons=reasons,
        category_scores=compute_category_scores(validation.per_test_scores),
    )


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
