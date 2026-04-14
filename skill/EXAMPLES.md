# orbit-engine: Examples

Real output examples showing the new observable-pattern detection.

---

## Scenario 1 — Correction chain after vague prompt

**What happened in the conversation:**
- User: "Refactor the auth module"
- Claude: rewrote 3 files, added OAuth2, changed database schema
- User: "No, just extract the middleware"
- User: "Don't touch the schema"
- User: "Revert the routes file"

**orbit-engine output:**

```text
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
```

---

## Scenario 2 — Unsolicited long response

**What happened in the conversation:**
- User: "Fix the type error on line 42 of utils.ts"
- Claude: generated 180 lines — rewrote the entire file, added error handling, refactored interfaces, added JSDoc

**orbit-engine output:**

```text
DIAGNOSIS
- Response far exceeded the request — user asked for a one-line fix, got a full rewrite
Risk: medium

ACTIONS
1. Restate with constraint — "Fix only the type error on line 42, change nothing else"
2. Use @file:utils.ts:calculateTotal to reference just the function, not the whole file

DO NOT DO NOW
- Accept the full rewrite — it changes behavior beyond what was requested
```

---

## Scenario 3 — Complex first message with no constraints

**What happened in the conversation:**
- User's first message: "I need to migrate 40 Prisma models to a new schema with different relations"
- No file targets, no priority order, no constraints

**orbit-engine output:**

```text
DIAGNOSIS
- Large task with no scope constraints — high speculation risk
Risk: medium

ACTIONS
1. Shift+Tab (Plan Mode) — map dependencies and define migration order before touching any file
2. Define done — which models are critical? What does "migrated" mean for each?

DO NOT DO NOW
- Start migrating without a plan — generates speculative code that will need rework
```

---

## Scenario 4 — Exploratory reading without direction

**What happened in the conversation:**
- User: "Understand this codebase"
- Claude: read 12 files across 4 directories, generated a 300-line summary
- Most files read were not referenced in follow-up messages

**orbit-engine output:**

```text
DIAGNOSIS
- 12 files read in one turn without a specific search goal
- Large summary generated — most content unused in subsequent messages
Risk: high — address before continuing

ACTIONS
1. Add boundary — "Focus on src/auth/ only" or "I need to understand the payment flow specifically"
2. /compact "preserve summary of auth module only" — discard unrelated file contents

DO NOT DO NOW
- Ask to "understand" another broad area — compounds the context with more unused content
```

---

## Scenario 5 — Repeated edits to the same file

**What happened in the conversation:**
- Claude edited `api/routes.ts` in turn 3, turn 5, turn 8, and turn 11
- Each edit was a correction or addition to the previous edit
- Other files were edited once and left alone

**orbit-engine output:**

```text
DIAGNOSIS
- api/routes.ts edited 4 times across the conversation — rework pattern
- Each re-edit re-processes all previous context for the same target
Risk: critical

ACTIONS
⚠️ 1. /compact "preserve final state of api/routes.ts and current task requirements" — collapse the rework history
2. Break remaining work into batches — finish routes.ts completely before moving to the next file
3. Define done for routes.ts — what does "finished" look like?

DO NOT DO NOW
- Edit routes.ts again without first confirming the final requirements
```

---

## Scenario 6 — Large file pasted instead of referenced

**What happened in the conversation:**
- User pasted 400 lines of a controller directly into the message
- Only 1 function in that controller was relevant to the question

**orbit-engine output:**

```text
DIAGNOSIS
- Large code block pasted (full controller) when only one function was relevant
Risk: low

No action required. Something to keep in mind.
```

Next time, use `@file:controller.ts:handlePayment` to reference just what's needed.

---

## Scenario 7 — Healthy session (silence)

**What happened in the conversation:**
- Short session, focused task, clear prompts, no rework

The skill produces **no output**. Complete silence.

Silence = healthy. Nothing to optimize.

---

## Scenario 8 — Explicit trigger on a healthy session

**What happened:** User types "analyze cost" on a short, focused session.

```text
Session looks healthy. No action needed.
```

One line. Move on.
