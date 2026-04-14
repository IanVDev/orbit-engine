# Contributing

Contributions are welcome for improvements to the skill logic, new strategies, documentation fixes, or bug reports.

## Naming convention

The project name is `orbit-engine`. Use `orbit-engine:` as the response label in all usage examples throughout the docs and demo. When referencing the install artifact, always use `skill/` folder or `skill/SKILL.md`. No aliases, no abbreviations.

## Before opening a PR

1. **Check existing issues** to avoid duplicate work.
2. **Keep the skill compact** because if the skill's output grows, it defeats its own purpose.
3. **Test your changes** against the 3 standard scenarios:
   - Long session before a complex task
   - Tokens near the limit
   - Complex migration or architecture planning

## Adding a new strategy

- Include a concrete before/after example with estimated token counts.
- The strategy must be actionable in ≤1 line of instruction.
- It must not overlap with an existing strategy unless it's clearly superior.

## Editing the skill file

The `.skill` file controls the diagnostic and recommendation logic. When editing:

- Keep the response format: `DIAGNOSIS → ACTIONS → DO NOT DO NOW`
- Maximum 3 actions per recommendation with no exceptions
- Each action must reference a specific command or Claude Code feature

## Reporting issues

Open a GitHub issue with:

- What you were doing when the skill activated (or didn't)
- What the skill recommended
- What the expected behavior was

## What belongs in public documentation

All documentation in this repository is written for users, not for internal decision tracking.

Before opening a PR, ask:

- Does this text belong to a user trying to install or use the skill?
- Or does it belong to a private decision log?

If it's the second, it doesn't belong here.

**Do not include in public docs:**

- Adoption metrics or growth targets
- Expansion triggers or roadmap timelines
- Architectural limitations or known weaknesses
- Internal reasoning about what to build next

**When in doubt:** abstract to principles, not specifics.

## Commit checklist

Before pushing, confirm:

- [ ] No internal metrics or targets (stars, user counts, timelines)
- [ ] No roadmap specifics (what comes next, when, under what conditions)
- [ ] No known limitations framed as future problems to solve
- [ ] All new docs are written for users, not for contributors tracking decisions
- [ ] Naming convention respected: `orbit-engine`, `skill/`, no aliases

## Code of conduct

Be direct, constructive, and respectful. No fluff.
