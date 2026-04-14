# orbit-engine: Onboarding

## Install (10 seconds)

**Drag the `skill/` folder into the Claude Code interface.**

Alternatively, drag `skill/SKILL.md` directly — it always works.

No config. No restart.

---

## First Run

Ask this in Claude Code:

```text
How efficient is this?
```

You should see `DIAGNOSIS` in the response.

If you don't, paste this instead:

```text
Before answering, apply orbit-engine. Then: how efficient is this?
```

---

## After that

The skill activates automatically when it detects:

- Correction chains (multiple short follow-ups fixing your output)
- Rework patterns (same file edited repeatedly)
- Weak prompts (complex tasks with no constraints)
- Unsolicited long responses

You can also trigger it anytime: `analyze cost`

Silence = healthy session. Nothing to fix.
