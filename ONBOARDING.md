# orbit-engine

Your sessions can be more efficient than you think.

Every message in Claude Code re-reads the entire history. At message 30, you pay for all 29 before it. Add idle MCPs, skip a planning step, and a $2 session costs $18.

This skill detects that — and tells you exactly what to fix.

---

## Step 1 — Install (10 seconds)

**Drag `orbit-engine.skill` into the Claude Code interface.** That's the recommended method.

Alternatively, type directly in Claude Code (not in your terminal):

```text
/install orbit-engine.skill
```

> ⚠️ If `/install` returns an error or nothing happens, use drag-and-drop — it always works.

No config. No restart.

---

## Step 2 — First Run

Ask this in Claude Code:

```text
How efficient is this?
```

You should see:

```text
DIAGNOSIS
```

### ❌ If you don't see DIAGNOSIS

Paste this instead:

```text
Before answering, apply orbit-engine. Then: how efficient is this?
```

> **⛔ Do not continue until you see DIAGNOSIS at least once.**
> If it doesn't trigger, paste the exact prompt again — repetition works.
> If it still doesn't appear, the skill is not loaded — run `/install orbit-engine.skill` again or use drag-and-drop.

---

## Step 3 — Auto Mode

You only need the prompt above once.

After the first run, the skill activates automatically when it detects:

- Session over 15 messages without `/clear`
- Complex task keywords ("refactor", "migration", "redesign", "implement")
- Token pressure signals ("getting slow", "tokens running out", "near limit")

You can also trigger it explicitly anytime with: `analyze cost`

Silence = healthy session. No waste detected.

---

## What just happened

Without Plan Mode on a complex task, Claude generates speculatively.

**Same task, same 6 features:**

| | Without skill | With skill | Δ |
| --- | --- | --- | --- |
| Lines generated | 812 | 169 | **-79%** |
| Tokens consumed | ~6,059 | ~1,051 | **-83%** |
| Unnecessary classes | 6 | 0 | **-6** |

The skill doesn't change what you build. It changes how much you pay to build it.

---

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| No DIAGNOSIS after Step 2 + `analyze cost` | Reinstall using drag-and-drop (most reliable method) |
| `/install` returns an error | Use drag-and-drop instead — `/install` requires Claude Code interface, not terminal |
| `command not found` | You're in your terminal, not Claude Code |
| Want full reference | [GUIDE.md](GUIDE.md) — all signals, strategies, scenarios |
