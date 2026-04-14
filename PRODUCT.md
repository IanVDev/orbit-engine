# ORBIT — Product Definition

---

## Positioning

**ORBIT cuts the waste in AI coding sessions by detecting the patterns that make them expensive — and telling you exactly what to change, before you overspend.**

---

## Who it's for

Developers who use AI coding assistants (Claude Code, GPT, Gemini) daily and want to stop burning tokens on rework, vague prompts, and bloated sessions.

Specifically:

- **Solo developers** who pay per-token and feel the cost directly.
- **Team leads** who want consistent, efficient AI usage across their team without policing every session.
- **Agencies and consultancies** billing clients for AI-assisted development and needing to justify costs.

Not for: casual users who open AI once a week for a quick question.

---

## The problem

AI coding assistants charge based on tokens. Tokens are cumulative — every message re-reads the entire conversation history. At message 30, you're paying for all 29 previous messages plus the new one.

Most developers don't know which habits cause this. The result:

- A session that should cost $2 costs $18.
- A 20-minute task takes 45 minutes because of rework loops.
- Generated code is 4x longer than needed because the prompt lacked constraints.

The patterns that cause this are mechanical and repeatable: vague prompts, missing planning steps, not clearing context, correcting the AI 5 times instead of scoping the task once. These patterns are invisible to the developer but obvious in the session data.

There is no tool that watches the session in real time and tells you what to fix. ORBIT does that.

---

## How it works

1. **Install once.** Drop a file into your AI coding assistant. Takes 30 seconds.

2. **Work normally.** ORBIT watches the conversation as it happens. It reads the actual session history — it doesn't guess or estimate.

3. **Get a diagnosis when it matters.** When ORBIT detects a waste pattern, it tells you:
   - What it found (e.g., "3 correction cycles on the same file").
   - How risky it is (low / medium / high / critical).
   - Exactly what to do (specific commands, not generic advice).
   - What NOT to do right now (to avoid making it worse).

4. **Stay silent when everything is fine.** No output means no waste detected. ORBIT doesn't nag.

That's it. No dashboard, no setup, no configuration. It reads the conversation, detects waste, and tells you what to fix.

---

## What ORBIT detects

| Pattern | What it means | Typical cost |
| --- | --- | --- |
| Correction chains | You're fixing the AI's output 3+ times in a row | 2–5x token waste |
| Repeated edits | Same file edited 3+ times — task wasn't scoped | 3–4x token waste |
| Unsolicited long output | AI generated 200 lines when you asked for a fix | Bloated context for every future message |
| Exploratory reading | AI read 5+ files with no plan | Fills context with irrelevant content |
| Weak prompts | Complex task with zero constraints | Speculative output, guaranteed rework |
| Large pastes | Code dumped into chat instead of referenced | Permanent context bloat |

---

## Real numbers

Same task (data ingestion service, 6 features), same requirements:

| | Without ORBIT | With ORBIT | Difference |
| --- | --- | --- | --- |
| Lines generated | 812 | 169 | **−79%** |
| Tokens consumed | ~6,059 | ~1,051 | **−83%** |
| Rework cycles | 3 | 0 | **−100%** |
| Unnecessary classes | 6 | 0 | **−6** |

---

## FREE vs PRO

### ORBIT Free

Everything you need for personal use:

- Full waste pattern detection (all 6 patterns)
- Real-time diagnosis with risk levels
- Specific action recommendations
- Works with Claude Code, GPT, Gemini, and any AI assistant
- Silent when the session is healthy
- No account, no login, no tracking

**Cost: $0, forever.**

### ORBIT Pro

For developers and teams who want to measure and improve over time:

- Everything in Free
- **Session history**: see patterns across sessions, not just the current one
- **Impact tracking**: before/after metrics that prove the tool is working
- **Team dashboard**: aggregate waste patterns across your team's sessions
- **Evolution engine**: the skill improves itself based on your real usage data, validated through automated quality gates before any change ships
- **Evidence log**: append-only audit trail of every decision the system makes — nothing is hidden

**Cost: [pricing TBD]**

---

## Value proposition

ORBIT saves you money and time on every AI coding session by catching the specific habits that waste tokens — before you pay for them.

It's not a linter. It's not a dashboard. It doesn't require you to change how you work. It watches what's happening and tells you the minimum action that fixes the waste.

The difference between a $2 session and an $18 session is usually 2–3 decisions made in the first 5 minutes. ORBIT catches those decisions.

---

## What ORBIT is not

- **Not a code quality tool.** It doesn't review your code. It reviews your session behavior.
- **Not a prompt library.** It doesn't write prompts for you. It tells you when your prompt is missing constraints.
- **Not a cost calculator.** It doesn't count tokens. It detects the patterns that make token counts high.
- **Not an automation tool.** It never executes commands. It only recommends.

---

## One-line summary

ORBIT watches your AI coding session and tells you exactly which habits are costing you tokens — before you waste them.
