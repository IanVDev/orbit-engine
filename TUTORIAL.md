# orbit-engine — Hands-on Tutorial

Total time: ~2 minutes.
You won't just read. You'll test it.

---

## Prerequisite

**Drag `orbit-engine.skill` into the Claude Code interface.** That's the recommended method.

Alternatively, type in Claude Code (not in your terminal):

```text
/install orbit-engine.skill
```

> If `/install` returns an error, use drag-and-drop — it always works.

If already installed, continue.

---

## Lesson 1: First Run

The skill activates automatically on complex tasks — but that's probabilistic.
This lesson is deterministic. It always works.

### Step 1 — Activate

Open Claude Code and ask:

```text
How efficient is this?
```

---

### Step 2 — Verify

You should see:

```text
DIAGNOSIS
```

---

### ❌ If it does NOT appear

Paste this instead:

```text
Before answering, apply orbit-engine. Then: how efficient is this?
```

⛔ Do not continue until you see **DIAGNOSIS at least once.**
If it doesn't trigger, paste the exact prompt again — repetition works.
If it still doesn't appear, the skill is not installed — go back to the prerequisite step.

---

### Step 3 — Switch to Auto Mode

You only need the prompt above once.

After the first run, the skill activates automatically when it detects:

- Session over 15 messages without `/clear`
- Complex task keywords ("refactor", "migration", "redesign")
- Token pressure ("getting slow", "near limit")

You can also trigger it anytime with: `analyze cost`

---

### Real output example

```text
DIAGNOSIS
- Complex task started without Plan Mode
- History not cleared from previous session
Risk: high

ACTIONS
1. Shift+Tab (Plan Mode) — map scope before executing
2. /compact "preserve auth decisions" — clean context safely
3. @file:auth.ts instead of pasting the entire file

DO NOT DO NOW
- Start coding without a plan
```

---

### What this means

If you saw this:

→ You had inefficiencies you didn't notice
→ Claude was working without a strategy
→ This causes rework and invisible cost

---

## Lesson 2: Applying the first improvement

Now run the first recommended action.

Example:

```text
Shift+Tab
```

And type:

```text
Map auth module dependencies before refactoring
```

---

### Expected result

- Claude stops generating speculatively
- It starts planning
- The output gets smaller and more focused

If the output is still large:

→ You didn't apply the ACTIONS correctly

---

## Lesson 3: Long sessions (where you lose the most tokens)

### The problem

Every message re-reads everything that came before.

```text
Message 1:  cost = 1×
Message 10: cost = 10×
Message 30: cost = 30×
```

---

### What to do

After ~10 messages, run:

```text
analyze cost
```

---

### Real diagnosis example

```text
DIAGNOSIS
- Context in critical state (>15 messages without /clear)
- Idle MCPs injecting context
Risk: critical

ACTIONS
1. /compact "preserve completed feature"
2. /mcp → disconnect idle integrations
3. Finish in 1 message
```

---

### Simple rule

```text
Session got long → analyze cost
```

---

## Lesson 4: You don't need to memorize commands

The skill always suggests what to do.

But these are the most common:

| Command | What it does |
| --- | --- |
| Shift+Tab | Plan before executing |
| /compact | Reduces context while keeping what matters |
| /clear | Resets the session |
| /mcp | Removes idle integrations |
| @file: | References only what's needed |

---

## Lesson 5: Large tasks (where the real gains are)

Example:

```text
I need to migrate 40 Prisma models to a new schema
```

Then:

```text
analyze cost
```

---

### Correct workflow

```text
1. Shift+Tab (plan)
2. Map dependencies
3. /compact "preserve plan"
4. Execute in small batches
5. /compact between phases
6. /clear when done
```

---

### Typical result

- Less rework
- Smaller, more focused responses
- Significant token savings

---

## Lesson 6: When the skill does NOT activate

This is normal.

It stays silent when:

- Short session
- Simple task
- No detectable waste

---

To force it:

```text
analyze cost
```

---

## Lesson 7: Automatic activation

The skill fires on its own when it detects:

| Situation | Example |
| --- | --- |
| Long session | Many messages without clearing |
| Complex task | "refactor", "migration" |
| Token pressure | "getting slow" |

---

## Your workflow from now on

```text
New task → Shift+Tab
Long session → analyze cost
Follow ACTIONS
/compact between phases
/clear when done
```

---

## What orbit-engine does

→ Shows where you can improve efficiency
→ Tells you exactly what to fix

---

## Quick FAQ

**Does it execute commands on its own?**
No. It only recommends.

**Does it work outside Claude Code?**
No. But the principles work manually in any AI assistant.

**How much does it really save?**
Refactor with plan: -60%. Avoiding rework: up to -80%.
