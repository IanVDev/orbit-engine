# orbit-engine: Guide

> **New here?** Start with [ONBOARDING.md](ONBOARDING.md) first. This document is the reference manual.

---

## Observable patterns (full list)

The skill reads the conversation history and fires when it detects at least one pattern below. Multiple patterns increase the risk level.

| Pattern | What it looks for | Risk |
| --- | --- | --- |
| Unsolicited long response | Claude's reply far exceeded what the prompt implied | Low–Medium |
| Correction chain | 3+ short follow-ups correcting/redirecting output | Medium–High |
| Repeated edits to same target | Same file/function edited 3+ times in the conversation | High–Critical |
| Exploratory reading without plan | 5+ files read in one turn without a specific search goal | Medium–High |
| Weak prompt | Complex task with no constraints, scope, or boundaries | Medium |
| Large content pasted | >100 lines pasted instead of using @file references | Low |
| Explicit request | `analyze cost` / `analyze-cost` / `/analyze-cost` | Always fires |

It does **not** activate for:

- Simple one-line questions
- Short or casual conversations
- Trivial tasks (single-file edits, quick lookups)

When in doubt, the skill activates rather than staying silent — the cost of not optimizing is higher than a false positive. On a fresh session with no history and no explicit trigger, it may stay silent because there is genuinely nothing to diagnose yet.

---

## Installation

**Drag the `skill/` folder into the Claude Code interface.** That's the most reliable method.

Alternatively, drag `skill/SKILL.md` directly — it always works. No terminal. No restart.

---

## Scenarios

### Scenario 1: Correction chain after vague prompt

```text
YOU: "Refactor the auth module"
(Claude rewrites 3 files, adds OAuth2, changes schema)
YOU: "No, just extract the middleware"
YOU: "Don't touch the schema"
YOU: "Revert the routes file"

orbit-engine:
DIAGNOSIS
- 3 consecutive corrections — prompt lacked constraints
- Same files edited multiple times (auth.ts, routes.ts, schema.ts)
Risk: high — address before continuing

ACTIONS
1. Restate with boundaries — "extract auth middleware into middleware/auth.ts, don't touch routes or schema"
2. Shift+Tab (Plan Mode) — confirm scope before executing
3. /compact "preserve current file state and user corrections"

DO NOT DO NOW
- Continue editing without a clear scope — each correction adds another full pass to context
```

### Scenario 2: Repeated rework on same file

```text
YOU: "analyze cost"

orbit-engine:
DIAGNOSIS
- api/routes.ts edited 4 times across the conversation — rework pattern
- Each re-edit re-processes all previous context for the same target
Risk: critical

ACTIONS
⚠️ 1. /compact "preserve final state of api/routes.ts and current requirements" — collapse the rework history
2. Define done for routes.ts — what does "finished" look like?
3. Finish routes.ts completely before moving to the next file

DO NOT DO NOW
- Edit routes.ts again without first confirming final requirements
```

### Scenario 3: Database migration planning

```text
YOU: "analyze cost"

orbit-engine:
DIAGNOSIS
- Task too complex to improvise (40 models)
- High rework risk without planning
Risk: high

ACTIONS
1. Plan Mode — map dependencies, circular constraints, critical queries
2. @file:model instead of pasting everything — precision without overhead
3. /compact "preserve plan + decisions" — prepare for clean execution

DO NOT DO NOW
- Dump all 40 models at once
- Start without a dependency map
```

---

## Strategies

The skill selects from these automatically:

### Session & context

- `/clear` between different tasks
- `/compact [instruction]` to clean without losing progress
- Group prompts into one message; edit instead of follow-ups

### Prompt quality

- Add constraints before complex tasks (scope, boundaries, definition of done)
- Rewrite vague prompts with specific file targets and acceptance criteria
- Specify what NOT to touch

### Task structure

- Plan Mode (Shift+Tab) before complex tasks
- `@file:function` instead of pasting whole files
- Break large tasks into 2-4 sequential subtasks
- Batch by file/component — finish one target before moving to the next

---

## FAQ

**Does the skill activate on its own or do I need to ask?**
It checks every response automatically. You can also trigger it explicitly: type "analyze cost" in your session.

**Will it remove context without warning?**
No. It always recommends `/compact` and never executes it. You decide.

**What if the recommendation doesn't make sense?**
Ignore it. The skill is a guidance tool, not a rule. Use your judgment.

**What's the real savings range?**

- Avoiding rework from vague prompts: 40–60%
- Before refactor or migration: 50–70%
- Collapsing correction chains: up to 80%

**Does it work on Claude.ai?**
The skill file requires Claude Code. The strategies can be applied manually anywhere.

---

## First steps

1. Install by dragging the `skill/` folder into Claude Code
2. Start a complex task and let the skill activate naturally
3. Try the commands: `/clear`, `/compact`, Plan Mode
4. After 5–10 uses, the patterns become automatic

---

**v3.0** · Observable pattern detection · Risk-differentiated behavior
