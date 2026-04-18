// diagnose_test.go — anti-regressão do parser de `orbit diagnose`.
//
// Cobre o contrato fail-closed em 5 ramos:
//
//  1. go test falhando com `--- FAIL` + file:line      → confidence=high
//  2. go test falhando só com file:line                → confidence=medium
//  3. TEST_RUN passando (exit 0)                       → confidence=none
//  4. Evento não suportado (CODE_CHANGE)               → confidence=none
//  5. Saída livre sem padrão reconhecido               → confidence=none
//
// O teste #5 é o guardião: se o parser começar a inventar file:line de
// texto arbitrário, este cenário falha.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempLog(t *testing.T, dir string, r RunResult) string {
	t.Helper()
	path := filepath.Join(dir, "test-log.json")
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// ── Parser unitário (sem IO) ─────────────────────────────────────────

func TestParseGoTestFailure_HighConfidence(t *testing.T) {
	out := `=== RUN   TestFoo
--- FAIL: TestFoo (0.00s)
    foo_test.go:42: expected 5, got 4
FAIL
FAIL    example.com/pkg    0.003s
`
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoTestFailure(&d, out)

	if d.Confidence != ConfidenceHigh {
		t.Fatalf("confidence = %q, want high", d.Confidence)
	}
	if d.TestName != "TestFoo" {
		t.Errorf("test_name = %q, want TestFoo", d.TestName)
	}
	if d.File != "foo_test.go" {
		t.Errorf("file = %q, want foo_test.go", d.File)
	}
	if d.Line != 42 {
		t.Errorf("line = %d, want 42", d.Line)
	}
	if !strings.Contains(d.Message, "expected 5") {
		t.Errorf("message não contém assertion: %q", d.Message)
	}
	if d.ErrorType != "go_test_assertion" {
		t.Errorf("error_type = %q, want go_test_assertion", d.ErrorType)
	}
}

func TestParseGoTestFailure_MediumConfidence(t *testing.T) {
	// file:line presente, mas sem `--- FAIL` — situação ambígua, parser
	// deve assumir medium em vez de inventar nome de teste.
	out := `panic: runtime error
main.go:17: nil map access
`
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoTestFailure(&d, out)

	if d.Confidence != ConfidenceMedium {
		t.Fatalf("confidence = %q, want medium", d.Confidence)
	}
	if d.TestName != "" {
		t.Errorf("test_name deveria estar vazio em medium, foi %q", d.TestName)
	}
	if d.File != "main.go" || d.Line != 17 {
		t.Errorf("file:line errados: %s:%d", d.File, d.Line)
	}
	if d.ErrorType != "file_line_only" {
		t.Errorf("error_type = %q, want file_line_only", d.ErrorType)
	}
}

func TestParseGoTestFailure_NoPattern_FailsClosed(t *testing.T) {
	// Texto sem file.go:line em lugar nenhum. Parser DEVE cala —
	// nunca inventar file/line a partir de conteúdo arbitrário.
	out := `Error: connection refused to redis at 127.0.0.1:6379
Retry attempt 3 of 5
Giving up.
`
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoTestFailure(&d, out)

	if d.Confidence != ConfidenceNone {
		t.Fatalf("confidence = %q, want none (fail-closed)", d.Confidence)
	}
	if d.File != "" || d.Line != 0 || d.Message != "" {
		t.Errorf("campos deveriam estar vazios: file=%q line=%d msg=%q",
			d.File, d.Line, d.Message)
	}
}

func TestParseGoTestFailure_EmptyOutput(t *testing.T) {
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoTestFailure(&d, "")
	if d.Confidence != ConfidenceNone {
		t.Errorf("output vazio não deve dar confidence — got %q", d.Confidence)
	}
}

// ── Integração: diagnoseTo(log_file) ─────────────────────────────────

func TestDiagnoseTo_HighConfidenceFromFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLog(t, dir, RunResult{
		Version:      1,
		Command:      "go",
		Args:         []string{"test", "./..."},
		ExitCode:     1,
		Event:        string(EventTestRun),
		Output:       "--- FAIL: TestBar (0.00s)\n    bar_test.go:7: oops got 1\nFAIL\n",
		Proof:        "deadbeef",
		SessionID:    "abcd1234",
		Timestamp:    "2026-04-18T10:00:00Z",
		SnapshotPath: "/tmp/fake-snap.json",
	})

	var buf bytes.Buffer
	if err := diagnoseTo(&buf, path, true); err != nil {
		t.Fatalf("diagnoseTo: %v", err)
	}

	var d Diagnosis
	if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
		t.Fatalf("decode JSON: %v — raw=%s", err, buf.String())
	}
	if d.Confidence != ConfidenceHigh {
		t.Errorf("confidence = %q, want high", d.Confidence)
	}
	if d.TestName != "TestBar" || d.File != "bar_test.go" || d.Line != 7 {
		t.Errorf("campos incorretos: test=%q file=%q line=%d",
			d.TestName, d.File, d.Line)
	}
	if d.SnapshotPath != "/tmp/fake-snap.json" {
		t.Errorf("snapshot_path não propagou: %q", d.SnapshotPath)
	}
	if d.Version != DiagnoseSchemaVersion {
		t.Errorf("version ausente/errada: %d", d.Version)
	}
}

func TestDiagnoseTo_ExitZero_NoDiagnosis(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLog(t, dir, RunResult{
		Version:   1,
		Command:   "go",
		Args:      []string{"test", "./..."},
		ExitCode:  0,
		Event:     string(EventTestRun),
		Output:    "ok  example.com/pkg  0.003s\n",
		SessionID: "abcd1234",
		Timestamp: "2026-04-18T10:00:00Z",
	})

	var buf bytes.Buffer
	if err := diagnoseTo(&buf, path, true); err != nil {
		t.Fatalf("diagnoseTo: %v", err)
	}

	var d Diagnosis
	_ = json.Unmarshal(buf.Bytes(), &d)
	if d.Confidence != ConfidenceNone {
		t.Errorf("exit 0 deveria dar confidence=none, got %q", d.Confidence)
	}
	if d.File != "" || d.TestName != "" {
		t.Errorf("não deveria popular campos em exit 0: %+v", d)
	}
}

func TestDiagnoseTo_UnsupportedEvent_NoDiagnosis(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLog(t, dir, RunResult{
		Version:   1,
		Command:   "git",
		Args:      []string{"commit", "-m", "x"},
		ExitCode:  1,
		Event:     string(EventCodeChange),
		Output:    "some_file.go:10: whatever",
		SessionID: "abcd1234",
		Timestamp: "2026-04-18T10:00:00Z",
	})

	var buf bytes.Buffer
	_ = diagnoseTo(&buf, path, true)

	var d Diagnosis
	_ = json.Unmarshal(buf.Bytes(), &d)
	if d.Confidence != ConfidenceNone {
		t.Errorf("evento %s não suportado deveria dar confidence=none, got %q",
			d.Event, d.Confidence)
	}
	// file:line existia no output, mas como evento não é TEST_RUN, parser
	// não deve ter rodado — fail-closed por escopo.
	if d.File != "" {
		t.Errorf("parser rodou em evento não-suportado: file=%q", d.File)
	}
}

func TestDiagnoseTo_HumanOutput_HasBlock(t *testing.T) {
	dir := t.TempDir()
	path := writeTempLog(t, dir, RunResult{
		Version:   1,
		Command:   "go",
		Args:      []string{"test", "./..."},
		ExitCode:  1,
		Event:     string(EventTestRun),
		Output:    "--- FAIL: TestHello\n    hello_test.go:3: oops\nFAIL\n",
		SessionID: "abcd1234",
		Timestamp: "2026-04-18T10:00:00Z",
	})

	var buf bytes.Buffer
	if err := diagnoseTo(&buf, path, false); err != nil {
		t.Fatalf("diagnoseTo human: %v", err)
	}
	out := buf.String()
	for _, expect := range []string{
		"orbit diagnose",
		"event: TEST_RUN",
		"error_type: go_test_assertion",
		"test:       TestHello",
		"at:         hello_test.go:3",
		"confidence: high",
	} {
		if !strings.Contains(out, expect) {
			t.Errorf("output humano não contém %q:\n%s", expect, out)
		}
	}
}

func TestDiagnoseTo_MissingFile_ReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := diagnoseTo(&buf, "/tmp/orbit-diagnose-does-not-exist.json", true)
	if err == nil {
		t.Fatalf("esperava erro para arquivo inexistente")
	}
}
