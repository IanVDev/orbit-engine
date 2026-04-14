# Feedback Collection

> **⚙️ Advanced** — This document is for contributors measuring real-world skill effectiveness.
> If you just want to use the skill, start with [QUICK-START.md](QUICK-START.md).

How to measure whether orbit-engine outputs are actually useful — not just structurally correct.

---

## The problem this solves

The [validation system](VALIDATION.md) proves the skill produces structurally correct output. But structural correctness ≠ usefulness. A diagnosis can pass all 18 tests and still be ignored by every user who reads it.

This feedback system answers three questions:

1. **Did the user act?** — Time-to-action after a diagnosis
2. **Was it clear?** — Did the user need to ask for clarification?
3. **Did it help?** — Did session behavior improve after acting?

---

## Design constraints

- **No infrastructure.** No databases, no analytics services, no external APIs.
- **No runtime changes to the skill.** The skill itself stays unchanged — feedback is collected by analyzing conversation logs after the fact.
- **Python stdlib only.** Consistent with the test suite.
- **Opt-in.** Users choose to export and analyze their sessions. Nothing is collected automatically.

---

## Metrics

### Primary metrics (per activation)

| Metric | What it measures | How to compute | Good | Bad |
| --- | --- | --- | --- | --- |
| **Time-to-action** | Turns between diagnosis and user acting on it | Count turns from diagnosis to first user message that references an action item | 0–1 turns | 3+ turns |
| **Action adoption rate** | Fraction of recommended actions the user executed | Match action items against subsequent user messages and tool calls | ≥ 0.5 | < 0.25 |
| **Clarification requests** | User asked "what do you mean?" after diagnosis | Count user messages with question marks or confusion signals within 2 turns | 0 | ≥ 1 |
| **Silence after activation** | User ignored the diagnosis entirely | No reference to any action item in next 3 turns | — | true |

### Impact metrics (per session)

| Metric | What it measures | How to compute | Good | Bad |
| --- | --- | --- | --- | --- |
| **Post-action rework** | Edits to same file after user acted on diagnosis | Count repeated edits to same file in 5 turns after action | 0–1 | 3+ |
| **Session length delta** | Did the session get shorter after adoption? | Compare total turns before vs. after first diagnosis | shorter | same or longer |
| **Pattern recurrence** | Same pattern detected again after user acted | Check if same pattern triggers again in later turns | 0 | ≥ 1 |

### Correlation metric

| Metric | What it measures | How to compute |
| --- | --- | --- |
| **Validation score ↔ adoption** | Do higher-scoring outputs get adopted more? | Group sessions by validation score quartile, compare mean adoption rate |

---

## Log schema

Each activation produces one log entry. Format: JSONL (one JSON object per line).

```jsonl
{"session_id":"s_001","activation_id":"a_001","timestamp":"2026-04-14T10:30:00Z","risk":"high","pattern":"weak_prompt","diagnosis_items":2,"action_items":3,"actions_adopted":2,"time_to_action":1,"clarification_requests":0,"silence":false,"post_action_rework":0,"pattern_recurrence":false,"validation_score":0.95}
```

### Field reference

| Field | Type | Description |
| --- | --- | --- |
| `session_id` | string | Unique session identifier (user-provided or auto-generated) |
| `activation_id` | string | Unique per-activation within a session |
| `timestamp` | string | ISO 8601 timestamp of the activation |
| `risk` | string | Risk level: low, medium, high, critical |
| `pattern` | string | Primary pattern detected (from SKILL.md observable patterns) |
| `diagnosis_items` | int | Number of bullet items in DIAGNOSIS |
| `action_items` | int | Number of items in ACTIONS |
| `actions_adopted` | int | How many actions the user executed |
| `time_to_action` | int \| null | Turns until user's first action-related message. null if silence. |
| `clarification_requests` | int | User confusion signals within 2 turns of diagnosis |
| `silence` | bool | User ignored the diagnosis entirely |
| `post_action_rework` | int | Repeated edits to same file after acting |
| `pattern_recurrence` | bool | Same pattern fired again later in session |
| `validation_score` | float \| null | Score from validator (0.0–1.0) if available |

---

## How to collect

### Step 1 — Export a session

From Claude Code, copy or export the full conversation text into a file:

```bash
# Example: save conversation as plain text
pbpaste > sessions/session_001.txt

# Or save multiple sessions
ls sessions/
  session_001.txt
  session_002.txt
  session_003.txt
```

The collector expects plain text with turn markers. Each user message starts on a new line after an assistant response. The format is flexible — the parser uses heuristics to detect turn boundaries.

### Step 2 — Run the collector

```bash
python3 tests/feedback_collector.py sessions/session_001.txt
python3 tests/feedback_collector.py sessions/           # all .txt files in directory
python3 tests/feedback_collector.py sessions/ --out feedback.jsonl
```

Output: one `.jsonl` file with one entry per activation found.

### Step 3 — Generate a report

```bash
python3 tests/feedback_report.py feedback.jsonl
python3 tests/feedback_report.py feedback.jsonl --with-validation  # correlate with scores
```

Output: summary statistics printed to stdout.

---

## Integration with validation scores

The feedback system connects to the existing validation system at one point: the `validation_score` field.

### How correlation works

```text
┌─────────────────────┐     ┌──────────────────────┐
│  Validation system   │     │  Feedback system      │
│  (tests/validator)   │     │  (tests/feedback_*)   │
│                      │     │                       │
│  Golden outputs      │     │  Real session logs    │
│  → HARD/SOFT score   │     │  → adoption metrics   │
│  → 0.0–1.0 per test  │     │  → 0.0–1.0 per       │
│                      │     │    activation          │
└──────────┬───────────┘     └──────────┬────────────┘
           │                            │
           └──────────┬─────────────────┘
                      │
           ┌──────────▼───────────┐
           │  Correlation report  │
           │                      │
           │  "Outputs that score │
           │   ≥95% in validation │
           │   have 72% adoption  │
           │   rate vs. 38% for   │
           │   outputs <80%"      │
           └──────────────────────┘
```

### What to look for

| Finding | What it means | Action |
| --- | --- | --- |
| High validation score + high adoption | Tests predict real quality | Keep current test calibration |
| High validation score + low adoption | Tests measure the wrong thing | Recalibrate SOFT asserts |
| Low validation score + high adoption | Users are forgiving, tests are strict | Relax thresholds or reclassify SOFT→informational |
| Low validation score + low adoption | Both systems agree: output is weak | Investigate the skill logic |

---

## Adoption signals

The collector uses keyword matching to detect whether a user acted on a recommended action. These are the signals it looks for:

### Command signals

| Action recommended | User signal detected |
| --- | --- |
| `/clear` | User message contains `/clear` |
| `/compact` | User message contains `/compact` |
| `Shift+Tab` / Plan Mode | User mentions "plan", "plan mode", or starts structured planning |
| `@file` reference | User uses `@file` or `@` reference in next message |

### Prompt improvement signals

| Action recommended | User signal detected |
| --- | --- |
| Restate with constraints | User message is longer than original and contains boundary words ("only", "don't touch", "just", "scope") |
| Define done | User message contains acceptance criteria language ("done when", "finished means", "success =") |
| Break into subtasks | User message contains numbered steps or explicit "step 1", "first", "then" |

### Non-adoption signals

| Signal | Interpretation |
| --- | --- |
| User immediately asks next unrelated question | Ignored |
| User says "ok" / "thanks" with no follow-up action | Acknowledged but not acted on |
| User asks "what do you mean?" or "why?" | Clarification needed — diagnosis was unclear |

---

## Privacy

- Sessions are local files. Nothing leaves the machine.
- The JSONL output contains only metrics — no raw conversation text.
- Session IDs are arbitrary strings. Use pseudonyms or hashes.
- The collector never reads files outside the `sessions/` directory.

---

## Files

| File | Role |
| --- | --- |
| `FEEDBACK.md` | This document — system design and usage guide |
| `tests/feedback_collector.py` | Parses session logs, computes metrics, writes JSONL |
| `tests/feedback_report.py` | Reads JSONL, prints summary statistics and correlation |
| `sessions/` | User-created directory for exported session text files |

---

## Limitations

1. **Heuristic turn detection.** The collector guesses turn boundaries from text patterns. Complex sessions with nested code blocks may confuse the parser.
2. **No intent detection.** The collector can't tell if a user ran `/compact` because the skill recommended it or because they were going to anyway.
3. **Small sample bias.** With <10 sessions, correlation statistics are meaningless. The report warns about this.
4. **Adoption ≠ quality.** A user might adopt an action and still get a bad outcome. The system measures behavior change, not outcome quality.

These are acceptable trade-offs for a zero-infrastructure system. The goal is directional signal, not scientific measurement.
