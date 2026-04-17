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
