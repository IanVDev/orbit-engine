# orbit-engine

[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Local CLI for traceable command execution with verifiable evidence.**

orbit-engine runs commands on your machine, captures stdout/stderr, and writes a structured log to `~/.orbit/` sealed with a SHA256 proof chain. When something fails, you get the exact command, the full output, and a way to verify it really happened — without anything leaving your machine by default.

---

## What it does

- `orbit run <cmd>` — executes the command, redacts secrets, writes a proof-sealed log
- `orbit verify` — re-validates the SHA256 proof of any log or the entire chain
- `orbit doctor` — checks installation, PATH conflicts, and security configuration
- `orbit diagnose` — extracts probable failure cause from the most recent log

orbit-engine **observes and records**. It does **not** sandbox, isolate, or restrict what your commands can do.

---

## When to use

- You want a verifiable record of CI/test runs without sending data to a third party
- You need to prove a specific command was executed with a specific output
- You're handing off a session and want the next person (or future you) to see what really ran
- You want secrets redacted from logs before they hit disk

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash
~/.orbit/bin/orbit quickstart
```

The installer detects OS×ARCH (linux/darwin × amd64/arm64), verifies the binary with `sha256sum -c`, and installs at `~/.orbit/bin/orbit`. Pinned version or custom prefix:

```bash
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | \
  bash -s -- --version v0.2.2 --prefix /usr/local/bin
```

To inspect the installer before running it, see [QUICK-START.md](QUICK-START.md).

---

## First use in 60 seconds

```bash
# 1. Validate environment
orbit doctor

# 2. Run a command — generates proof + log
orbit run go test ./...

# 3. Verify the proof chain
orbit verify --chain

# 4. If it failed, extract the probable cause
orbit diagnose
```

Every `orbit run` writes one JSON file to `~/.orbit/logs/` with: command, exit code, redacted output, output bytes, SHA256 proof, and `prev_proof` for chain integrity.

---

## Commands

| Command | Description |
|---|---|
| `orbit run <cmd>` | Execute with proof, live output, and log |
| `orbit run --safe <cmd>` | Risk preview — analyzes the command **without executing it** |
| `orbit run --json <cmd>` | Structured JSON output |
| `orbit doctor` | Environment diagnostics (PATH, binary, tracking server) |
| `orbit doctor --security` | Security posture check (strict) |
| `orbit doctor --deep` | Deep diagnostics (symlinks, wrappers, commit mismatch) |
| `orbit verify <file>` | Verify a single log's proof |
| `orbit verify --chain` | Verify the entire log chain |
| `orbit diagnose` | Extract probable failure cause from last log |
| `orbit stats` | Aggregate metrics (local history works offline; `/metrics` panel needs the tracking server) |
| `orbit hygiene install` | Install pre-commit hook in current repo |
| `orbit hygiene check` | Check if pre-commit hook is present |
| `orbit context-pack` | Generate context pack for session transitions (alias: `ctx`) |
| `orbit history` | Browse local execution history with secret redaction |
| `orbit update` | Update the orbit binary |
| `orbit version` | Print installed version |
| `orbit quickstart` | Full onboarding walkthrough |

### orbit history

Lists local execution logs from `~/.orbit/logs/`.

```bash
orbit history                        # table: most recent first
orbit history --failed               # only executions with exit_code != 0
orbit history --detail <session_id>  # full details for a specific execution
orbit history --json                 # structured JSON output
```

Sensitive fields (`output`, `args`, `guidance`, `decision_reason`) are redacted
before any rendering. Records missing `session_id` or `timestamp` are excluded
from trusted output and reported as degraded integrity (exit code 2).

Full reference and internals: [GUIDE.md](GUIDE.md).

---

## Security model

**orbit-engine does not sandbox or isolate execution.** `orbit run <cmd>` executes `<cmd>` with your user's full permissions. Do not pass untrusted commands to `orbit run`.

Defaults out of the box (no configuration required):

| Behavior | Default |
|---|---|
| Log storage | `~/.orbit/logs/` — local disk only |
| Outgoing network | none — no data leaves your machine |
| Remote tracking | disabled |
| HMAC authentication | disabled (dev mode) |
| Secret redaction | always active (Bearer tokens, API keys, AWS keys, SSH headers) |

Redaction is applied **before** display in the terminal **and** before writing to disk. The SHA256 proof covers the original `output_bytes` count, not the redacted string — so proof integrity is preserved regardless of how many values were redacted.

**Tracking server.** The orbit CLI does not start a server automatically. The optional tracking server is a separate component used for aggregated metrics (`orbit stats`). When used:

- `orbit quickstart` starts an embedded server on `127.0.0.1:<random-port>` (loopback, random port, never 9100 — only lives for the duration of quickstart)
- An external tracking server, if deployed, defaults to `127.0.0.1:9100` (loopback only). Setting `ORBIT_BIND_ALL=1` exposes it on all interfaces and requires `ORBIT_HMAC_SECRET` in public mode.
- `orbit doctor` checks `localhost:9100/health` and reports WARNING if unreachable — absence is expected in most local setups.

`orbit run`, `orbit verify`, `orbit diagnose`, and `orbit doctor` all work without any tracking server running.

Verify your current posture at any time:

```bash
orbit doctor --security
```

For exposing the tracking server externally, see [SECURITY.md](SECURITY.md) (covers `ORBIT_MODE=public`, HMAC, `ORBIT_BIND_ALL`, fail-closed enforcement). Threat model details are in [THREAT_MODEL.md](THREAT_MODEL.md).

---

## Verification & evidence

Each log includes:

```json
{
  "session_id": "run-1713886800000000000",
  "command": "go test ./...",
  "exit_code": 1,
  "output_bytes": 5432,
  "proof": "sha256hex",
  "prev_proof": "sha256hex_of_previous_log",
  "timestamp": "2026-04-25T03:41:11Z"
}
```

`proof = SHA256(session_id + timestamp + output_bytes)` and the chain is anchored via `prev_proof` to detect tampering. `orbit verify --chain` walks the entire chain and fails closed on any break.

---

## Claude Code skill

Drag the `skill/` folder into the Claude Code interface. The skill activates when risk patterns are detected during a session and provides the `/orbit-prompt` slash command for prompt refinement. See [skill/README.md](skill/README.md) for details.

---

## Contributing

Pull requests welcome. See [CONTRIBUTING.md](CONTRIBUTING.md). All releases pass `make gate-cli` (9 offline gates, <120s) before tagging — contract in [docs/CLI_RELEASE_GATE.md](docs/CLI_RELEASE_GATE.md).

## License

MIT © 2026 — see [LICENSE](LICENSE).
