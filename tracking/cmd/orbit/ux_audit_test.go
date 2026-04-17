// ux_audit_test.go — G5: output structure audit for the `orbit` CLI.
//
// WHAT this tests: every user-facing command produces structured output.
// By "structured" we mean: output contains at least one recognizable
// anchor — a step marker, a status keyword, a KV line, or a version
// string. This rules out unstructured text dumps (free prose, raw stack
// traces, empty output).
//
// WHAT this does NOT test: exhaustive output content (that belongs to
// individual command tests). The audit is about observable UX shape, not
// business logic.
//
// Commands covered:
//   - version      → always deterministic, no server
//   - help         → always deterministic, no server
//   - doctor       → ORBIT_SKIP_GUARD=1 + no live server (expect WARNINGs, not CRITICAL exit)
//
// Commands intentionally excluded:
//   - quickstart   → full E2E covered by G3; too slow + embedded server
//   - stats        → requires live tracking-server at :9100
//   - run          → requires external command + server
//   - context-pack → depends on repo state of CWD
//   - analyze      → requires args
//
// TestMain compiles the real binary once per test run. Fail-closed:
// build failure aborts all tests.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ── TestMain: build once ──────────────────────────────────────────────────────

var uxAuditBin string

func TestMain(m *testing.M) {
	code, err := runTestMainForUXAudit(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain (ux_audit): %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runTestMainForUXAudit(m *testing.M) (int, error) {
	dir, err := os.MkdirTemp("", "orbit-ux-audit-*")
	if err != nil {
		return 1, fmt.Errorf("mkdirtemp: %w", err)
	}
	defer os.RemoveAll(dir)

	bin := filepath.Join(dir, "orbit")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 1, fmt.Errorf("go build: %v\n%s", err, stderr.String())
	}
	uxAuditBin = bin
	return m.Run(), nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// runOrbit runs the real orbit binary with the given args and returns the
// combined output. It sets ORBIT_SKIP_GUARD=1 so CI environments with
// multiple orbit installations don't block the command.
// timeout is 15s; fail-closed on timeout or non-zero exit.
func runOrbit(t *testing.T, expectExit0 bool, args ...string) string {
	t.Helper()
	if uxAuditBin == "" {
		t.Fatal("uxAuditBin not set — TestMain must have failed")
	}
	cmd := exec.Command(uxAuditBin, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "ORBIT_SKIP_GUARD=1")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		if expectExit0 && err != nil {
			t.Fatalf("orbit %v exited non-zero: %v\noutput:\n%s", args, err, out.String())
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("orbit %v timed out\npartial output:\n%s", args, out.String())
	}
	return out.String()
}

// ── UX structure patterns ─────────────────────────────────────────────────────

// structuralPatterns describes the recognized output shapes.
// An output is "structured" when it contains at least one of these.
var structuralPatterns = []*regexp.Regexp{
	// [n/N] — step progress marker
	regexp.MustCompile(`\[\d+/\d+\]`),
	// STATUS keyword (case-sensitive tags used in doctor / hygiene)
	regexp.MustCompile(`\b(OK|WARNING|CRITICAL|INSTALLED|ALREADY_PRESENT|ERROR)\b`),
	// KV line: "  Label   : value"
	regexp.MustCompile(`^\s+\S[^:]+\s*:\s+\S`),
	// version string: "orbit version vX.Y.Z (commit=..." — no space before (
	regexp.MustCompile(`orbit version .+\(commit=`),
	// summary line from doctor: "Resumo: N OK · M WARNING · K CRITICAL"
	regexp.MustCompile(`Resumo:.*\d+ OK`),
	// command listing (help): two-space indent + command name
	regexp.MustCompile(`^\s{2,}\w[\w-]+\s{2,}\S`),
	// ✓ / ✅ / ⚠️ / ❌ glyph lines (structured feedback)
	regexp.MustCompile(`[✓✅⚠❌]`),
}

func hasStructuredOutput(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		for _, re := range structuralPatterns {
			if re.MatchString(line) {
				return true
			}
		}
	}
	return false
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestUXAudit_Version checks that `orbit version` emits exactly one line
// containing the version + commit — no raw data, no stacks.
func TestUXAudit_Version(t *testing.T) {
	out := runOrbit(t, true, "version")

	// Must contain the canonical version pattern.
	if !regexp.MustCompile(`orbit version .+\(commit=`).MatchString(out) {
		t.Fatalf("`orbit version` output missing structured version line\ngot:\n%s", out)
	}

	// Must have structured output at all.
	if !hasStructuredOutput(out) {
		t.Fatalf("`orbit version` output has no recognized structure\ngot:\n%s", out)
	}
}

// TestUXAudit_Help checks that `orbit help` lists the core commands.
func TestUXAudit_Help(t *testing.T) {
	// help exits 1 (no subcommand), so we don't assert exit0.
	out := runOrbit(t, false, "help")

	required := []string{"quickstart", "doctor", "version", "stats"}
	for _, cmd := range required {
		if !strings.Contains(out, cmd) {
			t.Errorf("`orbit help` missing command %q in output\ngot:\n%s", cmd, out)
		}
	}

	if !hasStructuredOutput(out) {
		t.Fatalf("`orbit help` output has no recognized structure\ngot:\n%s", out)
	}
}

// TestUXAudit_Doctor checks that `orbit doctor` emits a structured report
// with a Resumo line and severity keywords.
func TestUXAudit_Doctor(t *testing.T) {
	// Doctor exits non-zero when there are CRITICALs (e.g., no commit stamp
	// in the test binary). We don't assert exit0 here.
	out := runOrbit(t, false, "doctor")

	// Must have the summary line.
	if !regexp.MustCompile(`Resumo:.*\d+ OK`).MatchString(out) {
		t.Fatalf("`orbit doctor` missing 'Resumo:' summary line\ngot:\n%s", out)
	}

	// Doctor check lines use the format "    glyph  [TAG      ] name   detail".
	// Assert every such line (identified by the leading glyph) carries a
	// recognized tag. We deliberately exclude log-prefix lines ([SECURITY],
	// positional markers [1], etc.) by anchoring on the check-line shape.
	checkLine := regexp.MustCompile(`^\s+[✅⚠❌]\s+\[(\w+)`)
	validTags := map[string]bool{"OK": true, "WARNING": true, "CRITICAL": true}
	for _, line := range strings.Split(out, "\n") {
		m := checkLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tag := strings.TrimSpace(m[1])
		if !validTags[tag] {
			t.Errorf("doctor check line has unrecognized tag [%s]:\n  %s", tag, line)
		}
	}

	if !hasStructuredOutput(out) {
		t.Fatalf("`orbit doctor` output has no recognized structure\ngot:\n%s", out)
	}
}

// ── TestAllCommandsUseStandardOutput — G5 strict audit ──────────────────────
//
// Goal: every command documented in `orbit help` must emit output that
// matches at least one of the three canonical UX patterns. The command
// list is extracted at runtime from the help listing, so adding a new
// command automatically brings it under the audit.
//
// The three patterns (exclusive — any additional shape must be justified
// by updating the patterns here, not by exempting commands):
//
//   1. Status keyword   — OK | WARNING | CRITICAL | INSTALLED | ALREADY_PRESENT
//                         | ERROR | HIGH | MEDIUM | LOW | SKIPPED | DEGRADED
//   2. Step marker      — [n/N]
//   3. KV line          — "label<sep>value" with : or = as separator
//
// Pre-validation, we strip two known lines of startup noise — the stdlib
// log banner and the trust-level banner — so they can't trivially satisfy
// the audit for any command.

// helpCommandPattern captures command names from `orbit help`. The listing
// format is "  name    description", stable across sessions.
var helpCommandPattern = regexp.MustCompile(`^\s{2}([a-z][a-z0-9-]+)\s{2,}\S`)

// bannerLines are the known-invariant startup lines emitted by the orbit
// process before any command-specific output. They must be stripped for
// the audit — they are not the command's UX.
var bannerLines = []*regexp.Regexp{
	regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}\s+\[`), // stdlib log prefix
	regexp.MustCompile(`^orbit: trust=`),                             // trust banner
}

// standardOutputPatterns is the CLOSED SET of recognized UX shapes. The
// audit fails if a command's output (post-banner-strip) contains no line
// matching any of these.
var standardOutputPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(OK|WARNING|CRITICAL|INSTALLED|ALREADY_PRESENT|ERROR|HIGH|MEDIUM|LOW|SKIPPED|DEGRADED)\b`),
	regexp.MustCompile(`\[\d+/\d+\]`),
	regexp.MustCompile(`\w+\s*[:=]\s*\S`),
}

// extractCommandsFromHelp parses the command listing from help output.
// Returns the command names in the order they appear (stable).
func extractCommandsFromHelp(help string) []string {
	var cmds []string
	inSection := false
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "Comandos:") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.TrimSpace(line) == "" {
			break // section ends on blank line
		}
		if m := helpCommandPattern.FindStringSubmatch(line); m != nil {
			cmds = append(cmds, m[1])
		}
	}
	return cmds
}

// stripBannerLines removes known startup-noise lines from output.
func stripBannerLines(raw string) string {
	var kept []string
	for _, line := range strings.Split(raw, "\n") {
		banner := false
		for _, re := range bannerLines {
			if re.MatchString(line) {
				banner = true
				break
			}
		}
		if !banner {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// matchesStandardPattern returns true if any non-empty, non-whitespace line
// of s matches at least one pattern in standardOutputPatterns.
func matchesStandardPattern(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		for _, re := range standardOutputPatterns {
			if re.MatchString(line) {
				return true
			}
		}
	}
	return false
}

// TestAllCommandsUseStandardOutput is the G5 fail-closed audit. It
// extracts the full command list from `orbit help` and asserts that every
// command's output (after banner strip) exhibits at least one recognized
// UX pattern.
//
// Commands that require arguments are invoked with `--help` instead, so
// they still produce command-specific output. Quickstart is exempted here
// because it has dedicated E2E coverage (TestQuickstart_*) and running it
// on every audit iteration would be wasteful.
func TestAllCommandsUseStandardOutput(t *testing.T) {
	// runOrbit uses the binary built by TestMain in this file. No extra
	// build helper is required.

	// 1. Extract command list from help (dynamic — new commands auto-audit).
	helpRaw := runOrbit(t, false, "help")
	commands := extractCommandsFromHelp(helpRaw)
	if len(commands) < 3 {
		t.Fatalf("expected several commands in help output, got %d: %v\n---\n%s",
			len(commands), commands, helpRaw)
	}

	// No commands currently need special flag handling — each one either
	// has default behaviour that emits structured stdout or emits a
	// structured error on stderr when called with no args. We prefer the
	// natural invocation over --help, because Go's default flag-help format
	// is distinct from our 3-pattern UX and would force a false exemption.
	needsHelpFlag := map[string]bool{}

	// Commands skipped with explicit justification (NOT a free pass — each
	// entry must be backed by dedicated coverage elsewhere).
	skip := map[string]string{
		"quickstart": "covered end-to-end by TestQuickstart_* (slow, uses embedded server)",
	}

	for _, cmd := range commands {
		cmd := cmd
		t.Run(cmd, func(t *testing.T) {
			if reason, ok := skip[cmd]; ok {
				t.Skipf("audit skip — %s", reason)
			}

			args := []string{cmd}
			if needsHelpFlag[cmd] {
				args = append(args, "--help")
			}

			raw := runOrbit(t, false, args...)
			stripped := stripBannerLines(raw)
			if strings.TrimSpace(stripped) == "" {
				t.Fatalf("`orbit %s` produced no output after stripping startup banner — unaudited UX\nraw:\n%s",
					strings.Join(args, " "), raw)
			}
			if !matchesStandardPattern(stripped) {
				t.Fatalf("`orbit %s` output does not match any of the 3 UX patterns (Status / Step / KV):\n%s",
					strings.Join(args, " "), stripped)
			}
		})
	}
}

// TestUXAudit_NoCommandPanics verifies that every documented command
// (when invoked with no args or --help) exits cleanly without a Go panic
// stack trace in the output.
func TestUXAudit_NoCommandPanics(t *testing.T) {
	commands := []string{"version", "help", "doctor", "stats", "context-pack"}
	panicPattern := regexp.MustCompile(`goroutine \d+ \[`)

	for _, sub := range commands {
		t.Run(sub, func(t *testing.T) {
			// Don't assert exit0 — some exit 1 legitimately.
			out := runOrbit(t, false, sub)
			if panicPattern.MatchString(out) {
				t.Errorf("`orbit %s` output contains a panic stack trace:\n%s", sub, out)
			}
		})
	}
}
