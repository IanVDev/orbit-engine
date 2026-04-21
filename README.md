# orbit-engine

**An append-only, verifiable record of human↔AI sessions, with diagnosis based on evidence.**

Orbit Engine observes the commands you run, classifies each event, decides whether to snapshot the state, and emits actionable guidance — every record is sealed with a SHA256 proof. Five verbs only: **detect, record, diagnose, observe, prove**.

[Get started in 2 min](ONBOARDING.md) · [Tutorial](TUTORIAL.md) · [Usage](#usage) · [See output](#what-it-outputs)

---

## What it does

A single closed loop, executed on every `orbit run`:

```
execution → event → decision → snapshot → guidance
```

- **detect** — a deterministic classifier reads the command (e.g. `git commit`, `pytest`, `git push`, `docker build`) and assigns an event type.
- **record** — every execution becomes an append-only JSON entry under `~/.orbit/logs/`, sealed with a SHA256 proof.
- **diagnose** — `orbit doctor` (and the deprecated `orbit analyze`) inspects the local environment for risk patterns (PATH conflicts, missing commit stamp, broken tracking) and stays silent when healthy.
- **observe** — when the decision engine triggers a snapshot, `git status / HEAD / diff --stat` is captured read-only into `~/.orbit/snapshots/`. Orbit never writes to your project files.
- **prove** — every log entry can be re-hashed and verified against the stored proof. Nothing is taken on faith.

---

## What you actually get

Orbit Engine ships in two layers. The CLI is the product; the telemetry stack is opt-in.

| | **CLI (default)** | **Optional telemetry mode** |
| --- | --- | --- |
| What it is | The `orbit` Go binary plus the Claude Code skill. Closes the loop locally and writes evidence to `~/.orbit/`. | The CLI plus a Go tracking server, PromQL gateway, Prometheus and Grafana for longitudinal observation. |
| What it needs | The binary on your `PATH`. Nothing else. | Docker + Prometheus/Grafana on a host you control. |
| What it answers | *"What just happened, what did the engine decide, and what should I look at?"* | *"How do events trend across N sessions? Where do risk patterns repeat?"* |
| Who it's for | Every user. | Teams that need longitudinal evidence beyond per-session records. |

If you are not sure which you want, use the CLI. The telemetry stack under `tracking/` is purely optional and the loop works without it.

---

## The closed loop in practice

Real scenario: a `git commit` followed by a failing `pytest` run.

| Step | Event | Decision | Effect |
| --- | --- | --- | --- |
| `orbit run git commit -m wip` | `CODE_CHANGE` | `TRIGGER_SNAPSHOT` | snapshot of `git status / HEAD / diff --stat` written to `~/.orbit/snapshots/` |
| `orbit run pytest` (exit 1) | `TEST_RUN` (criticality `medium`) | `TRIGGER_ANALYZE` | guidance points at the first `file:line` extracted from the failing output |
| `orbit run git push` | `PUBLISH` | `TRIGGER_SNAPSHOT` | snapshot of the published state for later comparison |

Every entry in `~/.orbit/logs/` is one JSON document containing the command, exit code, event, decision, criticality, snapshot path, guidance and the SHA256 proof. The dashboard reads the same files — no second source of truth.

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

## Install (CLI binary)

Pre-built binaries for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` are published on every tagged release.

```bash
# pick your platform
VERSION="v0.1.0"
OS="darwin"   # or: linux
ARCH="arm64"  # or: amd64

BASE="https://github.com/IanVDev/orbit-engine/releases/download/${VERSION}"
BIN="orbit-${VERSION}-${OS}-${ARCH}"

# download + verify + install
curl -fsSLo "${BIN}" "${BASE}/${BIN}"
curl -fsSLo "${BIN}.sha256" "${BASE}/${BIN}.sha256"
sha256sum -c "${BIN}.sha256"
chmod +x "${BIN}"
sudo install -m 0755 "${BIN}" /usr/local/bin/orbit

orbit version  # expects: orbit version v0.1.0 (commit=... build=...)
```

Fail-closed: `sha256sum -c` aborts if the binary does not match the published checksum. Do not skip it.

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

Then use it normally — Orbit Engine will activate when it detects a risk pattern in your conversation.

---

## What it outputs

Fixed format. Always recommends, never executes.

```
DIAGNOSIS
- [detected risk pattern]
- [detected risk pattern]
Risk: [low / medium / high / critical]

ACTIONS
1. [exact command] — [why it helps here]
2. [exact command] — [why it helps here]

DO NOT DO NOW
- [what to avoid and why]
```

The skill stays silent when the session is healthy — no output means no risk pattern detected.

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

## Release gate

All CLI releases are tagged only after `make gate-cli` returns 🟢 PASS.
Contract and 8-gate breakdown in [docs/CLI_RELEASE_GATE.md](docs/CLI_RELEASE_GATE.md).
Runs offline in under 120s and requires only `go`, `python3`, `bash`.

## Contributing

Pull requests welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

---

## License

MIT © 2026 · See [LICENSE](LICENSE).
