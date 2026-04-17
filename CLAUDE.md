# ORBIT-ENGINE — CLAUDE.md

## Objective
Build a deterministic CLI where behavior is enforced, not assumed.

If it cannot be tested or executed, it does not exist.

---

## Core Rules

1. Verifiability
Every change must result in code or tests.
No conceptual changes.

2. Fail-Closed
If something is critical, block it.
Do not degrade silently.

3. Single Source of Truth
Never duplicate logic.
Reuse existing scripts and modules.

4. Git Index Only
All file checks must use:
git cat-file -s :<path>

5. Idempotency
Commands must be safe to run multiple times.

6. No Trust in User Behavior
Automate protections.
Assume users will skip steps.

---

## CLI Expectations

- Explicit output only (INSTALLED, WARNING, NOT_INSTALLED)
- No silent success or failure
- No hidden side effects

---

## Testing

At least one test per feature.

go test ./... -count=1

---

## Hygiene

- >5MB: block
- >1MB: warn
- Enforced locally (hook) and in CI

---

## Decision Rule

If unclear:
stop and present two interpretations.

---

## Anti-Patterns

- Duplicating shell logic in Go
- Relying on working tree instead of index
- Adding abstractions without tests
- Silent degradation

---

## Final

The system must enforce correctness.

Not suggest it.