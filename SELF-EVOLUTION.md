# Self-Evolution

> **⚙️ Advanced** — This document is for contributors evolving the skill with safety guarantees.
> If you just want to use the skill, start with [QUICK-START.md](QUICK-START.md).

How to use orbit-engine to improve its own skill files, with external validation as the final gate.

---

## The problem

The skill detects waste patterns in conversations. When used on its own development sessions, it can suggest improvements to its own logic. But a system that modifies itself without external checks will drift — optimizing for its own metrics instead of real quality.

**Principle:** The skill may *suggest* changes. Only external evidence may *approve* them.

---

## Architecture

```text
┌─────────────────────────────────────────────────────────┐
│                    evolve.py (orchestrator)              │
│                                                         │
│  1. BACKUP     snapshot current skill/SKILL.md          │
│  2. PROPOSE    apply candidate changes to skill/        │
│  3. VALIDATE   run tests → HARD pass + SOFT score       │
│  4. DECIDE     decision_engine.py → accept or reject    │
│  5. COMMIT     accept: keep changes · reject: restore   │
│                                                         │
└─────────────────────────────────────────────────────────┘
         │                 │                  │
         ▼                 ▼                  ▼
   tests/run_tests.py   feedback.jsonl   decision_engine.py
   (18 HARD/SOFT)        (adoption data)  (gate logic)
```

### Single command

```bash
python3 tests/evolve.py skill/SKILL.md
```

This runs the full cycle: backup → validate → decide → accept or reject.

---

## Decision engine

The decision engine is a pure function with no learning, no weights, and no model. It takes two inputs — validation results and feedback metrics — and returns one of three verdicts.

### Verdicts

| Verdict | Meaning | Action |
| --- | --- | --- |
| **ACCEPT** | All gates passed | Keep the changes |
| **REJECT** | At least one gate failed | Restore backup |
| **HOLD** | Gates passed but feedback data is insufficient | Keep changes, flag for manual review |

### Gate 1 — Validation (required)

| Condition | Verdict |
| --- | --- |
| Any HARD assert fails | **REJECT** — non-negotiable |
| Average SOFT score drops > 5 points vs. baseline | **REJECT** — quality regression |
| Average SOFT score drops 1–5 points vs. baseline | **HOLD** — marginal, needs review |
| Gaming detector fires a new warning | **HOLD** — possible overfitting |
| All HARD pass + SOFT score ≥ baseline | Gate passes |

### Gate 2 — Feedback correlation (when data exists)

| Condition | Verdict |
| --- | --- |
| No feedback data available | Skip gate (not blocking) |
| Adoption rate < 25% across ≥ 10 sessions | **HOLD** — tests pass but users don't act |
| Time-to-action > 3.0 turns average | **HOLD** — output may be unclear |
| Silence rate > 50% | **HOLD** — users are ignoring the skill |
| Pattern recurrence > 30% | **HOLD** — fix isn't preventing the pattern |
| Adoption ≥ 50% AND time-to-action ≤ 1.0 | Gate passes with confidence |

### Gate 3 — Safety invariants (required)

| Condition | Verdict |
| --- | --- |
| Skill file grew > 20% in lines | **HOLD** — complexity increase needs review |
| Any observable pattern was removed | **REJECT** — coverage regression |
| Output format template was modified | **REJECT** — breaking contract |
| Silence rule was weakened or removed | **REJECT** — false positive risk |
| Gating rules were weakened | **REJECT** — safety regression |

### Combination logic

```text
IF gate_1 == REJECT OR gate_3 == REJECT:
    → REJECT (restore backup, no exceptions)

IF gate_1 == HOLD OR gate_2 == HOLD OR gate_3 == HOLD:
    → HOLD (keep changes, flag for manual review)

IF gate_1 == PASS AND gate_3 == PASS:
    IF gate_2 == PASS OR gate_2 == SKIP:
        → ACCEPT
```

**Fail-closed:** Any ambiguity or unexpected state → HOLD. Never auto-accept when uncertain.

---

## Baseline management

The system needs a baseline to compare against. The baseline is a snapshot of scores from the last accepted state.

### File: `tests/.baseline.json`

```json
{
  "timestamp": "2026-04-14T12:00:00Z",
  "tests_passed": 18,
  "tests_total": 18,
  "avg_score": 0.99,
  "per_test_scores": {
    "CT1": 1.0,
    "CT2": 1.0,
    "CT3": 0.95,
    "CT4": 0.92
  },
  "gaming_warnings": 0,
  "skill_lines": 260,
  "skill_hash": "sha256:abc123..."
}
```

Created automatically by `evolve.py --save-baseline` or on first ACCEPT.

---

## Usage

### Save current baseline

```bash
python3 tests/evolve.py --save-baseline
```

Captures current test scores and skill metrics as the reference point.

### Run evolution cycle

```bash
python3 tests/evolve.py skill/SKILL.md
```

The cycle:

1. Copies `skill/SKILL.md` → `skill/SKILL.md.bak`
2. Runs tests against the current (candidate) state
3. Compares results against `.baseline.json`
4. Optionally loads `feedback.jsonl` for adoption data
5. Runs decision engine
6. Prints verdict with reasoning
7. If REJECT: restores backup automatically

### Dry run (validate without commit)

```bash
python3 tests/evolve.py skill/SKILL.md --dry-run
```

Runs all gates and prints the verdict without restoring or committing.

### With feedback data

```bash
python3 tests/evolve.py skill/SKILL.md --feedback feedback.jsonl
```

Includes feedback metrics in Gate 2 evaluation.

---

## Typical workflow

```text
1. Developer modifies skill/SKILL.md (directly or via skill suggestion)

2. Run the evolution cycle:
   $ python3 tests/evolve.py skill/SKILL.md

3. See the verdict:

   ┌────────────────────────────────────────────────┐
   │  orbit-engine evolution gate                   │
   │                                                │
   │  Gate 1 (Validation):  PASS                    │
   │    HARD: 18/18 passed                          │
   │    SOFT: 99% avg (baseline: 99%) → no drop     │
   │    Gaming: 0 new warnings                      │
   │                                                │
   │  Gate 2 (Feedback):    SKIP                    │
   │    No feedback data provided                   │
   │                                                │
   │  Gate 3 (Safety):      PASS                    │
   │    Lines: 260 → 265 (+1.9%) — within 20%      │
   │    Patterns: 6/6 present                       │
   │    Format template: unchanged                  │
   │    Silence rule: present                       │
   │    Gating rules: present                       │
   │                                                │
   │  Verdict: ✅ ACCEPT                            │
   │  Baseline updated.                             │
   └────────────────────────────────────────────────┘

4. If REJECT, backup is automatically restored:

   │  Verdict: 🔴 REJECT                           │
   │  Reason: HARD assert failure in CT1            │
   │  Restored skill/SKILL.md from backup.          │
```

---

## What the decision engine does NOT do

- **No machine learning.** No gradient descent on test scores.
- **No auto-modification.** It never edits the skill file — it only gates changes.
- **No optimization loops.** It runs once per invocation. No iterative refinement.
- **No network access.** Everything is local files and Python stdlib.
- **No trust escalation.** Passing N times doesn't lower the bar for N+1.

Each invocation is independent. The decision engine is stateless except for the baseline file.

---

## Dogfooding — using the skill on its own development

The skill should analyze its own development sessions. Without this, the system is incomplete — it validates changes but never *suggests* them.

### The loop

```text
1. Developer works on orbit-engine (edits skill, tests, infra)
2. At any point, apply the skill:
   "Before answering, apply orbit-engine"
3. Skill reads conversation history, detects waste patterns
4. Skill outputs DIAGNOSIS / ACTIONS / DO NOT DO NOW
5. Developer acts on recommendations (or not)
6. Changes are validated:
   $ python3 tests/evolve.py skill/SKILL.md --origin skill-suggested
7. Decision engine approves or rejects
8. Evidence log records: verdict + origin=skill-suggested
```

### Origin tracking

The `--origin` flag marks who proposed the change:

| Origin | Meaning |
| --- | --- |
| `manual` | Developer edited the skill directly (default) |
| `skill-suggested` | orbit-engine diagnosed a pattern and recommended the change |
| `automated` | CI, script, or batch process made the change |

This lets you compare acceptance rates by origin — are skill-suggested changes better?

```bash
# Run after a skill-suggested change
python3 tests/evolve.py skill/SKILL.md --origin skill-suggested

# Review evidence by origin
grep '"origin"' tests/evidence_log.jsonl | python3 -m json.tool
```

### What this proves

When the evidence log contains entries with `"origin": "skill-suggested"` and `"verdict": "ACCEPT"`, it means:

1. The skill detected a real pattern in its own development
2. The recommendation led to a concrete change
3. The change passed all 3 gates (validation, feedback, safety)
4. The system is eating its own dog food — and the food is good

---

## Files

| File | Role |
| --- | --- |
| `SELF-EVOLUTION.md` | This document — design and usage guide |
| `tests/evolve.py` | Orchestrator — backup, validate, decide, commit |
| `tests/decision_engine.py` | Gate logic — pure function, no state, no learning |
| `tests/.baseline.json` | Snapshot of last accepted scores |
| `tests/evidence_log.jsonl` | Append-only decision history with origin tracking |
| `skill/SKILL.md.bak` | Automatic backup (created during evolution cycle) |
