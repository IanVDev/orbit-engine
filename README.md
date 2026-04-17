# orbit-engine

**Your Claude Code sessions cost more than they should. Orbit Engine shows you exactly where — and what to fix.**

A Claude Code skill that detects waste patterns in your session and outputs the exact commands to eliminate them.

[Get started in 2 min](ONBOARDING.md) · [Tutorial](TUTORIAL.md) · [Usage](#usage) · [See output](#what-it-outputs)

---

## The problem

Token cost in Claude Code isn't per-message. It's cumulative.

Every message re-reads the entire conversation history. At message 30, you're paying for all 29 previous messages plus the new one. Skip a planning step on a complex task, send vague prompts that cause rework, and a session that should cost $2 costs $18.

The patterns that cause this are mechanical and detectable. This skill detects them and tells you what to do.

---

## What you actually get

Orbit Engine ships as two layers. Most users only need the first.

| | **FREE — the skill** | **PRO — skill + measurement infra** |
| --- | --- | --- |
| What it is | A Claude Code skill (`.md` files) that activates during your session and outputs `DIAGNOSIS → ACTIONS → DO NOT DO NOW`. | The skill plus a Go backend (tracking server, PromQL gateway, Prometheus, Grafana) that records real token spend over time. |
| What it needs | Claude Code. Nothing else. | Docker + Prometheus/Grafana on a host you control. |
| What it answers | *"Is my current session wasting tokens, and what should I do right now?"* | *"How much did Orbit actually save me across N sessions? Where do patterns repeat? Is the skill still working in week 6?"* |
| Who it's for | Every Claude Code user. | Teams, power users, anyone who needs longitudinal evidence (not just in-session advice). |

If you are not sure which you want, you want FREE. The skill works standalone — the Go infrastructure in `tracking/` is *only* for measurement and is not required for the skill to do its job.

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

## Usage

### Claude Code

Download `orbit-engine.skill` and install in your skills folder.

Or drag the `skill/` folder directly into the Claude Code interface.

**First Run** — after installing, ask:

```text
How efficient is this?
```

- ✅ See `DIAGNOSIS` → skill is active.
- ❌ No `DIAGNOSIS` → paste the exact prompt again. If it still doesn't appear, reinstall using drag-and-drop.

After the first run, the skill activates automatically on complex tasks and long sessions.

Full onboarding (30 seconds): [ONBOARDING.md](ONBOARDING.md)

### Any AI (GPT, Gemini, etc.)

Copy and paste [`orbit-engine.prompt.md`](orbit-engine.prompt.md) at the start of your session.

Then use it normally — Orbit Engine will activate when it detects inefficiency in your conversation.

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
| Correction chain | 3+ short follow-ups correcting your output |
| Rework pattern | same file edited 3+ times in the conversation |
| Weak prompt | complex task with no constraints, scope, or boundaries |
| Complex task | "refactor...", "migration...", "redesign...", "implement..." |

> **Tip:** On a fresh session with no history, auto-triggers may not fire. Use `analyze cost` explicitly or the guaranteed phrase above.

---

## Files

```
orbit-engine/
├── orbit-engine.skill        # Installable skill package (ZIP)
├── orbit-engine.prompt.md    # Universal prompt (GPT, Gemini, etc.)
├── skill/                    # The skill source files
│   ├── SKILL.md              # Core logic — install this
│   ├── EXAMPLES.md           # Output examples
│   ├── ONBOARDING.md         # First-time setup (inside skill)
│   └── QUICK-START.md        # Quick reference (inside skill)
├── README.md
├── ONBOARDING.md             # First-time setup (2 min)
├── QUICK-START.md            # Quick reference
├── TUTORIAL.md               # Hands-on tutorial
├── GUIDE.md                  # Full reference guide
├── VALIDATION.md             # Test coverage (contributors)
├── FEEDBACK.md               # Feedback collection system (contributors)
├── SELF-EVOLUTION.md         # Self-evolution cycle (contributors)
├── CONTRIBUTING.md
└── LICENSE                   # MIT
```

> **To install:** drag the entire `skill/` folder into Claude Code, or drag individual `.md` files from inside it.

---

## Contributing

Pull requests welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

---

## License

MIT © 2026 · See [LICENSE](LICENSE).
