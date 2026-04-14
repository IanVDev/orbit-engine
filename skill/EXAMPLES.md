# orbit-engine: Examples

Real output examples for common scenarios.

---

## Scenario 1 — Long session before a complex task

**Context:** 25+ messages in, no /clear used, about to start a refactor.

```text
DIAGNOSIS
- Long session without /clear or /compact
- Complex task started without Plan Mode
Risk: high

ACTIONS
1. Shift+Tab (Plan Mode) — map scope before spending tokens
2. /compact "preserve architecture decisions" — clean history safely
3. /clear after finishing — reset cost between tasks

DO NOT DO NOW
- Start coding without a plan — causes rework and invisible cost
```

---

## Scenario 2 — Tokens running low

**Context:** User mentions things are "getting slow" or "near the limit."

```text
DIAGNOSIS
- Session showing signs of token pressure
- Multiple messages accumulated without compaction
Risk: critical

ACTIONS
1. /compact "preserve completed feature" — free up space now
2. /mcp → disconnect non-essential MCPs — remove invisible overhead
3. Finish current task in as few messages as possible

DO NOT DO NOW
- Start new tasks or paste large files — no room for exploration
```

---

## Scenario 3 — Complex migration without planning

**Context:** User asks to migrate 40 database models without Plan Mode.

```text
DIAGNOSIS
- Large task started without planning step
- High rework risk without dependency mapping
Risk: high

ACTIONS
1. Shift+Tab (Plan Mode) — map dependencies and constraints first
2. @file:model instead of pasting full files — precision without overhead
3. /compact "preserve plan + decisions" — prepare for clean execution

DO NOT DO NOW
- Dump all models at once — overwhelms context without structure
```

---

## Scenario 4 — Idle MCPs wasting context

**Context:** 3 MCPs connected (Figma, Slack, GitHub), only GitHub in use.

```text
DIAGNOSIS
- Connected MCPs not being used in recent messages
- Each idle MCP adds overhead to every message
Risk: medium

ACTIONS
1. /mcp → review connected integrations
2. Disconnect Figma and Slack if not needed for current task

DO NOT DO NOW
- Disconnect MCPs you're about to use — check first, then disconnect
```

---

## Scenario 5 — Large file pasted instead of referenced

**Context:** User pasted an 800-line controller into chat.

```text
DIAGNOSIS
- Large file pasted in full instead of using precise reference
Risk: medium

ACTIONS
1. Use @file:functionName to reference only what's needed
2. /compact "preserve current task context" — reduce what just grew

DO NOT DO NOW
- Paste another large file — compounds the overhead
```

---

## Scenario 6 — Healthy session (no output)

**Context:** Short session, simple task, no waste detected.

The skill stays **completely silent**. No DIAGNOSIS, no output.

Silence = healthy. Nothing to fix.

---

## Scenario 7 — Explicit trigger on a healthy session

**Context:** User types "analyze cost" but the session is short and focused.

```text
Session looks healthy. No action needed.
```

One line. No drama. Move on.
