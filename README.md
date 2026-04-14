# orbit-engine

**Your Claude Code sessions cost more than they should. Orbit Engine shows you exactly where — and what to fix.**

A Claude Code skill that detects waste patterns in your session and outputs the exact commands to eliminate them.

[Get started in 2 min](ONBOARDING.md) · [Tutorial](TUTORIAL.md) · [Install](#install) · [See output](#what-it-outputs)

---

## The problem

Token cost in Claude Code isn't per-message. It's cumulative.

Every message re-reads the entire conversation history. At message 30, you're paying for all 29 previous messages plus the new one. Add 3 idle MCPs injecting tool definitions, skip a planning step on a complex task, and a session that should cost $2 costs $18.

The patterns that cause this are mechanical and detectable. This skill detects them and tells you what to do.

---

## Before / After

Real scenario: data ingestion service, no planning step, context not cleared.

| | Without skill | With skill | Δ |
| --- | --- | --- | --- |
| Lines generated | 812 | 169 | **-79%** |
| Tokens consumed | ~6,059 | ~1,051 | **-83%** |
| Unnecessary classes | 6 | 0 | **-6** |
| Rework cycles | 3 | 0 | **-100%** |

Same 6 features. Same requirements. ~83% fewer tokens.

<details>
<summary>See the actual skill output</summary>

```
DIAGNOSIS
- Complex task started without Plan Mode
- Context not cleared from previous session
Risk: high

ACTIONS
1. Shift+Tab (Plan Mode) — scope first, then execute
2. /compact "preserve schema decisions" — clear safely
3. @file:schema.ts instead of full file dumps

DO NOT DO NOW
- /clear before planning — would lose current context
```

</details>

---

## Install

**Drag `orbit-engine.skill` into Claude Code.** Recommended method.

Alternatively, type in Claude Code (not in your terminal):

```
/install orbit-engine.skill
```

> If `/install` returns an error, use drag-and-drop — it always works. No config. No restart.

**First Run** — after installing, ask:

```text
How efficient is this?
```

- ✅ See `DIAGNOSIS` → skill is active.
- ❌ No `DIAGNOSIS` → paste the exact prompt again. If it still doesn't appear, reinstall using drag-and-drop.

After the first run, the skill activates automatically on complex tasks and long sessions.

Full onboarding (30 seconds): [ONBOARDING.md](ONBOARDING.md)

---

## What it outputs

Fixed format. Always recommends, never executes.

```
DIAGNOSIS
- [detected waste pattern]
- [detected waste pattern]
Risk: [low / medium / high / critical]

ACTIONS
1. [exact command] — [why it helps here]
2. [exact command] — [why it helps here]

DO NOT DO NOW
- [what to avoid and why]
```

The skill stays silent when the session is healthy — no output means no waste detected.

---

## How it activates

| Trigger | Example |
| --- | --- |
| Explicit | type `analyze cost`, `analyze-cost`, or `/analyze-cost` |
| Guaranteed | `Before answering, apply orbit-engine. Then: [your task]` |
| Long session | >15 messages without `/clear` |
| Complex task | "refactor...", "migration...", "redesign...", "implement..." |
| Token pressure | "getting slow", "tokens running out", "near limit" |

> **Tip:** On a fresh session with no history, auto-triggers may not fire. Use `analyze cost` explicitly or the guaranteed phrase above.

---

## Files

```
orbit-engine/
├── orbit-engine.skill       # Install this
├── README.md
├── ONBOARDING.md            # First-time setup (2 min)
├── QUICK-START.md           # Quick reference
├── TUTORIAL.md              # Hands-on tutorial
├── GUIDE.md                 # Full reference guide
├── VALIDATION.md            # Test coverage (contributors)
├── CONTRIBUTING.md
└── LICENSE                  # MIT
```

---

## Contributing

Pull requests welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

---

## License

MIT © 2026 · See [LICENSE](LICENSE).
