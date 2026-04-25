# Security Policy

## Reporting vulnerabilities

If you discover sensitive content that shouldn't be in this repository,
open a **private** security advisory via GitHub:
**Settings → Security → Advisories → New draft advisory**

Do not open a public issue for security concerns.

## What belongs here (public)

- `skill/` — public skill logic (detection patterns, output format, gating rules)
- Documentation (README, GUIDE, TUTORIAL, ONBOARDING, QUICK-START, VALIDATION)
- CONTRIBUTING.md, LICENSE

## What does NOT belong here

- Scoring systems, weights, thresholds, or internal formulas
- Eval data, test fixtures, or assertion files
- Decision logs, roadmaps, or expansion strategies
- Monetization logic, pricing, or growth metrics
- Any file from: `evals/`, `decision-engine/`, `future/`, `.internal/`

These are blocked by `.gitignore`. If you need to work with internal logic,
use the private repository (see below).

## Terminal output redaction

`orbit run` redacts sensitive values before display in the terminal and before writing to `~/.orbit/logs/`. The SHA256 proof is derived from the original `output_bytes` count, not from the redacted output — proof integrity is preserved regardless of how many values were redacted. The `live_output_mode` field in run results records whether live output was active (`"interactive"`, `"ci"`, or `"json"`).

## Repository separation

| Repo | Visibility | Contains |
|------|-----------|----------|
| `orbit-engine` | **Public** | Skill files, documentation, examples |
| `orbit-engine-internal` | **Private** | Evals, decision engine, scoring, roadmap |

The public repo is a **consumer artifact**. The private repo is the **development workspace**.
