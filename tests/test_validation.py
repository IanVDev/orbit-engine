"""
orbit-engine validation tests.

Maps every eval case from VALIDATION.md into executable assertions.
Each test receives a simulated skill output and verifies it against the
behavioral contract defined in SKILL.md.

Severity model:
    v.hard.assert_*()  →  HARD — structural contract.  Failure = test fails.
    v.soft.assert_*()  →  SOFT — quality signal.  Failure = score penalty.

A test PASSES when all HARD asserts pass. SOFT failures only reduce the
quality score (0.0 – 1.0). See validator.py for details.

Test count: 13 evals + 4 canonical tests + 1 cross-cutting format check = 18

Run:  python tests/run_tests.py
"""

from __future__ import annotations

from validator import OrbitOutputValidator, GamingDetector


# ═══════════════════════════════════════════════════════════════════════════
# SIMULATED OUTPUTS — paste real skill outputs here to run live validation.
# These defaults are "golden" reference outputs that match SKILL.md rules.
# ═══════════════════════════════════════════════════════════════════════════

OUTPUTS: dict[str, str] = {

    # ------------------------------------------------------------------
    # POSITIVE CASES (skill must activate)
    # ------------------------------------------------------------------

    "eval_01_correction_chain": """\
DIAGNOSIS
- 3 consecutive corrections after initial response — prompt lacked constraints
- Same files edited multiple times (auth.ts, routes.ts, schema.ts)
Risk: high — address before continuing

ACTIONS
1. Restate request with boundaries — "extract auth middleware from auth.ts into middleware/auth.ts, don't touch routes or schema"
2. Shift+Tab (Plan Mode) — confirm scope before executing
3. /compact "preserve current file state and user corrections" — reduce accumulated rework from context

DO NOT DO NOW
- Continue editing without a clear scope — each correction adds another full pass to context
""",

    "eval_02_repeated_edits": """\
DIAGNOSIS
- api/routes.ts edited 4 times across the conversation — rework pattern
- Each re-edit of routes.ts re-processes all previous context for overlapping diffs
Risk: critical

ACTIONS
⚠️ 1. /compact "preserve final state of api/routes.ts and current task requirements" — collapse the rework history
2. Break remaining work into batches — finish routes.ts completely before moving to the next file
3. Define done for routes.ts — what does "finished" look like?

DO NOT DO NOW
- Edit routes.ts again without first confirming the final requirements
""",

    "eval_03_complex_migration_no_constraints": """\
DIAGNOSIS
- Large task with no scope constraints — high speculation risk
Risk: medium

ACTIONS
1. Shift+Tab (Plan Mode) — map dependencies and define migration order before touching any file
2. Define done with acceptance criteria — which models are critical? What does "migrated" mean for each?

DO NOT DO NOW
- Start migrating without a plan — generates speculative code that will need rework
""",

    "eval_04_unsolicited_long_response": """\
DIAGNOSIS
- Response far exceeded the request — user asked for a one-line fix, got a full rewrite
Risk: medium

ACTIONS
1. Restate with explicit constraint — "Fix only the type error on line 42 of utils.ts, change nothing else"
2. Use @file:utils.ts:calculateTotal to reference just the function, not the whole file

DO NOT DO NOW
- Accept the full rewrite — it changes behavior beyond what was requested
""",

    "eval_05_exploratory_reading": """\
DIAGNOSIS
- 12 files read in one turn without a specific search goal
- Large summary generated — most content unused in subsequent 5 messages
Risk: high — address before continuing

ACTIONS
1. Add boundary — "Focus on src/auth/ only" or "I need to understand the payment flow specifically"
2. /compact "preserve summary of auth module only" — discard unrelated file contents

DO NOT DO NOW
- Ask to "understand" another broad area — compounds the context with more unused content
""",

    "eval_06_large_paste": """\
DIAGNOSIS
- Large code block pasted (full controller) when only one function was relevant
Risk: low

No action required. Something to keep in mind.
""",

    "eval_07_explicit_request": """\
DIAGNOSIS
- Complex task started without Plan Mode
- Context not cleared from previous session
Risk: high — address before continuing

ACTIONS
1. Shift+Tab (Plan Mode) — scope first, then execute
2. /compact "preserve schema decisions" — clear safely
3. @file:schema.ts instead of full file dumps

DO NOT DO NOW
- /clear before planning — would lose current context
""",

    "eval_08_ambiguous_fail_closed": """\
DIAGNOSIS
- Complex multi-step request with vague scope
Risk: medium

ACTIONS
1. Clarify scope before executing — what specific files and behavior are in scope?
2. Shift+Tab (Plan Mode) — outline approach first

DO NOT DO NOW
- Start implementation without confirming boundaries
""",

    # ------------------------------------------------------------------
    # GATING CASES (skill must block specific actions)
    # ------------------------------------------------------------------

    "eval_09_no_clear_with_unsaved": """\
DIAGNOSIS
- Context contains unresolved decisions and working state
Risk: medium

ACTIONS
1. /compact "preserve authentication decisions and current task" — safely summarize instead of clearing
2. Save key decisions to a file before any context operations

DO NOT DO NOW
- /clear — would destroy unsaved decisions and plans
""",

    "eval_10_no_compact_short_session": """\
Session looks healthy. No action needed.
""",

    # ------------------------------------------------------------------
    # NEGATIVE CASES (skill must NOT activate)
    # ------------------------------------------------------------------

    "eval_11_simple_question": "",  # silence

    "eval_12_trivial_fix": "",  # silence

    "eval_13_casual": "",  # silence

    # ------------------------------------------------------------------
    # CANONICAL TESTS
    # ------------------------------------------------------------------

    "ct1_block_no_grounding": """\
DIAGNOSIS
- Complex refactor requested but no code has been shared — entire analysis would be speculative
- No constraints provided — no file targets, no scope boundaries, no definition of done
Risk: high — address before continuing

ACTIONS
1. Share the actual files — paste or reference via @file the TypeScript service code
2. Define scope with boundaries — which part of the pipeline to focus on (ingestion, validation, transformation, writes)?
3. Break the work into sequential subtasks — tackle one component at a time with clear acceptance criteria

DO NOT DO NOW
- Do not generate a typical Kafka ingestion service template — without seeing the real code, any refactor is guesswork
""",

    "ct2_surgical_with_code": """\
DIAGNOSIS
- transformAndValidate is a 60-line god function mixing validation, normalization, enrichment, and formatting
- Timestamp normalization logic is inline with nested conditionals instead of an extracted utility
Risk: medium

ACTIONS
1. Extract timestamp normalization into a pure function — reduces transformAndValidate to composition of named steps
2. Separate Zod schema definition from transformation — schema.parse should happen before the transformation pipeline

DO NOT DO NOW
- Touch the Kafka consumer or database writer — user constraint explicitly excludes them
""",

    "ct3_ambiguous_intent": """\
DIAGNOSIS
- Request can be interpreted as cosmetic cleanup or deep architectural refactor — scope is ambiguous
- No constraints provided to distinguish between a quick rename pass and a full restructure
Risk: high — address before continuing

ACTIONS
1. Clarify intent before executing — "Do you want to rename variables only, or restructure the module boundaries?"
2. Define scope with boundaries — which files are in scope and what does "clean up" mean concretely?
3. Shift+Tab (Plan Mode) — outline both interpretations and let the user pick one

DO NOT DO NOW
- Start refactoring without confirming which interpretation is correct — wrong guess wastes a full pass of edits
""",

    "ct4_perceived_quality": """\
DIAGNOSIS
- JWT verification logic duplicated in 3 places with minor variations (requireAuth, authenticateSocket, requireAdmin)
- Each copy handles token extraction and error responses slightly differently — inconsistency risk
Risk: medium

ACTIONS
1. Extract a shared verifyToken utility that handles JWT parsing — all 3 callers delegate to it with their own error handling
2. Keep role checks in the route-level middleware — separation of authentication vs authorization

DO NOT DO NOW
- Touch refreshToken or lookupRefreshToken — user constraint explicitly excludes the refresh flow
""",
}


# ═══════════════════════════════════════════════════════════════════════════
# TESTS
# ═══════════════════════════════════════════════════════════════════════════

class TestSuite:
    """All eval + canonical tests as methods."""

    def __init__(self) -> None:
        self.passed = 0
        self.failed = 0
        self._results: list[tuple[str, bool, float, str]] = []  # (name, ok, score, summary)

    def _run(self, name: str, output_key: str, fn) -> None:
        v = OrbitOutputValidator(OUTPUTS[output_key])
        fn(v)
        ok = v.hard_passed          # only HARD failures fail the test
        score = v.score             # 0.0–1.0 including SOFT
        self._results.append((name, ok, score, v.results.summary()))
        if ok:
            self.passed += 1
        else:
            self.failed += 1

    # ---------------------------------------------------------------
    # POSITIVE CASES
    # ---------------------------------------------------------------

    def test_eval_01_correction_chain(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_diagnosis_count(1, 3)
            v.hard.assert_risk_at_least("high")
            v.hard.assert_actions_present()
            v.hard.assert_do_not_do_now_present()
            v.hard.assert_contains(r"correct|follow.?up|prompt", "Mentions correction/prompt issue")
            v.hard.assert_no_fabricated_numbers()
            v.soft.assert_no_generic_advice()
            v.soft.assert_action_density(min_words=4)
            v.soft.assert_diagnosis_specificity()
        self._run("Eval 01 — Correction chain", "eval_01_correction_chain", check)

    def test_eval_02_repeated_edits(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_risk("high", "critical")
            v.hard.assert_actions_present()
            v.hard.assert_do_not_do_now_present()
            v.hard.assert_contains(r"edited.*times|rework", "Mentions repeated edits/rework")
            v.hard.assert_no_fabricated_numbers()
            v.soft.assert_no_generic_advice()
            v.soft.assert_action_density(min_words=4)
            v.soft.assert_diagnosis_specificity()
        self._run("Eval 02 — Repeated edits", "eval_02_repeated_edits", check)

    def test_eval_03_complex_migration(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_risk_at_least("medium")
            v.hard.assert_actions_present()
            v.hard.assert_do_not_do_now_present()
            v.hard.assert_contains_any(
                [r"plan\s*mode", r"shift\+tab", r"define\s*done", r"constraints"],
                "Recommends planning or constraints",
            )
            v.hard.assert_no_speculative_code()
            v.soft.assert_no_generic_advice()
            v.soft.assert_action_density(min_words=4)
        self._run("Eval 03 — Complex migration no constraints", "eval_03_complex_migration_no_constraints", check)

    def test_eval_04_unsolicited_long(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_risk_at_least("medium")
            v.hard.assert_actions_present()
            v.hard.assert_do_not_do_now_present()
            v.hard.assert_contains(r"exceed|long|rewrite", "Mentions over-generation")
            v.soft.assert_no_generic_advice()
            v.soft.assert_action_density(min_words=4)
        self._run("Eval 04 — Unsolicited long response", "eval_04_unsolicited_long_response", check)

    def test_eval_05_exploratory_reading(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_risk_at_least("high")
            v.hard.assert_actions_present()
            v.hard.assert_do_not_do_now_present()
            v.hard.assert_contains(r"files?\s*read|search\s*goal|boundary", "Mentions aimless reading")
            v.soft.assert_no_generic_advice()
            v.soft.assert_diagnosis_specificity()
        self._run("Eval 05 — Exploratory reading", "eval_05_exploratory_reading", check)

    def test_eval_06_large_paste(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_risk("low")
            v.hard.assert_contains(r"keep in mind|no action required", "Low risk — no action note")
            v.hard.assert_not_contains(r"^ACTIONS\s*$", "No ACTIONS section for low risk")
        self._run("Eval 06 — Large paste", "eval_06_large_paste", check)

    def test_eval_07_explicit_request(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_risk_at_least("medium")
            v.hard.assert_actions_present()
            v.hard.assert_do_not_do_now_present()
        self._run("Eval 07 — Explicit request", "eval_07_explicit_request", check)

    def test_eval_08_fail_closed(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_risk_at_least("medium")
            v.hard.assert_actions_present()
            v.hard.assert_do_not_do_now_present()
        self._run("Eval 08 — Ambiguous fail-closed", "eval_08_ambiguous_fail_closed", check)

    # ---------------------------------------------------------------
    # GATING CASES
    # ---------------------------------------------------------------

    def test_eval_09_no_clear_unsaved(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_diagnosis_present()
            v.hard.assert_actions_present()
            v.hard.assert_not_contains(
                r"(?:^|\n)\s*(?:⚠️\s*)?\d+\.\s*/clear\b",
                "Does NOT recommend /clear (unsaved context)",
            )
            v.hard.assert_contains(r"/compact", "Recommends /compact as safe alternative")
        self._run("Eval 09 — No /clear with unsaved context", "eval_09_no_clear_with_unsaved", check)

    def test_eval_10_no_compact_short(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_healthy()
            v.hard.assert_not_contains(r"/compact", "Does NOT recommend /compact on short session")
        self._run("Eval 10 — No /compact on short session", "eval_10_no_compact_short_session", check)

    # ---------------------------------------------------------------
    # NEGATIVE CASES
    # ---------------------------------------------------------------

    def test_eval_11_simple_question(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_silent()
        self._run("Eval 11 — Simple question (silence)", "eval_11_simple_question", check)

    def test_eval_12_trivial_fix(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_silent()
        self._run("Eval 12 — Trivial fix (silence)", "eval_12_trivial_fix", check)

    def test_eval_13_casual(self) -> None:
        def check(v: OrbitOutputValidator):
            v.hard.assert_silent()
        self._run("Eval 13 — Casual conversation (silence)", "eval_13_casual", check)

    # ---------------------------------------------------------------
    # CANONICAL TEST 1 — Block on complex prompt with no grounding
    # ---------------------------------------------------------------

    def test_ct1_block_no_grounding(self) -> None:
        def check(v: OrbitOutputValidator):
            # Structure (HARD)
            v.hard.assert_diagnosis_present()
            v.hard.assert_diagnosis_count(1, 3)
            v.hard.assert_risk_at_least("high")
            v.hard.assert_actions_present()
            v.hard.assert_actions_count(1, 3)
            v.hard.assert_do_not_do_now_present()

            # Must detect absence of code (HARD)
            v.hard.assert_contains_any(
                [r"no code", r"no files?\s*(?:shared|provided|referenced)", r"speculative"],
                "CT1: Detects absence of real code",
            )

            # Must detect absence of constraints (HARD)
            v.hard.assert_contains_any(
                [r"no constraint", r"no scope", r"no boundar", r"no definition of done", r"missing constraint"],
                "CT1: Detects absence of constraints",
            )

            # Must NOT generate speculative code (HARD)
            v.hard.assert_no_speculative_code()

            # Must NOT contain fabricated numbers (HARD)
            v.hard.assert_no_fabricated_numbers()

            # Actions must ask for real input (HARD)
            v.hard.assert_contains_any(
                [r"share.*(?:file|code)", r"@file", r"paste.*code", r"actual.*file"],
                "CT1: Actions ask user to share real files",
            )
            v.hard.assert_contains_any(
                [r"define.*scope", r"define.*done", r"break.*subtask", r"scope.*boundar"],
                "CT1: Actions ask for scope/subtasks",
            )

            # DO NOT DO NOW must block speculative generation (HARD)
            v.hard.assert_contains_any(
                [r"do not.*generat", r"template", r"guesswork", r"speculative", r"without.*(?:seeing|real)"],
                "CT1: DO NOT DO NOW blocks speculative generation",
            )

            # --- Semantic quality (SOFT) ---
            v.soft.assert_no_generic_advice()
            v.soft.assert_action_density(min_words=4)
            v.soft.assert_not_echo([
                "TypeScript service doing Kafka ingestion",
                "large verbose and hard to maintain",
                "token waste and unnecessary abstractions",
            ])
            v.soft.assert_output_conciseness(max_lines=15)
            v.soft.assert_diagnosis_specificity()

        self._run("CT1 — Block on complex prompt (no grounding)", "ct1_block_no_grounding", check)

    # ---------------------------------------------------------------
    # CANONICAL TEST 2 — Surgical diagnosis with real code
    # ---------------------------------------------------------------

    def test_ct2_surgical_with_code(self) -> None:
        def check(v: OrbitOutputValidator):
            # Structure (HARD)
            v.hard.assert_diagnosis_present()
            v.hard.assert_diagnosis_count(1, 3)
            v.hard.assert_risk_at_least("low")
            v.hard.assert_actions_present()
            v.hard.assert_actions_count(1, 3)
            v.hard.assert_do_not_do_now_present()

            # Risk proportionality (HARD)
            v.hard.assert_risk("low", "medium")

            # Diagnosis must reference real code (HARD)
            v.hard.assert_references_real_code([
                "transformAndValidate",
                "timestamp",
                "normalization",
                "Zod",
                "schema",
                "enrichment",
                "god function",
            ])

            # Actions must target specific patterns (HARD)
            v.hard.assert_contains_any(
                [r"extract", r"separate", r"pure function", r"named step"],
                "CT2: Actions target specific code patterns",
            )

            # Must respect constraint boundary (HARD)
            v.hard.assert_respects_constraint_boundary([
                "consumer",
                "startConsumer",
                "writeToDatabase",
                "pool",
            ])

            # DO NOT DO NOW must protect declared boundary (HARD)
            v.hard.assert_contains_any(
                [r"kafka.*consumer", r"database.*writer", r"db.*writer", r"constraint.*exclud"],
                "CT2: DO NOT DO NOW protects constraint boundary",
            )

            # Anti-speculation (HARD)
            v.hard.assert_no_speculative_code()
            v.hard.assert_no_fabricated_numbers()

            # --- Semantic quality (SOFT) ---
            v.soft.assert_no_generic_advice()
            v.soft.assert_action_density(min_words=4)
            v.soft.assert_grounding_depth(
                markers=[
                    "transformAndValidate",
                    "timestamp",
                    "normalization",
                    "Zod",
                    "schema",
                    "enrichment",
                    "god function",
                    "typeMap",
                    "dbRecord",
                    "normalizedTimestamp",
                ],
                min_matches=3,
            )
            v.soft.assert_not_echo([
                "Only touch the transformation step",
                "Do not change the Kafka consumer or the DB writer",
                "transformation logic fits in one function under 30 lines",
            ])
            v.soft.assert_diagnosis_specificity()

        self._run("CT2 — Surgical diagnosis (real code)", "ct2_surgical_with_code", check)

    # ---------------------------------------------------------------
    # CANONICAL TEST 3 — Ambiguous intent (perception under doubt)
    # ---------------------------------------------------------------

    def test_ct3_ambiguous_intent(self) -> None:
        def check(v: OrbitOutputValidator):
            # Structure (HARD) — must activate fail-closed
            v.hard.assert_diagnosis_present()
            v.hard.assert_diagnosis_count(1, 3)
            v.hard.assert_risk_at_least("medium")
            v.hard.assert_actions_present()
            v.hard.assert_actions_count(1, 3)
            v.hard.assert_do_not_do_now_present()

            # Must detect ambiguity (HARD)
            v.hard.assert_contains_any(
                [r"ambigu", r"interpret", r"unclear", r"clarif"],
                "CT3: Detects ambiguous intent",
            )

            # Must NOT just pick one interpretation and go (HARD)
            v.hard.assert_no_speculative_code()

            # Must ask the user to disambiguate (HARD)
            v.hard.assert_contains_any(
                [r"clarif.*intent", r"which.*interpret", r"do you want",
                 r"confirm.*scope", r"define.*scope"],
                "CT3: Asks user to disambiguate",
            )

            # Anti-speculation (HARD)
            v.hard.assert_no_fabricated_numbers()

            # --- Perception quality (SOFT) ---
            v.soft.assert_no_generic_advice()
            v.soft.assert_action_justification()
            v.soft.assert_diagnosis_actionable()
            v.soft.assert_sections_coherent()
            v.soft.assert_risk_proportionality()
            v.soft.assert_diagnosis_specificity()
            v.soft.assert_action_density(min_words=4)

            # --- Robustness (SOFT) ---
            v.soft.assert_no_hallucinated_commands()
            v.soft.assert_no_contradictory_actions()
            v.soft.assert_diagnosis_not_tautological()

        self._run("CT3 — Ambiguous intent (perception under doubt)", "ct3_ambiguous_intent", check)

    # ---------------------------------------------------------------
    # CANONICAL TEST 4 — Perceived quality (subtle pattern + real code)
    # ---------------------------------------------------------------

    def test_ct4_perceived_quality(self) -> None:
        def check(v: OrbitOutputValidator):
            # Structure (HARD)
            v.hard.assert_diagnosis_present()
            v.hard.assert_diagnosis_count(1, 3)
            v.hard.assert_risk_at_least("low")
            v.hard.assert_actions_present()
            v.hard.assert_actions_count(1, 3)
            v.hard.assert_do_not_do_now_present()

            # Risk proportionality — duplication is medium, not critical (HARD)
            v.hard.assert_risk("low", "medium")

            # Must reference real code from auth-middleware.ts (HARD)
            v.hard.assert_references_real_code([
                "requireAuth",
                "authenticateSocket",
                "requireAdmin",
                "verifyToken",
                "JWT",
                "jwt",
            ])

            # Must respect constraint boundary (HARD)
            v.hard.assert_respects_constraint_boundary([
                "refreshToken",
                "lookupRefreshToken",
            ])

            # DO NOT DO NOW protects the refresh flow (HARD)
            v.hard.assert_contains_any(
                [r"refresh", r"lookupRefresh", r"constraint.*exclud"],
                "CT4: DO NOT DO NOW protects refresh flow",
            )

            # Anti-speculation (HARD)
            v.hard.assert_no_speculative_code()
            v.hard.assert_no_fabricated_numbers()

            # --- Perception quality (SOFT) — this is the core of CT4 ---
            v.soft.assert_no_generic_advice()
            v.soft.assert_action_justification()
            v.soft.assert_diagnosis_actionable()
            v.soft.assert_sections_coherent()
            v.soft.assert_risk_proportionality()
            v.soft.assert_diagnosis_specificity()
            v.soft.assert_action_density(min_words=4)

            # Deep grounding — must reference ≥3 code elements (SOFT)
            v.soft.assert_grounding_depth(
                markers=[
                    "requireAuth", "authenticateSocket", "requireAdmin",
                    "verifyToken", "JWT", "jwt.verify", "token",
                    "Bearer", "role", "userId",
                ],
                min_matches=3,
            )

            # --- Robustness (SOFT) ---
            v.soft.assert_no_hallucinated_commands()
            v.soft.assert_no_contradictory_actions()
            v.soft.assert_diagnosis_not_tautological()

            # Must not echo the user's constraint
            v.soft.assert_not_echo([
                "clean up the auth middleware",
                "duplicated verification logic",
                "don't touch the refresh token flow",
            ])

        self._run("CT4 — Perceived quality (subtle pattern + real code)", "ct4_perceived_quality", check)

    # ---------------------------------------------------------------
    # FORMAT RULES (apply to all positive outputs)
    # ---------------------------------------------------------------

    def test_format_rules_all_positive(self) -> None:
        """Cross-cutting rules from SKILL.md that apply to every positive case."""
        positive_keys = [k for k in OUTPUTS if OUTPUTS[k].strip() and "healthy" not in OUTPUTS[k].lower()]

        ok = True
        details: list[str] = []
        for key in positive_keys:
            v = OrbitOutputValidator(OUTPUTS[key])
            p = v.parsed
            if p.has_diagnosis and len(p.diagnosis_items) > 3:
                ok = False
                details.append(f"{key}: DIAGNOSIS has {len(p.diagnosis_items)} items (max 3)")
            if p.has_actions and len(p.action_items) > 3:
                ok = False
                details.append(f"{key}: ACTIONS has {len(p.action_items)} items (max 3)")
            if p.risk_level in ("high", "critical") and not p.has_do_not_do_now:
                ok = False
                details.append(f"{key}: risk={p.risk_level} but missing DO NOT DO NOW")
            # Semantic: no generic advice in any output
            v.soft.assert_no_generic_advice()
            if not v.results.all_passed:
                ok = False
                details.append(f"{key}: contains generic advice")

        self._results.append(("Format rules — all positive outputs", ok, 1.0 if ok else 0.0,
                              "; ".join(details) if details else ""))
        if ok:
            self.passed += 1
        else:
            self.failed += 1

    # ---------------------------------------------------------------
    # Run all
    # ---------------------------------------------------------------

    def run_all(self) -> None:
        """Execute every test method in order."""
        methods = [m for m in dir(self) if m.startswith("test_")]
        methods.sort()
        for name in methods:
            getattr(self, name)()

    def run_gaming_analysis(self) -> list[str]:
        """Cross-output gaming detection over all golden outputs."""
        detector = GamingDetector()
        for key, output in OUTPUTS.items():
            if output.strip():
                detector.add(key, output)
        return detector.analyze()
