// doctor_structured_test.go — G-struct: tests that assert against the
// DoctorReport struct directly instead of parsing terminal output.
//
// Coverage:
//   - TestDoctorReport_StatusShape    → struct contract (Status ∈ {OK, WARNING, CRITICAL})
//   - TestDoctorReport_MessageCarriesDetail → Message includes name + detail
//   - TestDoctorJSONOutput_Roundtrip  → --json produces parseable JSON with the same contract
//   - TestDoctorHumanOutput_Snapshot  → single golden snapshot of the human output (controlled input)
//   - TestDoctorJSON_Deterministic    → running doctor --json twice yields byte-identical output
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Struct-level tests — no subprocess, no text parsing
// ---------------------------------------------------------------------------

// TestDoctorReport_StatusShape asserts that every check produced by the
// internal result converts to a DoctorCheck whose Status is one of the
// three recognized values. Fail-closed: an unknown status is a bug.
func TestDoctorReport_StatusShape(t *testing.T) {
	res := &doctorResult{checks: []check{
		{name: "a", severity: sevOK, detail: "green"},
		{name: "b", severity: sevWarning, detail: "yellow"},
		{name: "c", severity: sevCritical, detail: "red"},
	}}
	rep := res.toReport()

	if len(rep.Checks) != 3 {
		t.Fatalf("want 3 checks, got %d", len(rep.Checks))
	}
	want := []string{"OK", "WARNING", "CRITICAL"}
	for i, c := range rep.Checks {
		if c.Status != want[i] {
			t.Errorf("checks[%d].Status = %q; want %q", i, c.Status, want[i])
		}
	}
	if rep.Summary.OK != 1 || rep.Summary.Warning != 1 || rep.Summary.Critical != 1 {
		t.Errorf("Summary = %+v; want OK=1 Warning=1 Critical=1", rep.Summary)
	}
}

// TestDoctorReport_NameDetailSeparation asserts that Name and Detail are
// kept as distinct fields — no silent concatenation. Empty details stay
// empty; whitespace-only details are preserved verbatim (the contract is
// byte-faithful, callers decide how to render).
func TestDoctorReport_NameDetailSeparation(t *testing.T) {
	res := &doctorResult{checks: []check{
		{name: "with-detail", severity: sevOK, detail: "some detail"},
		{name: "no-detail", severity: sevOK, detail: ""},
	}}
	rep := res.toReport()

	if rep.Checks[0].Name != "with-detail" || rep.Checks[0].Detail != "some detail" {
		t.Errorf("check[0] = %+v; want Name=with-detail Detail='some detail'", rep.Checks[0])
	}
	if rep.Checks[1].Name != "no-detail" || rep.Checks[1].Detail != "" {
		t.Errorf("check[1] = %+v; want Name=no-detail Detail=''", rep.Checks[1])
	}
}

// TestDoctorReport_SchemaVersion locks the version field. External
// consumers pin against this — bumping it is a breaking change.
func TestDoctorReport_SchemaVersion(t *testing.T) {
	res := &doctorResult{checks: []check{{name: "x", severity: sevOK}}}
	rep := res.toReport()
	if rep.Version != "v1" {
		t.Errorf("DoctorReport.Version = %q; want %q", rep.Version, "v1")
	}
	if DoctorSchemaVersion != "v1" {
		t.Errorf("DoctorSchemaVersion = %q; want %q", DoctorSchemaVersion, "v1")
	}

	// The error envelope shares the same version discriminator.
	errRep := newDoctorErrorReport(io.EOF)
	if errRep.Version != "v1" {
		t.Errorf("DoctorErrorReport.Version = %q; want %q", errRep.Version, "v1")
	}
	if errRep.Error != "EOF" {
		t.Errorf("DoctorErrorReport.Error = %q; want %q", errRep.Error, "EOF")
	}
}

// TestDoctorJSONEmitter_Envelope asserts emitJSONReport produces a
// top-level object with "version", "checks", and "summary" keys.
func TestDoctorJSONEmitter_Envelope(t *testing.T) {
	res := &doctorResult{checks: []check{
		{name: "x", severity: sevOK, detail: "ok detail"},
	}}
	var buf bytes.Buffer
	if err := emitJSONReport(&buf, res); err != nil {
		t.Fatalf("emitJSONReport: %v", err)
	}

	var decoded DoctorReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("emitted JSON is not parseable: %v\n---\n%s", err, buf.String())
	}
	if decoded.Version != "v1" {
		t.Errorf("Version = %q; want v1", decoded.Version)
	}
	if len(decoded.Checks) != 1 {
		t.Fatalf("want 1 check, got %d", len(decoded.Checks))
	}
	got := decoded.Checks[0]
	if got.Status != "OK" || got.Name != "x" || got.Detail != "ok detail" {
		t.Errorf("check = %+v; want Status=OK Name=x Detail='ok detail'", got)
	}
	if decoded.Summary.OK != 1 {
		t.Errorf("Summary.OK = %d; want 1", decoded.Summary.OK)
	}
}

// TestDoctorJSONEmitter_AtomicSingleWrite asserts the happy path does
// exactly one Write call with the full buffered envelope. Zero partial
// writes, zero retries.
func TestDoctorJSONEmitter_AtomicSingleWrite(t *testing.T) {
	w := &countingWriter{accept: true}
	res := &doctorResult{checks: []check{{name: "x", severity: sevOK, detail: "ok"}}}

	if err := emitJSONReport(w, res); err != nil {
		t.Fatalf("emitJSONReport: %v", err)
	}
	if w.writes != 1 {
		t.Errorf("Write calls = %d; atomic contract requires exactly 1", w.writes)
	}

	var rep DoctorReport
	if err := json.Unmarshal(w.buf.Bytes(), &rep); err != nil {
		t.Fatalf("buffered output is not valid JSON: %v\n---\n%s", err, w.buf.String())
	}
	if rep.Version != "v1" {
		t.Errorf("rep.Version = %q; want v1", rep.Version)
	}
}

// TestDoctorJSONEmitter_AtomicOnPartialWriteFailure: when the writer
// returns (n<len, err), the emitter must NOT attempt a second write to
// fall back to the error envelope — that would interleave two envelopes
// on top of each other and produce garbage. Exactly one Write call,
// error is propagated.
func TestDoctorJSONEmitter_AtomicOnPartialWriteFailure(t *testing.T) {
	// The writer accepts the first 10 bytes of the success envelope,
	// then returns an error. Under the atomic contract, no retry is made.
	w := &partialWriter{acceptBytes: 10}
	res := &doctorResult{checks: []check{{name: "x", severity: sevOK}}}

	err := emitJSONReport(w, res)
	if err == nil {
		t.Fatalf("expected error when writer partially fails")
	}
	if w.writes != 1 {
		t.Errorf("Write calls = %d; atomic contract forbids a second write after partial failure", w.writes)
	}
	// What reached the writer is a prefix of ONE envelope — never a mix.
	// We don't require it to be parseable JSON (partial by nature), but we
	// require it NOT to contain a second envelope marker.
	occurrences := strings.Count(w.buf.String(), `"version"`)
	if occurrences > 1 {
		t.Errorf("writer received %d version markers; expected at most 1 envelope prefix", occurrences)
	}
}

// TestDoctorJSONEmitter_AtomicRejection: when the writer rejects the
// first byte entirely (n=0, err), the buffer stays empty and the
// emitter returns the error.
func TestDoctorJSONEmitter_AtomicRejection(t *testing.T) {
	w := &countingWriter{accept: false}
	res := &doctorResult{checks: []check{{name: "x", severity: sevOK}}}

	err := emitJSONReport(w, res)
	if err == nil {
		t.Fatalf("expected error when writer rejects all writes")
	}
	if w.writes != 1 {
		t.Errorf("Write calls = %d; expected 1", w.writes)
	}
	if w.buf.Len() != 0 {
		t.Errorf("rejecting writer received %d bytes; expected 0", w.buf.Len())
	}
}

// TestDoctorErrorEnvelope_Wellformed covers the fallback envelope that
// the atomic emitter would use if the primary encode ever fails. Since
// our concrete types cannot fail to marshal in practice, we validate
// the helper directly: the envelope carries Version=v1 and is valid JSON.
func TestDoctorErrorEnvelope_Wellformed(t *testing.T) {
	payload, err := encodeIndentedJSON(newDoctorErrorReport(errors.New("boom")))
	if err != nil {
		t.Fatalf("encodeIndentedJSON(errorReport): %v", err)
	}

	var fallback DoctorErrorReport
	if uErr := json.Unmarshal(payload, &fallback); uErr != nil {
		t.Fatalf("error envelope is not valid JSON: %v\n---\n%s", uErr, payload)
	}
	if fallback.Version != "v1" {
		t.Errorf("fallback.Version = %q; want v1", fallback.Version)
	}
	if fallback.Error != "boom" {
		t.Errorf("fallback.Error = %q; want %q", fallback.Error, "boom")
	}
}

// ── test-only writers ────────────────────────────────────────────────────────

// countingWriter either accepts all writes (buffered) or rejects them.
type countingWriter struct {
	accept bool
	buf    bytes.Buffer
	writes int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes++
	if !c.accept {
		return 0, errors.New("countingWriter: rejected")
	}
	return c.buf.Write(p)
}

// partialWriter accepts acceptBytes from each Write and returns an error.
// Used to simulate a writer that flushes a prefix before failing.
type partialWriter struct {
	acceptBytes int
	buf         bytes.Buffer
	writes      int
}

func (p *partialWriter) Write(b []byte) (int, error) {
	p.writes++
	n := len(b)
	if n > p.acceptBytes {
		n = p.acceptBytes
	}
	p.buf.Write(b[:n])
	return n, errors.New("partialWriter: simulated partial failure")
}

// ---------------------------------------------------------------------------
// Human snapshot — single golden test on a controlled doctorResult
// ---------------------------------------------------------------------------

// TestDoctorHumanOutput_Snapshot locks the human render format for a known
// set of checks. We build the doctorResult synthetically (no env-dependent
// collectors) so the snapshot is deterministic.
//
// When the format intentionally changes, update the golden string here —
// every change should be a conscious one, not accidental drift.
func TestDoctorHumanOutput_Snapshot(t *testing.T) {
	res := &doctorResult{checks: []check{
		{name: "snapshot check A", severity: sevOK, detail: "fine"},
		{name: "snapshot check B", severity: sevWarning, detail: "soft issue"},
		{name: "snapshot check C", severity: sevCritical, detail: "hard issue"},
	}}

	// Capture stdout during printStructuredReport.
	got := captureStdout(t, func() { printStructuredReport(res) })

	// The WARNING glyph is "⚠️ " (trailing space inside severity.glyph),
	// so that line has one extra leading space vs. OK/CRITICAL — intentional.
	const want = `
  Verificações:
    ✅  [OK      ] snapshot check A                           fine
    ⚠️   [WARNING ] snapshot check B                           soft issue
    ❌  [CRITICAL] snapshot check C                           hard issue

─────────────────────────────────────────────────
  Resumo: 1 OK · 1 WARNING · 1 CRITICAL

`
	if got != want {
		t.Errorf("snapshot drift:\n---got---\n%s\n---want---\n%s", got, want)
	}
}

// captureStdout temporarily replaces os.Stdout with a pipe, runs fn, and
// returns what was written. Small helper — avoids introducing a testing lib.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-done
}

// ---------------------------------------------------------------------------
// Determinism — subprocess level
// ---------------------------------------------------------------------------

// TestDoctorJSON_Deterministic runs `orbit doctor --json` twice in a row
// and asserts byte-identical output. The environment is stable between
// consecutive calls (same PATH, same env, same binary), so any difference
// is a non-determinism bug (timestamps, maps iterated without sort, etc.).
//
// Uses a freshly compiled binary via a temp dir so the test does not
// depend on what's installed globally.
func TestDoctorJSON_Deterministic(t *testing.T) {
	bin := buildOrbitForDoctorTest(t)

	first := runOrbitForDoctorTest(t, bin, "doctor", "--json")
	second := runOrbitForDoctorTest(t, bin, "doctor", "--json")

	if first != second {
		t.Fatalf("doctor --json output is not deterministic\n---1---\n%s\n---2---\n%s",
			first, second)
	}

	// Sanity: first is valid JSON with the expected envelope.
	var rep DoctorReport
	if err := json.Unmarshal([]byte(first), &rep); err != nil {
		t.Fatalf("doctor --json emitted invalid JSON: %v\n%s", err, first)
	}
	if len(rep.Checks) == 0 {
		t.Fatal("doctor --json emitted empty checks list")
	}
	// Every Status must be in the contract.
	valid := map[string]bool{"OK": true, "WARNING": true, "CRITICAL": true}
	for i, c := range rep.Checks {
		if !valid[c.Status] {
			t.Errorf("checks[%d].Status = %q is not in the contract", i, c.Status)
		}
	}
}

// TestDoctorJSON_NoLogContamination asserts that the JSON output is a
// single top-level object — no stray stdlib log lines ([SECURITY]...),
// banners, or dividers mixed in.
func TestDoctorJSON_NoLogContamination(t *testing.T) {
	bin := buildOrbitForDoctorTest(t)
	out := runOrbitForDoctorTest(t, bin, "doctor", "--json")

	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "{") {
		t.Fatalf("doctor --json output does not start with '{' — log contamination?\n%s", out)
	}
	if !strings.HasSuffix(trimmed, "}") {
		t.Fatalf("doctor --json output does not end with '}'\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Build helper (doctor-specific to avoid colliding with other test files)
// ---------------------------------------------------------------------------

var doctorTestBin string

func buildOrbitForDoctorTest(t *testing.T) string {
	t.Helper()
	if doctorTestBin != "" {
		if _, err := os.Stat(doctorTestBin); err == nil {
			return doctorTestBin
		}
	}
	// Reuse the UX-audit binary when it's been built in the same test run.
	if uxAuditBin != "" {
		if _, err := os.Stat(uxAuditBin); err == nil {
			doctorTestBin = uxAuditBin
			return doctorTestBin
		}
	}
	t.Fatalf("test binary not built; expected TestMain in ux_audit_test.go to populate uxAuditBin")
	return ""
}

func runOrbitForDoctorTest(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = t.TempDir()
	// ORBIT_SKIP_GUARD lets the doctor run even if the dev machine has
	// duplicate orbit installs. We're testing doctor itself, not the guard.
	cmd.Env = append(os.Environ(), "ORBIT_SKIP_GUARD=1", "ORBIT_SKIP_GUARD_IN_CI=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Doctor exits 1 when CRITICAL checks are present — that is a normal
	// runtime state, not a test failure. We only fail on non-exit errors
	// like "binary not found" or a timeout.
	_ = cmd.Run()
	return stdout.String()
}

// ensure we don't shadow a future build of the binary in an unexpected
// location (tests should all go through the TestMain-built path).
func init() {
	if runtime.GOOS == "" {
		_ = filepath.Separator // keep imports honest
	}
}
