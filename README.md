# orbit-engine

**Local, auditable proof of every command you run — with automatic diagnosis when something breaks.**

When `pytest` fails at 3 AM, you need three things: the exact command, the full output, and a way to prove to your future self it really happened. Orbit writes all three to `~/.orbit/`, sealed with a SHA256 proof. Nothing leaves your machine.

## Install in 10 seconds

```bash
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash
~/.orbit/bin/orbit quickstart
```

That's it. The installer detects OS×ARCH, verifies the binary with `sha256sum -c`, installs at `~/.orbit/bin/orbit`, and smoke-tests `orbit version`. Fails closed with **CAUSE + ACTION** on any error.

## What it looks like

```
$ orbit run go test ./...
  FAIL: TestAuth (3 tests failed)
  Exit code:   1
  Proof:       299c672a2cad92e7...
  Event:       TEST_RUN (criticality: medium)
  Decision:    TRIGGER_ANALYZE
  Guidance:    auth_test.go:47 — assertion failure on token refresh

$ orbit verify ~/.orbit/logs/2026-04-22T*.json
  ✅  proof confere — sha256 verified
```

Full demo script: [docs/DEMO.md](docs/DEMO.md).

## Why not just shell history

- **Debug after the fact** — exit code + output + git snapshot at the exact moment it failed. Shell history loses all of that.
- **Working with Claude / GPT / Copilot** — SHA256 proof of what the AI actually ran, when, and the exit code. Not what it *said* it did.
- **Lightweight audit** — signed logs, re-verifiable offline with `orbit verify`. No server, no account, no telemetry.

## How it works

A single closed loop, executed on every `orbit run`:

```
execution → event → decision → snapshot → guidance
```

Five verbs: **detect** the command, **record** the execution with proof, **diagnose** risk patterns in the environment, **observe** git state when it matters, **prove** any entry is unaltered. Full walkthrough: [GUIDE.md](GUIDE.md).

---

```bash
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash
~/.orbit/bin/orbit quickstart
```

What the installer does, fail-closed:
1. Detects `OS × ARCH` (Linux/macOS, amd64/arm64).
2. Resolves latest release (or `--version vX.Y.Z`).
3. Downloads binary + `.sha256`.
4. Verifies integrity with `sha256sum -c` — aborts on mismatch.
5. Installs at `~/.orbit/bin/orbit` (no sudo) + smoke tests `orbit version`.

Any failure prints **CAUSA** and **AÇÃO** (cause + corrective action), never just an error.

Custom prefix / pinned version:
```bash
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | \
  bash -s -- --version v0.1.1 --prefix /usr/local/bin
```

### Manual install (if you prefer)

```bash
VERSION="v0.1.1"
OS="darwin"; ARCH="arm64"           # or: linux / amd64

BASE="https://github.com/IanVDev/orbit-engine/releases/download/${VERSION}"
BIN="orbit-${VERSION}-${OS}-${ARCH}"

curl -fsSLo "${BIN}" "${BASE}/${BIN}"
curl -fsSLo "${BIN}.sha256" "${BASE}/${BIN}.sha256"
sha256sum -c "${BIN}.sha256"        # must pass
chmod +x "${BIN}"
sudo install -m 0755 "${BIN}" /usr/local/bin/orbit
orbit version                       # orbit version v0.1.1 (commit=... build=...)
```

Fail-closed: `sha256sum -c` aborts if the binary does not match the published checksum. Do not skip it.

### First run (10 seconds)

```bash
orbit quickstart
```

Expected output: 3 steps (`[1/3]` init → `[2/3]` run → `[3/3]` proof verified) with `session_id`, `proof` SHA256, `event_id`. Total time: ~0.02s of work, instant feedback.

If something is off, run `orbit doctor` — it reports `OK/WARNING/CRITICAL` for each check (PATH, binary integrity, tracking-server connectivity) and tells you what to fix.

## Usage

### Claude Code

Drag the `skill/` folder directly into the Claude Code interface.

Or drag the individual `.md` files from inside `skill/` (SKILL.md is the minimum).

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
├── skill/                    # Installable skill (drag into Claude Code)
│   ├── SKILL.md              # Core logic — install this
│   ├── EXAMPLES.md           # Output examples
│   ├── ONBOARDING.md         # First-time setup (inside skill)
│   └── QUICK-START.md        # Quick reference (inside skill)
├── orbit-engine.prompt.md    # Universal prompt (GPT, Gemini, etc.)
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
