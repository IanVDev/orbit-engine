# orbit-engine

Local, auditable proof of every command you run — with automatic diagnosis when something breaks.

When `pytest` fails at 3 AM, you need three things: the exact command, the full output, and a way to prove to your future self it really happened. Orbit writes all three to `~/.orbit/`, sealed with a SHA256 proof. Nothing leaves your machine by default.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash
~/.orbit/bin/orbit quickstart
```

The installer detects OS×ARCH, verifies the binary with `sha256sum -c`, installs at `~/.orbit/bin/orbit`, and runs `orbit version`. Fails closed with **CAUSE + ACTION** on any error.

Custom prefix / pinned version:
```bash
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | \
  bash -s -- --version <version> --prefix /usr/local/bin
```

Replace `<version>` with the desired tag (e.g. `v0.2.2`). Check [Releases](https://github.com/IanVDev/orbit-engine/releases) for the latest.

### Manual install (inspect before running)

If you prefer to inspect the installer before executing it:

```bash
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh \
  -o install_orbit.sh
cat install_orbit.sh          # inspect before running
bash install_orbit.sh
```

For a pinned version on a specific platform:

```bash
VERSION="$(gh release view --repo IanVDev/orbit-engine --json tagName -q .tagName)"
OS="darwin"; ARCH="arm64"    # or: linux / amd64

BASE="https://github.com/IanVDev/orbit-engine/releases/download/${VERSION}"
BIN="orbit-${VERSION}-${OS}-${ARCH}"

curl -fsSLo "${BIN}" "${BASE}/${BIN}"
curl -fsSLo "${BIN}.sha256" "${BASE}/${BIN}.sha256"
sha256sum -c "${BIN}.sha256"   # must pass — do not skip
chmod +x "${BIN}"
install -m 0755 "${BIN}" ~/.orbit/bin/orbit
orbit version
```

---

## Security Model

**Orbit executes commands on your machine. It does not sandbox or isolate execution.**

- `orbit run <cmd>` executes `<cmd>` with your user's full permissions.
- Do not pass untrusted commands to `orbit run`.
- Orbit observes and records — it does not restrict what a command can do.

**Log sanitization.** Before writing to `~/.orbit/logs/`, orbit redacts known secret patterns from captured output:
- `Authorization: Bearer <token>` → `Bearer [REDACTED]`
- `x-authorization: <value>` → `x-authorization: [REDACTED]`
- `password=`, `token=`, `api_key=`, `api-key=` → value replaced with `[REDACTED]`
- `sk-live-*`, `sk-test-*`, `sk-proj-*` (Stripe/OpenAI keys) → `[REDACTED]`
- `AKIA...` (AWS access keys) → `[REDACTED]`
- SSH private key headers → `[REDACTED]`

Redaction is applied before persistence, not at read time. The SHA256 proof covers the original `output_bytes` count, not the redacted string — so proof integrity is preserved.

**Network.** The optional local tracking server binds to `127.0.0.1:9100` by default. `orbit run` works fully without it — the server is only needed for aggregated metrics (`orbit stats`). No outgoing connections are made to external hosts unless explicitly configured.

---

## Safe Defaults

Out of the box, without any configuration:

| Behavior | Default |
|---|---|
| Log storage | `~/.orbit/logs/` (local disk only) |
| Tracking server bind | `127.0.0.1:9100` (loopback only) |
| Remote tracking | disabled |
| HMAC authentication | disabled (dev mode) |
| Secret redaction | always active |

These defaults are intentional: orbit is safe to run on a local development machine without any configuration.

To verify your current configuration:

```bash
orbit doctor --security
```

Expected output on a clean local setup:

```
⚠️  [WARNING ] ORBIT_MODE        not set — recommended: public for external exposure
✅  [OK      ] ORBIT_HMAC_SECRET  absent (acceptable in local dev)
✅  [OK      ] Network binding    loopback only (127.0.0.1)
✅  [OK      ] Log sanitization   3 patterns verified (Bearer, x-authorization, password)
```

---

## Public Mode

If you expose the tracking server to a network, `ORBIT_MODE=public` enforces additional constraints.

**Minimum required configuration:**

```bash
export ORBIT_MODE=public
export ORBIT_HMAC_SECRET="$(openssl rand -hex 32)"
```

**What public mode enforces (fail-closed):**

| Condition | Behavior |
|---|---|
| `ORBIT_MODE=public` without `ORBIT_HMAC_SECRET` | Process exits at startup with `[SECURITY] FATAL` |
| `ORBIT_REMOTE_TRACKING=on` without `ORBIT_HMAC_SECRET` | Process exits at startup with `[SECURITY] FATAL` |
| `ORBIT_BIND_ALL=1` without `ORBIT_HMAC_SECRET` | `orbit doctor --security` reports CRITICAL |

**Fail-closed** means the server refuses to start, not that it starts in a degraded state.

**HMAC authentication.** When `ORBIT_HMAC_SECRET` is set, all requests to `/track` and `/reconcile` must include a valid `X-Orbit-Signature` header (HMAC-SHA256 of the request body). Requests without a valid signature are rejected with HTTP 401.

**Remote tracking.** In public mode, remote event tracking is disabled by default. To enable:

```bash
export ORBIT_MODE=public
export ORBIT_HMAC_SECRET="<your-secret>"
export ORBIT_REMOTE_TRACKING=on   # requires ORBIT_HMAC_SECRET — fails if absent
```

**Binding.** By default the server binds to `127.0.0.1:9100`. To expose on all interfaces (requires HMAC):

```bash
export ORBIT_BIND_ALL=1
export ORBIT_HMAC_SECRET="<your-secret>"
```

If you use `ORBIT_BIND_ALL=1`, placing orbit behind a reverse proxy (nginx, caddy) with TLS is strongly recommended over direct public exposure.

**Verify configuration before starting:**

```bash
ORBIT_MODE=public ORBIT_HMAC_SECRET="<your-secret>" orbit doctor --security
```

All checks must return `OK` before exposing the server externally.

---

## Logs

Logs are written to `~/.orbit/logs/` as JSON files, one per execution.

**Format:**

```json
{
  "version": 1,
  "session_id": "run-1713886800000000000",
  "command": "go test ./...",
  "exit_code": 1,
  "output": "...(redacted if secrets detected)...",
  "output_bytes": 5432,
  "proof": "sha256hex",
  "prev_proof": "sha256hex_of_previous_log",
  "timestamp": "2026-04-25T03:41:11Z"
}
```

**Integrity.** Each log includes `proof = SHA256(session_id + timestamp + output_bytes)` and a `prev_proof` chain. To verify:

```bash
orbit verify ~/.orbit/logs/<file>.json
orbit verify --chain     # verifies every log in sequence
```

**Sanitization.** Secrets matching known patterns (`Authorization: Bearer`, `password=`, `x-authorization:`, AWS keys, etc.) are redacted before display in the terminal and before persistence. The redacted version appears on screen and is stored in the log. The SHA256 proof is derived from the original `output_bytes` count, not from the redacted text — so proof integrity is preserved regardless of how many values were redacted.

**Retention.** Logs are not automatically deleted. Use `ORBIT_MAX_LOGS=<n>` to limit the number of files retained. Recommended for long-running installations:

```bash
export ORBIT_MAX_LOGS=500
```

Without this limit, `~/.orbit/logs/` grows unbounded. Each log file is typically 2–10 KB.

---

## Minimal secure usage example

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/IanVDev/orbit-engine/main/scripts/install_remote.sh | bash

# 2. Verify environment (local dev — no public exposure)
orbit doctor

# 3. Run a command with proof
orbit run go test ./...

# 4. Verify the proof
orbit verify --chain

# 5. Inspect the diagnosis on failure
orbit diagnose
```

For public exposure, configure before starting:

```bash
export ORBIT_MODE=public
export ORBIT_HMAC_SECRET="$(openssl rand -hex 32)"
export ORBIT_MAX_LOGS=500

# Verify security posture — all checks must be OK
orbit doctor --security
```

---

## Usage

### orbit run

```bash
orbit run <command> [args...]
```

Executes the command, captures output, computes SHA256 proof, classifies the event, and writes a structured log to `~/.orbit/logs/`.

```
$ orbit run go test ./...
  Exit code:   1
  Proof:       299c672a2cad92e7...
  Event:       TEST_RUN (criticality: medium)
  Decision:    TRIGGER_ANALYZE
  Guidance:    auth_test.go:47 — assertion failure on token refresh
```

**Live output.** In interactive terminals, `orbit run` streams stdout and stderr in real time as the command runs. Sensitive values (`Authorization: Bearer`, `x-authorization:`, `password=`, and similar patterns) are redacted before display — `[REDACTED]` appears on screen instead of the raw value. The SHA256 proof is generated from the original captured `output_bytes`, not from the redacted text, so proof integrity is unaffected.

In `--json` mode, CI environments, or non-TTY pipes, the live UI is not rendered. The `live_output_mode` field in the run result and log (`"interactive"`, `"ci"`, or `"json"`) records which mode was active.

### orbit doctor

```bash
orbit doctor              # environment diagnostics (PATH, binary, tracking server)
orbit doctor --security   # security configuration check (always strict)
orbit doctor --strict     # treat warnings as errors
orbit doctor --deep       # deep diagnostics: symlinks, wrappers, commit mismatch
orbit doctor --json       # structured JSON output
```

### orbit verify

```bash
orbit verify ~/.orbit/logs/<file>.json   # verify single log
orbit verify --chain                     # verify entire chain
```

### orbit diagnose

Extracts the probable failure cause from the most recent log — file path, line number, error type, and next-step guidance — so you don't have to scan raw output.

```bash
orbit diagnose            # analyze most recent log
orbit diagnose <file>     # analyze a specific log file
orbit diagnose --json     # structured output
```

### orbit run --safe

```bash
orbit run --safe <command> [args...]
```

Analyzes the command's risk level **without executing it**. No process is created, no file is modified.

```
$ orbit run --safe rm -rf /
  Comando recebido:  rm -rf /
  🔴 Risco: CRITICAL
  Fatores:
    - destruição de sistema de arquivos raiz (rm -rf /)
  ⚠️   execution skipped (safe mode)
  Nenhum processo foi criado. Nenhum arquivo foi modificado.
```

`--safe` is a **risk preview**, not a sandbox. It never executes the command. Dangerous commands remain dangerous outside Orbit — `--safe` does not make them safe.

### orbit stats

```bash
orbit stats               # aggregate metrics from local store
orbit stats --share       # shareable summary
```

### Claude Code skill

Drag the `skill/` folder into the Claude Code interface, or drag individual `.md` files from inside `skill/`.

After installing, use `orbit run` normally. The skill activates when risk patterns are detected in the session.

#### /orbit-prompt

The `orbit-prompt` skill ships as a separate artifact (`orbit-prompt-skill/orbit-prompt.skill`) and exposes a slash command in Claude Code:

```text
/orbit-prompt <sua tarefa aqui>
```

Example:

```text
/orbit-prompt "Refactor auth module"
→ IMPROVED: "Extract middleware from auth.ts to middleware/auth.ts.
   Keep function signatures. Don't touch routes or schema.
   Success = all tests pass."
→ VERDICT: READY TO SEND
```

> **Autocomplete note.** If `/orbit-prompt` does not appear in the Claude Code autocomplete, install the skill via the Claude Code interface and restart the session. The slash command bridge is at `.claude/commands/orbit-prompt.md`.

### All commands

| Command | Description |
|---|---|
| `orbit run <cmd>` | Execute with proof, live output, and log |
| `orbit run --safe <cmd>` | Risk preview without execution |
| `orbit run --json <cmd>` | Structured JSON output |
| `orbit doctor` | Environment diagnostics |
| `orbit doctor --security` | Security posture check (strict) |
| `orbit doctor --deep` | Deep diagnostics (symlinks, wrappers, path) |
| `orbit verify <file>` | Verify a single log's proof |
| `orbit verify --chain` | Verify entire log chain |
| `orbit diagnose` | Extract probable failure cause from last log |
| `orbit stats` | Aggregate metrics from local store |
| `orbit hygiene install` | Install pre-commit hook in current repo |
| `orbit hygiene check` | Check if pre-commit hook is present |
| `orbit context-pack` | Generate context pack for session transitions |
| `orbit logs prune` | Prune logs older than a threshold |
| `orbit update` | Update the orbit binary |
| `orbit version` | Print installed version |
| `orbit quickstart` | Full onboarding walkthrough |
| `orbit analyze` | *(deprecated)* Alias for `orbit doctor --alert-only` |

---

## How it works

```
execution → event classification → decision → snapshot → guidance
```

1. `orbit run` executes the command and captures stdout + stderr.
2. The command is classified into an event type (TEST_RUN, BUILD_FAILED, CODE_MERGE, etc.).
3. A decision is computed (NONE / TRIGGER_SNAPSHOT / TRIGGER_ANALYZE).
4. A SHA256 proof is computed and chained to the previous log.
5. The log is written with secrets redacted.

Full reference: [GUIDE.md](GUIDE.md).

---

## Release gate

All CLI releases pass `make gate-cli` before tagging. Contract in [docs/CLI_RELEASE_GATE.md](docs/CLI_RELEASE_GATE.md). Runs offline in under 120s, requires only `go`, `python3`, `bash`.

## Contributing

Pull requests welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT © 2026 — see [LICENSE](LICENSE).
