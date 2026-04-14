# Validation

> **⚙️ Advanced** — This document is for contributors and technical reviewers.
> If you just want to use the skill, start with [QUICK-START.md](QUICK-START.md).

How to verify that orbit-engine is working correctly.

## Test coverage

The skill includes 13 eval cases covering every observable pattern and rule, plus 4 canonical tests that define the core behavioral contract.

### Positive cases (skill must activate)

| ID | Name | Tests |
| --- | --- | --- |
| 1 | Correction chain | Detection: 3+ short follow-ups correcting output. Diagnosis includes prompt quality. |
| 2 | Repeated edits to same file | Detection: same file edited 3+ times. Risk high or critical. |
| 3 | Complex migration without constraints | Detection: weak prompt on large task. Recommends Plan Mode + constraints. |
| 4 | Unsolicited long response | Detection: response far exceeded what prompt implied. |
| 5 | Exploratory reading without plan | Detection: 5+ files read in one turn without specific goal. |
| 6 | Large file pasted | Detection: large content pasted instead of @file reference. |
| 7 | Explicit request | Activation: direct question about efficiency. Always fires. |
| 8 | Ambiguous — fail closed | Activation: unclear signal, skill activates (cost of missing > false positive). |

### Gating cases (skill must block specific actions)

| ID | Name | Tests |
| --- | --- | --- |
| 9 | No /clear with unsaved context | Gating: blocks /clear when decisions are not persisted. Recommends /compact instead. |
| 10 | No /compact on short session | Gating: blocks /compact when conversation is short and context is light. |

### Negative cases (skill must NOT activate)

| ID | Name | Tests |
| --- | --- | --- |
| 11 | Simple question | No activation for one-line knowledge questions. |
| 12 | Trivial fix | No activation for trivial code corrections. |
| 13 | Casual conversation | No activation for social messages. |

---

## Output format checklist

Every positive activation must produce output matching its risk level:

### Risk: low

- [ ] DIAGNOSIS present with 1 bullet point
- [ ] Risk classification present: "low"
- [ ] One-line note: "No action required. Something to keep in mind."
- [ ] NO ACTIONS section
- [ ] NO DO NOT DO NOW section

### Risk: medium

- [ ] DIAGNOSIS present with 1–2 bullet points
- [ ] Risk classification present: "medium"
- [ ] ACTIONS present with 1–2 numbered items
- [ ] DO NOT DO NOW present with at least 1 item
- [ ] Actions include mix of commands AND prompt/structure advice

### Risk: high

- [ ] DIAGNOSIS present with 2–3 bullet points
- [ ] Risk classification present: "high — address before continuing"
- [ ] ACTIONS present with 2–3 numbered items
- [ ] DO NOT DO NOW present with at least 1 item
- [ ] Skill waits for user acknowledgment before proceeding

### Risk: critical

- [ ] DIAGNOSIS present with 2–3 bullet points
- [ ] Risk classification present: "critical"
- [ ] ACTIONS present, first item prefixed with ⚠️
- [ ] DO NOT DO NOW present with at least 1 item
- [ ] Skill waits for user to act on ⚠️ action before proceeding

### All risk levels

- [ ] Each diagnosis item describes something observable in the conversation
- [ ] No fabricated numbers, dollar amounts, or percentages
- [ ] No variation from the template structure
- [ ] Actions reference real Claude Code commands, prompt improvements, or task structure changes

---

## Pattern coverage matrix

| Observable Pattern | Eval IDs | Type |
| --- | --- | --- |
| Unsolicited long response | 4 | Detection |
| Correction chain (3+ follow-ups) | 1 | Detection |
| Repeated edits to same target | 2 | Detection |
| Exploratory reading without plan | 5 | Detection |
| Weak prompt (missing constraints) | 1, 3 | Detection |
| Large content pasted | 6 | Detection |
| /clear blocked when context unsaved | 9 | Gating |
| /compact blocked when context light | 10 | Gating |
| Rewrite prompt blocked when prompt is clear | 10 | Gating |
| Fail-closed on doubt | 8 | Activation |
| No activation for simple questions | 11 | Negative |
| No activation for trivial tasks | 12 | Negative |
| No activation for casual conversation | 13 | Negative |
| Block on complex prompt without code or constraints | CT1 | Canonical |
| Surgical diagnosis when code + constraints are present | CT2 | Canonical |
| Perceived quality under ambiguous intent | CT3 | Canonical |
| Perceived quality with subtle pattern + real code | CT4 | Canonical |

---

## How to run

### Manual testing

1. Install the skill: drag the `skill/` folder into Claude Code
2. Open a new session
3. Test each scenario one at a time
4. Compare the response against the expected output and risk-level checklist
5. Mark each assertion as pass or fail

### Automated testing

Run the test suite against golden reference outputs:

```bash
python3 tests/run_tests.py           # summary with scores
python3 tests/run_tests.py --verbose # assertion-level detail per test
python3 tests/run_tests.py --failures # detail only for failures
```

To validate real skill output: replace the simulated outputs in
`tests/test_validation.py` → `OUTPUTS` dict with actual skill responses,
then re-run. Every assertion maps directly to a rule in this document.

**Files:**

| File | Role |
| --- | --- |
| `tests/run_tests.py` | Runner — executes all tests, prints report with scores |
| `tests/test_validation.py` | 18 tests (13 evals + CT1–CT4 + format rules) |
| `tests/validator.py` | Parser + assertion library + gaming detector |
| `tests/fixtures/ingestion.ts` | Real TypeScript file for CT2 grounding |
| `tests/fixtures/auth-middleware.ts` | Real TypeScript file for CT4 grounding |

Total: 13 evals + 4 canonical tests + 1 cross-cutting format check.

---

## Severity model

Every assertion has a severity level that determines its impact:

### HARD — structural contract

A HARD assert enforces a non-negotiable rule from SKILL.md. If any HARD assert fails, the **test fails** and the exit code is 1.

Examples of HARD asserts:

- `DIAGNOSIS present` — the skill must activate
- `Risk >= high` — the risk level must be correct
- `No speculative code generated` — the skill must never generate code
- `Respects constraint boundaries` — must not suggest touching protected code

### SOFT — quality signal

A SOFT assert checks quality without blocking. If a SOFT assert fails, the test **still passes** but the quality score drops. This prevents overfitting: a response can be structurally correct but mediocre.

Examples of SOFT asserts:

- `No generic advice` — no consultant filler phrases
- `Action density` — each action has enough substance
- `Diagnosis specificity` — diagnosis contains real anchors (identifiers, quantities)
- `Grounding depth` — output references multiple code elements, not just one

### Scoring

Each assert contributes 1 point to the quality score. The score is the ratio of earned points to possible points (0.0 – 1.0). A test at 100% passed all HARD and SOFT asserts. A test at 80% passed all HARD asserts but some SOFT asserts flagged quality issues.

```text
Score = earned_points / max_points
Test pass/fail = all HARD asserts passed (SOFT ignored for verdict)
```

### Real-world feedback

Validation scores measure structural and perceived quality against golden outputs. To measure whether outputs are *actually useful* in practice, see [FEEDBACK.md](FEEDBACK.md) — a lightweight system that collects adoption metrics from real sessions and correlates them with validation scores.

### Anti-gaming detection

The test suite includes a cross-output gaming detector that analyzes all golden outputs together for three signals:

| Signal | What it catches | Threshold |
| --- | --- | --- |
| **Template repetition** | >50% of outputs share identical action skeletons (same structure, different nouns) | 3+ outputs with same skeleton |
| **Keyword stuffing** | Diagnosis items with >60% anchor density — unnatural concentration of identifiers | Per-item ratio check |
| **Structural cloning** | >50% of outputs share identical fingerprints (same bullet counts, same risk level) | 3+ outputs with same fingerprint |
| **Vocabulary monotony** | All action items across outputs reuse the same small vocabulary | Type-token ratio < 0.3 with ≥30 words |
| **Justification cloning** | Same "— why" justification text appears in 3+ different outputs | Exact match across outputs |

Gaming warnings appear in the test report but do not fail the suite. They are signals for manual review.

### Classification guide

When writing new tests, use this classification:

| Assert type | Severity | Category | Rationale |
| --- | --- | --- | --- |
| Section present/absent | HARD | Structural | Format contract — sections must exist |
| Risk level | HARD | Structural | Behavioral contract — classification must be proportional |
| Required content patterns | HARD | Structural | The skill must detect the specific pattern |
| Anti-speculation | HARD | Structural | The skill must never generate code |
| Constraint boundaries | HARD | Structural | The skill must respect declared boundaries |
| No generic advice | SOFT | Quality | Filler phrases reduce trust |
| Action density | SOFT | Quality | Actions with <4 substantive words are empty |
| Diagnosis specificity | SOFT | Quality | Anchors (identifiers, quantities) improve clarity |
| Echo detection | SOFT | Quality | Reframing is better than parroting input |
| Grounding depth | SOFT | Quality | More code references = more grounded |
| Conciseness | SOFT | Quality | Brevity is desirable |
| Action justification | SOFT | Perception | "— why" suffix shows reasoning, not just commands |
| Diagnosis actionable | SOFT | Perception | Each diagnosis item links to at least one action |
| Sections coherent | SOFT | Perception | ACTIONS and DO NOT DO NOW do not contradict |
| Risk proportionality | SOFT | Perception | 1 item ≠ critical, 3 items ≠ low |
| No hallucinated commands | SOFT | Robustness | Only valid commands (/clear, /compact, @file, etc.) |
| No contradictory actions | SOFT | Robustness | /clear + /compact or break + atomic in same output |
| Diagnosis not tautological | SOFT | Robustness | Items must have evidence beyond pattern labels |

---

## Canonical tests

These four tests define the core behavioral contract of the skill. They must all pass together — each one validates a dimension that the others cannot cover.

---

### Canonical Test 1 — Block on complex prompt with no grounding

**Classification:** Anti-regression · Positive case

**Input:**

```text
We have a TypeScript service doing Kafka ingestion, validation, transformation, and PostgreSQL writes. The current implementation is large, verbose, and hard to maintain. I suspect token waste and unnecessary abstractions. Help me fix it.
```

**Required behavior:**

- [ ] DIAGNOSIS present
- [ ] Detects absence of real code in the conversation
- [ ] Detects absence of constraints or scope boundaries
- [ ] Risk classified as `high` or `critical`
- [ ] Does NOT generate a refactor, template, or speculative architecture
- [ ] ACTIONS include: share real files, define scope, break into subtasks
- [ ] DO NOT DO NOW blocks: generating a generic Kafka/PostgreSQL service template

**Failure condition:**
Skill responds with any refactored code, architectural scaffolding, or pattern suggestions before the user has shared actual code.

---

### Canonical Test 2 — Surgical diagnosis with real code and constraints

**Classification:** Behavioral contract · Complementary to Canonical Test 1

**Input:**

Same scenario as Canonical Test 1, but the user provides:

1. A real file — for example, `ingestion.ts` pasted or referenced via `@file`
2. A constraint — for example: "Only touch the transformation step. Do not change the Kafka consumer or the DB writer."
3. A definition of done — for example: "Done = transformation logic fits in one function under 30 lines."

**Required behavior:**

- [ ] Skill does NOT block — proceeds to diagnosis
- [ ] DIAGNOSIS is specific to the actual code (references real patterns, not hypothetical ones)
- [ ] Risk level is proportional to what the code actually shows
- [ ] ACTIONS target specific lines, functions, or patterns in the shared file
- [ ] DO NOT DO NOW blocks changes outside the declared constraint boundary
- [ ] No speculative output — every action traceable to something in the file

**Failure condition:**
Skill continues to block, or produces generic advice that ignores the provided code and constraints.

---

### Canonical Test 3 — Ambiguous intent (perception under doubt)

**Classification:** Perception · Positive case

**Input:**

```text
I need to clean up this service, it's messy
```

No code shared, no constraints, request is ambiguous ("clean up" could mean rename, restructure, or delete dead code).

**Required behavior:**

- [ ] DIAGNOSIS present
- [ ] Detects that the request is ambiguous (multiple interpretations)
- [ ] Risk classified as `high` (ambiguity + no code = high risk of waste)
- [ ] Does NOT generate a refactor or pick an interpretation
- [ ] ACTIONS ask the user to clarify intent, define scope, and consider Plan Mode
- [ ] DO NOT DO NOW blocks acting on an assumed interpretation
- [ ] Each action includes a justification (— why)
- [ ] Diagnosis items are actionable (link to at least one action)

**Failure condition:**
Skill picks one interpretation of "clean up" and starts generating advice, or produces generic advice that would work for any ambiguous request.

---

### Canonical Test 4 — Perceived quality with subtle pattern and real code

**Classification:** Perception + Grounding · Complementary to CT2

**Input:**

Same class of scenario as CT2 (real code + constraints), but:

1. A real file — `auth-middleware.ts` with JWT verification duplicated in 3 functions
2. A constraint — "Only touch the verification logic. Do not change the token refresh flow."
3. A subtler pattern — duplication is not obvious (each copy differs in variable names and error handling)

**Required behavior:**

- [ ] DIAGNOSIS is specific to the actual duplication pattern (names functions, describes variance)
- [ ] Risk level is proportional (`medium` — known code, scoped constraint, not critical)
- [ ] ACTIONS target the specific duplication with actionable steps (extract, consolidate)
- [ ] Each action includes a justification (— why)
- [ ] DO NOT DO NOW protects the token refresh flow (constraint boundary)
- [ ] References real code elements (requireAuth, authenticateSocket, requireAdmin, verifyToken)
- [ ] No speculative output — every action traceable to the file

**Failure condition:**
Skill produces a correct structural output but with generic actions ("refactor the duplicated code") that don't reference the actual functions or explain why each action matters.

---

### Why all four tests are required

| | CT1 | CT2 | CT3 | CT4 |
| --- | --- | --- | --- | --- |
| **What it proves** | Fail-closed works | Unblocking works | Handles ambiguity | Quality is perceived |
| **Dimension** | Structural | Structural | Perception | Perception + Grounding |
| **Regression it catches** | Generates speculative code | Keeps blocking with real input | Picks an interpretation without asking | Output is correct but generic |
| **Risk if missing** | Silent quality degradation | Permanent block mode | Wasted work on wrong interpretation | User loses trust despite correct structure |

Passing only a subset is not sufficient. The skill must block speculation (CT1), unblock with evidence (CT2), handle ambiguity by asking (CT3), and deliver output that *feels* useful because it references real code and explains itself (CT4).
