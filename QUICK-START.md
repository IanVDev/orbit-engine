# orbit-engine: Quick Start

## Step 1 — Install

**Drag `orbit-engine.skill` into the Claude Code interface.** That's it.

Alternatively, type in Claude Code (not in your terminal):

```text
/install orbit-engine.skill
```

> If `/install` returns an error, use the drag-and-drop method above — it always works.

---

## Step 2 — Trigger

Paste this into Claude Code:

```text
create a data ingestion service in TypeScript with Kafka, validation and PostgreSQL
```

The skill activates automatically on complex tasks. On a fresh session with no history, it may not fire — Step 3 covers that.

---

## Step 3 — Verify (mandatory)

Look at the response. One of two things happened:

### ✅ You see `DIAGNOSIS`

```text
DIAGNOSIS
- Complex task started without Plan Mode
...
Risk: high

ACTIONS
1. ...
```

**The skill is active.** Follow the actions. You're done.

### ❌ You don't see `DIAGNOSIS`

The auto-trigger uses heuristics — it may not fire on a fresh session with no history.
That's expected. It's not a sign the skill isn't installed.

Use the **guaranteed prompt** instead:

```text
Before answering, apply orbit-engine. Then: create a data ingestion service in TypeScript with Kafka, validation and PostgreSQL
```

This phrase always activates the skill, regardless of session state.

Check the response:

- **DIAGNOSIS appeared →** skill is active. Auto-trigger just needed context. You're good.
- **Still no DIAGNOSIS →** the skill is not loaded. Go back to Step 1 and reinstall.

> **⛔ Do not continue until you see DIAGNOSIS at least once.**
> The guaranteed prompt above always works if the skill is installed.
> If it still doesn't appear, installation failed — nothing below will work.

---

## Before / After

### Refactor without planning

| | Before | After | Δ |
| --- | --- | --- | --- |
| Lines generated | 812 | 169 | **-79%** |
| Tokens consumed | ~6,059 | ~1,051 | **-83%** |
| Rework cycles | 3 | 0 | **-100%** |
| Unnecessary classes | 6 | 0 | **-6 files** |

### Long session (31 messages)

| | Before | After | Δ |
| --- | --- | --- | --- |
| Token cost | ~12,000+ | ~4,000 | **-67%** |
| Rewrites | 3 | 0 | **-100%** |
| Time wasted | ~40 min | 0 | **saved** |

### 40-model migration

| | Before | After | Δ |
| --- | --- | --- | --- |
| Completion | Tokens exhausted | Structured finish | **✓** |
| Rework | High | Zero | **-100%** |
| Token savings | — | — | **~70%** |

---

## How to trigger

| Trigger | When to use |
| --- | --- |
| `How efficient is this?` | **Recommended** — natural, works anywhere |
| `analyze cost` | Classic trigger — still works |
| `optimize this` | Short and direct |
| `is this optimal?` / `can this be improved?` | Variations — all recognized |
| `Before answering, apply orbit-engine. Then: [task]` | Guaranteed fallback — always fires even on fresh sessions |

The skill also activates automatically on long sessions (>15 msgs), complex task keywords, and token pressure signals.

---

## Commands the skill recommends

| Command | What it does |
| --- | --- |
| `/clear` | Wipes history. Use between different tasks. |
| `/compact [instruction]` | Summarizes history without losing what matters. |
| `/mcp` | Lists MCPs to disconnect idle ones. |
| `Shift+Tab` | Plan Mode. Plan before executing. |
| `@file:function` | Precise reference, no whole-file dumps. |

---

## 3 common situations

**Long session + big task**
→ Plan Mode → /compact → /clear after. Save 50-60%.

**Tokens almost gone**
→ /compact NOW → disconnect MCPs → finish in 1 message.

**Planning a migration**
→ Plan Mode → @file:model → /compact after. Map first, execute clean.

---

## Questions

**"What if the recommendation doesn't make sense?"**
Ignore it. You're in charge.

**"How much does it really save?"**
Refactor with plan: -60%. Avoiding rework: up to -80%.

**"Does it work on Claude.ai?"**
Claude Code only. The 10 strategies work manually anywhere.
