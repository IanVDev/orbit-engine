Always respond in Brazilian Portuguese.
Use clear, technical, and direct language.
Never mix languages.

---

# ORBIT-ENGINE — CLAUDE.md

## Objective
Build a deterministic CLI where behavior is enforced, not assumed.
If it cannot be tested or executed, it does not exist.

## Core Rules

1. **Verifiability** — every change must result in code or tests. No conceptual changes.
2. **Fail-Closed** — if something is critical, block it. Do not degrade silently.
3. **Single Source of Truth** — never duplicate logic. Reuse existing scripts and modules.
4. **Git Index Only** — all file checks must use `git cat-file -s :<path>`.
5. **Idempotency** — commands must be safe to run multiple times.
6. **No Trust in User Behavior** — automate protections. Assume users will skip steps.

## CLI Expectations
- Explicit output only (INSTALLED, WARNING, NOT_INSTALLED).
- No silent success or failure.
- No hidden side effects.

## Testing
At least one test per feature. Gate: `make gate-cli` must pass (9 gates, offline, fail-closed).

## Hygiene
- >5MB staged: block.
- >1MB staged: warn.
- Enforced locally (pre-commit hook in `scripts/hooks/`) and in CI.

## Decision Rule
If unclear: stop and present two interpretations.

## Anti-Patterns
- Duplicating shell logic in Go.
- Relying on working tree instead of index.
- Adding abstractions without tests.
- Silent degradation.
