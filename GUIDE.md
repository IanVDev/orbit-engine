# orbit-engine: Guide

> **New here?** Start with [ONBOARDING.md](ONBOARDING.md) first. This document is the reference manual.

---

## Activation signals (full list)

The skill fires when it detects at least one signal below. Multiple signals increase the risk level.

| Signal | Threshold | Risk contribution |
| --- | --- | --- |
| Long session | >15 messages without `/clear` | Medium |
| Complex task keyword | "refactor", "migration", "redesign", "rewrite", "implement" | Medium |
| Token pressure | "getting slow", "tokens running out", "near limit" | High |
| Multi-agent pattern | 2+ agents sharing context | High |
| No Plan Mode on complex task | Complex keyword + no Shift+Tab | High |
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

### Scenario 1: Before a large task

```text
YOU: "analyze cost"

orbit-engine:
DIAGNOSIS
- Long session without /clear (25 messages)
- Complex task started without planning
Risk: high

ACTIONS
1. Plan Mode (Shift+Tab) — map scope before spending tokens
2. /compact "preserve architecture decisions" — clean history safely
3. /clear after finishing — reset cost between tasks

DO NOT DO NOW
- Start coding without a plan — causes rework and invisible cost
```

### Scenario 2: Tokens near limit

```text
YOU: "analyze cost"

orbit-engine:
DIAGNOSIS
- Context in critical state
Risk: critical

ACTIONS
1. /compact "preserve completed feature" — free up space now
2. /mcp → disconnect non-essential MCPs — remove invisible overhead
3. Finish in 1 message

DO NOT DO NOW
- Start new tasks or paste large files
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

## The 10 strategies

The skill selects from these automatically:

### Session & context

- `/clear` between different tasks
- `/compact [instruction]` to clean without losing progress
- `/mcp` → disconnect idle MCPs
- Group prompts into one message; edit instead of follow-ups

### Input precision

- Plan Mode before complex tasks
- `@file:function` instead of the whole file
- Watch Claude work to interrupt loops immediately

### Configuration

- `CLAUDE.md` as an index only (<200 lines), move workflows to separate Skills
- Sonnet as default, Haiku for auxiliary tasks, Opus for critical architecture only
- Avoid multi-agent setups because each instance pays the full context cost

---

## FAQ

**Does the skill activate on its own or do I need to ask?**
It activates automatically when it detects the signals. You can also trigger it explicitly: type "analyze cost" in your session.

**Will it remove context without warning?**
No. It always recommends `/compact` and never executes it. You decide.

**What if the recommendation doesn't make sense?**
Ignore it. The skill is a guidance tool, not a rule. Use your judgment.

**What's the real savings range?**

- Long session: 30–50%
- Before refactor or migration: 50–70%
- Avoiding rework: up to 80%

**Does it work on Claude.ai?**
The skill file requires Claude Code. The 10 strategies can be applied manually anywhere.

---

## First steps

1. Install by dragging the `.skill` file into Claude Code
2. Open a long session and let the skill activate naturally
3. Try the commands: `/clear`, `/compact`, `/mcp`, Plan Mode
4. After 5–10 uses, the patterns become automatic

---

**v2.0** · Output reduction: 78–89% · Token savings: 30–70%
