"""
orbit-engine output validator.

Parses skill output text and provides assertion methods to verify
structural rules, required/forbidden content, and behavioral contracts
defined in VALIDATION.md and SKILL.md.

Severity model:
    HARD  — structural contract. Failure = test fails. Non-negotiable.
    SOFT  — quality signal. Failure = score penalty, NOT test failure.

Scoring:
    Each assert carries a point value (default 1). A test's quality score
    is the ratio of earned points to possible points (0.0–1.0). The test
    itself passes/fails based ONLY on HARD asserts.

Gaming detection:
    Cross-output analysis that catches formulaic responses designed to
    satisfy regex patterns without delivering real value.

Usage:
    from validator import OrbitOutputValidator
    v = OrbitOutputValidator(output_text)
    v.hard.assert_diagnosis_present()
    v.soft.assert_no_generic_advice()
    score = v.score           # 0.0–1.0
    passed = v.hard_passed    # bool — only HARD asserts
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from enum import Enum


# ---------------------------------------------------------------------------
# Parsed representation
# ---------------------------------------------------------------------------

@dataclass
class ParsedOrbitOutput:
    """Structured representation of an orbit-engine diagnosis block."""

    raw: str
    has_diagnosis: bool = False
    diagnosis_items: list[str] = field(default_factory=list)
    risk_level: str | None = None
    has_actions: bool = False
    action_items: list[str] = field(default_factory=list)
    has_do_not_do_now: bool = False
    do_not_do_now_items: list[str] = field(default_factory=list)
    has_urgent_marker: bool = False
    is_silent: bool = False
    is_healthy: bool = False


# ---------------------------------------------------------------------------
# Parser
# ---------------------------------------------------------------------------

_RISK_RE = re.compile(
    r"Risk:\s*(low|medium|high|critical)(?:\s*[—–-]\s*.+)?",
    re.IGNORECASE,
)

_BULLET_RE = re.compile(r"^\s*[-•]\s+(.+)", re.MULTILINE)
_NUMBERED_RE = re.compile(r"^\s*(?:⚠️\s*)?\d+\.\s+(.+)", re.MULTILINE)


def parse_output(text: str) -> ParsedOrbitOutput:
    """Parse raw orbit-engine output into structured form."""
    out = ParsedOrbitOutput(raw=text)

    stripped = text.strip()
    if not stripped:
        out.is_silent = True
        return out

    if re.search(r"session looks healthy", stripped, re.IGNORECASE):
        out.is_healthy = True
        return out

    # --- DIAGNOSIS section ---
    diag_match = re.search(
        r"DIAGNOSIS\s*\n(.*?)(?=\n\s*(?:Risk:|ACTIONS|DO NOT DO NOW)|$)",
        stripped,
        re.DOTALL | re.IGNORECASE,
    )
    if diag_match:
        out.has_diagnosis = True
        out.diagnosis_items = _BULLET_RE.findall(diag_match.group(1))

    # --- Risk level ---
    risk_match = _RISK_RE.search(stripped)
    if risk_match:
        out.risk_level = risk_match.group(1).lower()

    # --- ACTIONS section ---
    actions_match = re.search(
        r"ACTIONS\s*\n(.*?)(?=\n\s*(?:DO NOT DO NOW)|$)",
        stripped,
        re.DOTALL | re.IGNORECASE,
    )
    if actions_match:
        out.has_actions = True
        out.action_items = _NUMBERED_RE.findall(actions_match.group(1))

    # --- DO NOT DO NOW section ---
    dndn_match = re.search(
        r"DO NOT DO NOW\s*\n(.*?)$",
        stripped,
        re.DOTALL | re.IGNORECASE,
    )
    if dndn_match:
        out.has_do_not_do_now = True
        out.do_not_do_now_items = _BULLET_RE.findall(dndn_match.group(1))

    # --- Urgent marker ---
    out.has_urgent_marker = "⚠️" in stripped

    return out


# ---------------------------------------------------------------------------
# Severity
# ---------------------------------------------------------------------------

class Severity(Enum):
    HARD = "HARD"   # structural contract — test fails on violation
    SOFT = "SOFT"   # quality signal — score penalty, test still passes


# ---------------------------------------------------------------------------
# Assertion result carrier
# ---------------------------------------------------------------------------

@dataclass
class AssertRecord:
    name: str
    passed: bool
    severity: Severity
    detail: str = ""
    points: float = 1.0      # max score this assert contributes


class AssertionResult:
    """Accumulates pass/fail with severity and scoring."""

    def __init__(self) -> None:
        self.records: list[AssertRecord] = []

    def record(self, name: str, passed: bool, severity: Severity,
               detail: str = "", points: float = 1.0) -> None:
        self.records.append(AssertRecord(name, passed, severity, detail, points))

    # -- HARD-only verdict (determines test pass/fail) --
    @property
    def hard_passed(self) -> bool:
        return all(r.passed for r in self.records if r.severity == Severity.HARD)

    @property
    def hard_failed_count(self) -> int:
        return sum(1 for r in self.records
                   if r.severity == Severity.HARD and not r.passed)

    # -- scoring (HARD + SOFT combined) --
    @property
    def max_points(self) -> float:
        return sum(r.points for r in self.records) or 1.0

    @property
    def earned_points(self) -> float:
        return sum(r.points for r in self.records if r.passed)

    @property
    def score(self) -> float:
        return self.earned_points / self.max_points

    # -- legacy compat --
    @property
    def all_passed(self) -> bool:
        return all(r.passed for r in self.records)

    @property
    def passed(self) -> int:
        return sum(1 for r in self.records if r.passed)

    @property
    def failed(self) -> int:
        return sum(1 for r in self.records if not r.passed)

    @property
    def total(self) -> int:
        return len(self.records)

    def summary(self) -> str:
        lines: list[str] = []
        for r in self.records:
            if r.passed:
                icon = "✅"
            elif r.severity == Severity.HARD:
                icon = "❌"
            else:
                icon = "⚠️"
            tag = f"[{r.severity.value}]"
            line = f"  {icon} {tag:6s} {r.name}"
            if r.detail:
                line += f"  — {r.detail}"
            lines.append(line)
        lines.append(f"  Score: {self.score:.0%} ({self.earned_points:.0f}/{self.max_points:.0f})")
        return "\n".join(lines)


# ---------------------------------------------------------------------------
# Validator
# ---------------------------------------------------------------------------

class _SeverityProxy:
    """Proxy that routes assert calls through the validator with a fixed severity.

    Usage:
        v = OrbitOutputValidator(text)
        v.hard.assert_diagnosis_present()   # HARD severity
        v.soft.assert_no_generic_advice()   # SOFT severity
        v.assert_diagnosis_present()        # direct call → HARD (default)
    """

    def __init__(self, validator: "OrbitOutputValidator", severity: Severity) -> None:
        self._v = validator
        self._sev = severity

    def __getattr__(self, name: str):
        method = getattr(self._v, name)
        if not callable(method) or not name.startswith("assert_"):
            return method

        def wrapper(*args, **kwargs):
            prev = self._v._current_severity
            self._v._current_severity = self._sev
            try:
                return method(*args, **kwargs)
            finally:
                self._v._current_severity = prev
        return wrapper


class OrbitOutputValidator:
    """Validates orbit-engine output against VALIDATION.md rules.

    Assert methods record results with the current severity context.
    Use ``v.hard.assert_*()`` for structural contracts (HARD FAIL) and
    ``v.soft.assert_*()`` for quality signals (SOFT FAIL / score penalty).
    Direct ``v.assert_*()`` calls default to HARD.
    """

    def __init__(self, raw_output: str) -> None:
        self.raw = raw_output
        self.parsed = parse_output(raw_output)
        self.results = AssertionResult()
        self._current_severity: Severity = Severity.HARD
        self.hard = _SeverityProxy(self, Severity.HARD)
        self.soft = _SeverityProxy(self, Severity.SOFT)

    # -- convenience properties -----------------------------------------------

    @property
    def hard_passed(self) -> bool:
        return self.results.hard_passed

    @property
    def score(self) -> float:
        return self.results.score

    # -- internal recording helper -------------------------------------------

    def _rec(self, name: str, ok: bool, detail: str = "",
             points: float = 1.0) -> bool:
        self.results.record(name, ok, self._current_severity, detail, points)
        return ok

    # -- structure assertions -------------------------------------------------

    def assert_diagnosis_present(self) -> bool:
        return self._rec("DIAGNOSIS present", self.parsed.has_diagnosis)

    def assert_diagnosis_absent(self) -> bool:
        ok = not self.parsed.has_diagnosis and not self.parsed.has_actions
        return self._rec("DIAGNOSIS absent (no activation)", ok)

    def assert_risk(self, *expected: str) -> bool:
        ok = self.parsed.risk_level in [e.lower() for e in expected]
        return self._rec(
            f"Risk is {'/'.join(expected)}",
            ok,
            f"got '{self.parsed.risk_level}'",
        )

    def assert_risk_at_least(self, minimum: str) -> bool:
        order = ["low", "medium", "high", "critical"]
        min_idx = order.index(minimum.lower())
        actual = self.parsed.risk_level
        ok = actual is not None and order.index(actual) >= min_idx
        return self._rec(f"Risk >= {minimum}", ok, f"got '{actual}'")

    def assert_actions_present(self) -> bool:
        ok = self.parsed.has_actions and len(self.parsed.action_items) > 0
        return self._rec("ACTIONS present", ok)

    def assert_actions_count(self, min_count: int = 1, max_count: int = 3) -> bool:
        n = len(self.parsed.action_items)
        ok = min_count <= n <= max_count
        return self._rec(f"ACTIONS count in [{min_count}, {max_count}]", ok, f"got {n}")

    def assert_do_not_do_now_present(self) -> bool:
        ok = self.parsed.has_do_not_do_now and len(self.parsed.do_not_do_now_items) > 0
        return self._rec("DO NOT DO NOW present", ok)

    def assert_diagnosis_count(self, min_count: int = 1, max_count: int = 3) -> bool:
        n = len(self.parsed.diagnosis_items)
        ok = min_count <= n <= max_count
        return self._rec(f"DIAGNOSIS items in [{min_count}, {max_count}]", ok, f"got {n}")

    def assert_urgent_marker(self) -> bool:
        return self._rec("⚠️ urgent marker present", self.parsed.has_urgent_marker)

    def assert_silent(self) -> bool:
        return self._rec("Output is silent (no activation)", self.parsed.is_silent)

    def assert_healthy(self) -> bool:
        return self._rec("Session healthy message", self.parsed.is_healthy)

    # -- content assertions ---------------------------------------------------

    def assert_contains(self, pattern: str, label: str | None = None) -> bool:
        ok = bool(re.search(pattern, self.raw, re.IGNORECASE))
        return self._rec(label or f"Contains /{pattern}/", ok)

    def assert_not_contains(self, pattern: str, label: str | None = None) -> bool:
        ok = not bool(re.search(pattern, self.raw, re.IGNORECASE))
        return self._rec(label or f"Does NOT contain /{pattern}/", ok)

    def assert_contains_any(self, patterns: list[str], label: str | None = None) -> bool:
        ok = any(re.search(p, self.raw, re.IGNORECASE) for p in patterns)
        return self._rec(label or f"Contains any of {patterns}", ok)

    # -- anti-speculation assertions ------------------------------------------

    def assert_no_speculative_code(self) -> bool:
        """Fail if the output contains code blocks that look like implementations."""
        code_blocks = re.findall(r"```(?:ts|typescript|js|javascript)?\n(.+?)```", self.raw, re.DOTALL)
        speculative_markers = [
            r"(?:export\s+)?(?:async\s+)?function\s+\w+",
            r"(?:export\s+)?class\s+\w+",
            r"(?:const|let|var)\s+\w+\s*=\s*(?:new|async|\()",
            r"import\s+\{.+\}\s+from",
        ]
        has_spec = False
        for block in code_blocks:
            for marker in speculative_markers:
                if re.search(marker, block):
                    has_spec = True
                    break
        return self._rec("No speculative code generated", not has_spec)

    def assert_no_fabricated_numbers(self) -> bool:
        """Fail if output contains dollar amounts or fabricated percentages."""
        patterns = [
            r"\$\d+",
            r"\d+%\s*(?:fewer|less|more|saved|reduced|improvement)",
            r"~?\d+\s*tokens?\s*(?:saved|wasted|consumed)",
        ]
        has_numbers = any(re.search(p, self.raw, re.IGNORECASE) for p in patterns)
        return self._rec("No fabricated numbers/dollars/percentages", not has_numbers)

    # -- grounding assertions (CT2) -------------------------------------------

    def assert_references_real_code(self, markers: list[str]) -> bool:
        """Verify the output references specific real elements from the code."""
        found = [m for m in markers if re.search(re.escape(m), self.raw, re.IGNORECASE)]
        ok = len(found) >= 1
        return self._rec(
            "References real code elements",
            ok,
            f"found {found}" if found else f"none of {markers} found",
        )

    def assert_respects_constraint_boundary(self, protected_names: list[str]) -> bool:
        """Verify the output does NOT suggest changing protected components."""
        violations = []
        full_check = "\n".join([
            "\n".join(self.parsed.action_items),
            "\n".join(self.parsed.diagnosis_items),
        ])
        for name in protected_names:
            if re.search(
                rf"(?:refactor|rewrite|change|modify|touch|edit|restructure|move|split)\s+.*{re.escape(name)}",
                full_check,
                re.IGNORECASE,
            ):
                violations.append(name)
        return self._rec(
            "Respects constraint boundaries",
            len(violations) == 0,
            f"violations: {violations}" if violations else "",
        )

    # -- semantic quality assertions ------------------------------------------

    def assert_no_generic_advice(self) -> bool:
        """Fail if output contains vague filler phrases that carry no signal."""
        generic_phrases = [
            r"consider\s+(?:refactoring|improving|optimizing|reviewing)",
            r"you\s+(?:might|may)\s+want\s+to",
            r"it\s+(?:would|might)\s+be\s+(?:good|beneficial|helpful)\s+to",
            r"best\s+practic(?:e|es)",
            r"follow\s+(?:the\s+)?(?:solid|dry|kiss)\s+principles?",
            r"ensure\s+(?:proper|adequate)\s+(?:error\s+handling|testing|logging)",
            r"improve\s+(?:code\s+)?(?:readability|maintainability|quality)",
            r"clean(?:er)?\s+(?:code|architecture)",
            r"industry\s+standard",
            r"as\s+a\s+general\s+rule",
        ]
        found = [p for p in generic_phrases if re.search(p, self.raw, re.IGNORECASE)]
        return self._rec(
            "No generic advice / filler phrases",
            len(found) == 0,
            f"found: {found}" if found else "",
        )

    def assert_action_density(self, min_words: int = 4) -> bool:
        """Verify every action item has enough substance (not just filler)."""
        command_re = re.compile(r"(?:/\w|@file|shift\+tab|plan\s*mode)", re.IGNORECASE)
        weak: list[str] = []
        for item in self.parsed.action_items:
            if command_re.search(item):
                continue
            core = re.split(r"\s*[—–-]\s+", item, maxsplit=1)[0]
            words = [w for w in core.split() if len(w) > 2]
            if len(words) < min_words:
                weak.append(item[:60])
        return self._rec(
            f"Action density (>= {min_words} substantive words each)",
            len(weak) == 0,
            f"weak: {weak}" if weak else "",
        )

    def assert_grounding_depth(self, markers: list[str], min_matches: int = 2) -> bool:
        """Verify output references multiple real code elements, not just one."""
        found = [m for m in markers if re.search(re.escape(m), self.raw, re.IGNORECASE)]
        return self._rec(
            f"Grounding depth (>= {min_matches} code refs)",
            len(found) >= min_matches,
            f"found {len(found)}/{len(markers)}: {found}",
        )

    def assert_not_echo(self, input_phrases: list[str], max_echo_ratio: float = 0.5) -> bool:
        """Detect if the output just echoes back the user's input phrases."""
        if not self.parsed.diagnosis_items:
            return self._rec("Not echoing input", True, "no diagnosis to check")

        echo_count = 0
        for diag in self.parsed.diagnosis_items:
            for phrase in input_phrases:
                phrase_words = set(phrase.lower().split())
                diag_words = set(diag.lower().split())
                if len(phrase_words) > 0:
                    overlap = len(phrase_words & diag_words) / len(phrase_words)
                    if overlap >= 0.6:
                        echo_count += 1
                        break

        ratio = echo_count / len(self.parsed.diagnosis_items)
        return self._rec(
            f"Not echoing input (echo ratio <= {max_echo_ratio})",
            ratio <= max_echo_ratio,
            f"echo ratio: {ratio:.2f} ({echo_count}/{len(self.parsed.diagnosis_items)})",
        )

    def assert_output_conciseness(self, max_lines: int = 15) -> bool:
        """Verify the diagnosis block isn't excessively long."""
        block_patterns = [r"DIAGNOSIS", r"Risk:", r"ACTIONS", r"DO NOT DO NOW"]
        lines = self.raw.strip().split("\n")
        start = None
        end = len(lines)
        for i, line in enumerate(lines):
            if start is None and any(re.search(p, line, re.IGNORECASE) for p in block_patterns):
                start = i
            if start is not None:
                end = i + 1
        if start is None:
            return self._rec(f"Output conciseness (<= {max_lines} lines)", True, "no block found")
        block_line_count = end - start
        return self._rec(
            f"Output conciseness (<= {max_lines} lines)",
            block_line_count <= max_lines,
            f"got {block_line_count} lines",
        )

    def assert_diagnosis_specificity(self) -> bool:
        """Verify that diagnosis items contain at least one specific anchor."""
        anchor_re = re.compile(
            r"(?:"
            r"[a-zA-Z_]\w*\.[a-zA-Z]\w*"
            r"|[a-zA-Z]+/[a-zA-Z]"
            r"|[a-z]+[A-Z][a-zA-Z]*"
            r"|[a-zA-Z]+_[a-zA-Z]+"
            r"|`.+?`"
            r"|\d+\s*(?:times?|files?|lines?|edits?|corrections?|turns?|messages?|consecutive)"
            r"|(?:inline|nested|speculative|god\s*function|rework|dead.?letter)"
            r"|(?:no\s+(?:code|files?|constraints?|scope|boundar))"
            r"|(?:@file|/clear|/compact|shift\+tab)"
            r")"
        )
        weak: list[str] = []
        for item in self.parsed.diagnosis_items:
            if not anchor_re.search(item):
                weak.append(item[:80])
        return self._rec(
            "Diagnosis specificity (contains identifiers, quantities, or technical terms)",
            len(weak) == 0,
            f"weak items: {weak}" if weak else "",
        )

    # -- perception assertions (CT3/CT4) --------------------------------------

    def assert_diagnosis_actionable(self) -> bool:
        """Every diagnosis item should connect to at least one action.

        A good diagnosis isn't just observation — it motivates a concrete
        step. We check that key nouns from each diagnosis bullet appear
        somewhere in the ACTIONS section, ensuring the skill doesn't just
        list problems without solutions.
        """
        if not self.parsed.diagnosis_items or not self.parsed.action_items:
            return self._rec("Diagnosis → actionable link", True, "nothing to check")

        actions_text = " ".join(self.parsed.action_items).lower()
        orphan: list[str] = []
        # Extract significant words (>4 chars) from each diagnosis
        for item in self.parsed.diagnosis_items:
            sig_words = [w.lower().strip("—–-()") for w in item.split()
                         if len(w) > 4 and not w.startswith("-")]
            # At least one significant word must appear in ACTIONS
            if sig_words and not any(w in actions_text for w in sig_words):
                orphan.append(item[:60])
        return self._rec(
            "Diagnosis items are actionable (linked to ACTIONS)",
            len(orphan) == 0,
            f"orphaned: {orphan}" if orphan else "",
        )

    def assert_action_justification(self) -> bool:
        """Every action should include a justification (the '— why' suffix).

        Good actions look like:
            'Extract timestamp normalization — reduces god function complexity'
        Bad actions look like:
            'Extract timestamp normalization'

        The dash separator (—, –, or -) followed by ≥3 words is the signal.
        Exception: very short actions that ARE a command (e.g., /compact "...")
        carry their own justification in the command argument.
        """
        command_re = re.compile(r"/\w+\s+\"", re.IGNORECASE)
        missing: list[str] = []
        justify_re = re.compile(r"\s*[—–-]\s+\S+(?:\s+\S+){2,}")
        for item in self.parsed.action_items:
            if command_re.search(item):
                continue  # command with quoted arg = self-justifying
            if not justify_re.search(item):
                missing.append(item[:60])
        return self._rec(
            "Actions include justification (— why)",
            len(missing) == 0,
            f"missing why: {missing}" if missing else "",
        )

    def assert_risk_proportionality(self) -> bool:
        """Risk level should be proportional to the number of diagnosis items.

        Heuristic from SKILL.md:
            1 item → low/medium
            2 items → medium/high
            3 items → high/critical

        This catches over-escalation (1 bullet = critical) and
        under-escalation (3 serious bullets = low).
        """
        if not self.parsed.risk_level or not self.parsed.diagnosis_items:
            return self._rec("Risk proportionality", True, "nothing to check")

        n = len(self.parsed.diagnosis_items)
        risk = self.parsed.risk_level
        order = {"low": 0, "medium": 1, "high": 2, "critical": 3}
        risk_idx = order.get(risk, -1)

        # Acceptable ranges (flexible — not exact)
        ok = True
        detail = f"{n} items → {risk}"
        if n == 1 and risk_idx > 1:
            ok = False  # 1 item shouldn't be high/critical
            detail += " (over-escalated)"
        elif n >= 3 and risk_idx < 1:
            ok = False  # 3 items shouldn't be low
            detail += " (under-escalated)"
        return self._rec("Risk proportionality (items ↔ risk level)", ok, detail)

    def assert_sections_coherent(self) -> bool:
        """DO NOT DO NOW must not contradict ACTIONS.

        If ACTIONS says "do X" and DO NOT DO NOW also says "do X", the
        output is incoherent. We check that no action verb+target pair
        appears in both sections.
        """
        if not self.parsed.action_items or not self.parsed.do_not_do_now_items:
            return self._rec("Sections coherent (ACTIONS ↔ DNDN)", True, "nothing to check")

        # Extract verb+object patterns from actions
        verb_re = re.compile(
            r"(extract|separate|rewrite|refactor|move|split|merge|create|add|remove|delete|"
            r"use|define|break|clarify|share|save|compact|clear)\s+(.{3,30})",
            re.IGNORECASE,
        )
        action_targets = set()
        for item in self.parsed.action_items:
            for m in verb_re.finditer(item):
                action_targets.add((m.group(1).lower(), m.group(2).lower().strip()[:20]))

        conflicts: list[str] = []
        for dndn in self.parsed.do_not_do_now_items:
            for m in verb_re.finditer(dndn):
                verb = m.group(1).lower()
                target = m.group(2).lower().strip()[:20]
                for av, at in action_targets:
                    # Same verb AND overlapping target
                    if av == verb and (target in at or at in target):
                        conflicts.append(f"ACTIONS '{av} {at}' vs DNDN '{verb} {target}'")
        return self._rec(
            "Sections coherent (ACTIONS ↔ DNDN)",
            len(conflicts) == 0,
            f"conflicts: {conflicts}" if conflicts else "",
        )

    # -- robustness assertions ------------------------------------------------

    def assert_no_hallucinated_commands(self) -> bool:
        """Verify that commands referenced in ACTIONS actually exist.

        The valid Claude Code command vocabulary from SKILL.md:
            /clear, /compact, @file, Shift+Tab (Plan Mode)
        If the output recommends a command that doesn't exist (like /analyze,
        /optimize, /refactor), the skill is hallucinating.
        """
        valid_commands = {
            "/clear", "/compact", "@file", "shift+tab", "plan mode",
        }
        # Find all /command or @command patterns in actions
        cmd_re = re.compile(r"(/[a-z]+|@[a-z]+)", re.IGNORECASE)
        hallucinated: list[str] = []
        for item in self.parsed.action_items:
            for m in cmd_re.finditer(item):
                cmd = m.group(1).lower()
                if cmd not in valid_commands:
                    hallucinated.append(cmd)
        return self._rec(
            "No hallucinated commands",
            len(hallucinated) == 0,
            f"unknown commands: {hallucinated}" if hallucinated else "",
        )

    def assert_no_contradictory_actions(self) -> bool:
        """Verify that actions don't contradict each other.

        Known contradiction pairs:
            /clear + /compact  (both manage context, mutually exclusive intent)
            "rewrite prompt" + "prompt is clear"  (conflicting assessment)
        """
        items_text = " ".join(self.parsed.action_items).lower()
        contradictions: list[str] = []

        # Pair 1: /clear and /compact in same ACTIONS
        has_clear = "/clear" in items_text
        has_compact = "/compact" in items_text
        if has_clear and has_compact:
            contradictions.append("/clear + /compact recommended simultaneously")

        # Pair 2: break into subtasks + atomic/simple/single-step
        has_break = bool(re.search(r"break.*(?:subtask|smaller|batch)", items_text))
        has_atomic = bool(re.search(r"(?:simple|atomic|single.?step|trivial)", items_text))
        if has_break and has_atomic:
            contradictions.append("break into subtasks + described as simple/atomic")

        return self._rec(
            "No contradictory actions",
            len(contradictions) == 0,
            f"contradictions: {contradictions}" if contradictions else "",
        )

    def assert_diagnosis_not_tautological(self) -> bool:
        """Verify diagnosis items aren't just restating the pattern name.

        A tautological diagnosis: 'Correction chain detected'
        A good diagnosis: '3 consecutive corrections after initial response'

        The difference: a good diagnosis includes EVIDENCE, not just a label.
        We check that each item has ≥ N words beyond the pattern label.
        """
        pattern_labels = [
            "correction chain", "repeated edit", "rework pattern",
            "exploratory reading", "weak prompt", "large paste",
            "unsolicited response", "long response", "complex task",
            "no constraints", "missing constraints",
        ]
        tautological: list[str] = []
        for item in self.parsed.diagnosis_items:
            words = item.lower().split()
            # Check if the item is basically just a label
            for label in pattern_labels:
                label_words = set(label.split())
                item_words_set = set(words)
                # If label words make up >60% of the item's content, it's a tautology
                if len(label_words) >= 2 and label_words <= item_words_set:
                    non_label = [w for w in words if w not in label_words and len(w) > 2]
                    if len(non_label) < 3:
                        tautological.append(item[:60])
                        break
        return self._rec(
            "Diagnosis not tautological (includes evidence beyond label)",
            len(tautological) == 0,
            f"tautological: {tautological}" if tautological else "",
        )


# ---------------------------------------------------------------------------
# Gaming detector — cross-output analysis
# ---------------------------------------------------------------------------

class GamingDetector:
    """Detects formulaic / gamed outputs across a test suite.

    Three signals:
    1. **Template repetition** — action items are suspiciously similar across
       different tests (same sentence structure, just nouns swapped).
    2. **Keyword stuffing** — diagnosis items contain an unnatural density of
       anchor keywords purely to satisfy regex checks.
    3. **Structural cloning** — multiple outputs share identical section
       structure (same number of bullets, same line count, same risk).

    Usage:
        detector = GamingDetector()
        for name, output in all_outputs.items():
            detector.add(name, output)
        warnings = detector.analyze()   # list[str]
    """

    def __init__(self) -> None:
        self._outputs: dict[str, ParsedOrbitOutput] = {}

    def add(self, name: str, raw_output: str) -> None:
        self._outputs[name] = parse_output(raw_output)

    def analyze(self) -> list[str]:
        warnings: list[str] = []
        warnings.extend(self._check_template_repetition())
        warnings.extend(self._check_keyword_stuffing())
        warnings.extend(self._check_structural_cloning())
        warnings.extend(self._check_vocabulary_monotony())
        warnings.extend(self._check_justification_cloning())
        return warnings

    # -- signal 1: template repetition ----------------------------------------

    @staticmethod
    def _skeleton(text: str) -> str:
        """Replace identifiers/numbers with placeholders to compare structure."""
        s = text.lower().strip()
        s = re.sub(r"[a-zA-Z_]\w*\.[a-zA-Z]\w*", "<ID>", s)   # file.ext
        s = re.sub(r"`[^`]+`", "<ID>", s)                       # backticks
        s = re.sub(r"\d+", "<N>", s)                             # numbers
        s = re.sub(r"[a-z]+[A-Z][a-zA-Z]*", "<ID>", s)          # camelCase
        s = re.sub(r"\s+", " ", s)
        return s

    def _check_template_repetition(self) -> list[str]:
        """If >50% of outputs share the same action skeleton, flag it."""
        if len(self._outputs) < 4:
            return []

        skeleton_map: dict[str, list[str]] = {}
        for name, parsed in self._outputs.items():
            if not parsed.action_items:
                continue
            skel = tuple(self._skeleton(a) for a in parsed.action_items)
            key = "||".join(skel)
            skeleton_map.setdefault(key, []).append(name)

        warnings: list[str] = []
        for skel, names in skeleton_map.items():
            ratio = len(names) / len(self._outputs)
            if ratio > 0.5 and len(names) > 2:
                warnings.append(
                    f"GAMING: template repetition — {len(names)}/{len(self._outputs)} "
                    f"outputs share identical action skeleton ({', '.join(names[:3])}…)"
                )
        return warnings

    # -- signal 2: keyword stuffing -------------------------------------------

    def _check_keyword_stuffing(self) -> list[str]:
        """If diagnosis items have unnaturally high anchor density, flag it."""
        anchor_re = re.compile(
            r"[a-zA-Z_]\w*\.[a-zA-Z]\w*|[a-z]+[A-Z][a-zA-Z]*|"
            r"`[^`]+`|\d+\s*(?:times?|files?|lines?|edits?)"
        )

        warnings: list[str] = []
        for name, parsed in self._outputs.items():
            for item in parsed.diagnosis_items:
                words = item.split()
                if len(words) < 3:
                    continue
                anchors = anchor_re.findall(item)
                ratio = len(anchors) / len(words)
                if ratio > 0.6:
                    warnings.append(
                        f"GAMING: keyword stuffing in {name} — "
                        f"anchor density {ratio:.0%} in '{item[:60]}…'"
                    )
        return warnings

    # -- signal 3: structural cloning -----------------------------------------

    def _check_structural_cloning(self) -> list[str]:
        """If many outputs have identical structural fingerprints, flag it."""
        if len(self._outputs) < 4:
            return []

        fingerprints: dict[str, list[str]] = {}
        for name, parsed in self._outputs.items():
            if parsed.is_silent or parsed.is_healthy:
                continue
            fp = (
                f"diag={len(parsed.diagnosis_items)},"
                f"act={len(parsed.action_items)},"
                f"dndn={len(parsed.do_not_do_now_items)},"
                f"risk={parsed.risk_level}"
            )
            fingerprints.setdefault(fp, []).append(name)

        warnings: list[str] = []
        for fp, names in fingerprints.items():
            ratio = len(names) / len(self._outputs)
            if ratio > 0.5 and len(names) > 2:
                warnings.append(
                    f"GAMING: structural cloning — {len(names)}/{len(self._outputs)} "
                    f"outputs share fingerprint [{fp}] ({', '.join(names[:3])}…)"
                )
        return warnings

    # -- signal 4: vocabulary monotony ----------------------------------------

    def _check_vocabulary_monotony(self) -> list[str]:
        """Flag if outputs share a suspiciously small vocabulary.

        A real skill adapting to different scenarios uses different words.
        If the combined unique-word set across all action items is very small
        relative to total word count, the outputs are formulaic.

        Metric: type-token ratio (TTR) across all action items.
        TTR < 0.3 with ≥ 30 total words = monotonous.
        """
        all_words: list[str] = []
        for parsed in self._outputs.values():
            for item in parsed.action_items:
                all_words.extend(w.lower().strip(".,;:—–-()\"'") for w in item.split()
                                 if len(w) > 2)
        if len(all_words) < 30:
            return []

        unique = set(all_words)
        ttr = len(unique) / len(all_words)
        if ttr < 0.3:
            return [
                f"GAMING: vocabulary monotony — TTR {ttr:.2f} across "
                f"{len(all_words)} action words ({len(unique)} unique). "
                f"Expected ≥ 0.30 for varied responses."
            ]
        return []

    # -- signal 5: justification cloning --------------------------------------

    def _check_justification_cloning(self) -> list[str]:
        """Flag if the '— why' suffixes are copied between outputs.

        Unique actions should have unique justifications. If the same
        justification text appears in 3+ different outputs, it's a template.
        """
        justify_re = re.compile(r"\s*[—–-]\s+(.{10,})")
        justification_map: dict[str, list[str]] = {}

        for name, parsed in self._outputs.items():
            for item in parsed.action_items:
                m = justify_re.search(item)
                if m:
                    # Normalize: lowercase, collapse whitespace
                    j = re.sub(r"\s+", " ", m.group(1).lower().strip())
                    justification_map.setdefault(j, []).append(name)

        warnings: list[str] = []
        for justification, names in justification_map.items():
            unique_outputs = set(names)
            if len(unique_outputs) >= 3:
                warnings.append(
                    f"GAMING: justification cloning — '{justification[:50]}…' "
                    f"appears in {len(unique_outputs)} different outputs "
                    f"({', '.join(sorted(unique_outputs)[:3])}…)"
                )
        return warnings

