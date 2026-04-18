// verify_test.go — contratos do `orbit verify`.
//
// Cobre os 4 ramos do contrato fail-closed:
//
//  1. log válido (proof bate)                       → sucesso silencioso (exit 0)
//  2. log adulterado (output_bytes mexido)          → erro de mismatch
//  3. log com campo essencial ausente               → erro de schema
//  4. arquivo inexistente                           → erro de I/O
//
// O quinto contrato (timestamp inválido) é coberto pelo decode + parse.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// helperWriteLog escreve um log RunResult-compatível com proof correto.
// Devolve o caminho do arquivo e o RunResult serializado.
func helperWriteLog(t *testing.T, dir string) (string, RunResult) {
	t.Helper()

	sessionID := "ab12cd34"
	ts := time.Date(2026, 4, 18, 10, 30, 45, 123456789, time.UTC)
	outputBytes := int64(42)
	proof := tracking.ComputeHash(sessionID, ts, outputBytes)

	rec := RunResult{
		Version:     1,
		Command:     "echo",
		Args:        []string{"hello"},
		ExitCode:    0,
		Output:      "hello world output 42 bytes long aaaaaaa",
		Proof:       proof,
		SessionID:   sessionID,
		Timestamp:   ts.Format(time.RFC3339Nano),
		DurationMs:  3,
		Language:    "other",
		OutputBytes: outputBytes,
		Event:       "UNKNOWN",
		Decision:    "NONE",
	}

	path := filepath.Join(dir, "valid.json")
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path, rec
}

func TestVerify_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path, _ := helperWriteLog(t, dir)

	var buf bytes.Buffer
	if err := verifyTo(&buf, path); err != nil {
		t.Fatalf("verifyTo: erro inesperado: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "✅") {
		t.Errorf("esperava marca de sucesso na saída: %q", out)
	}
	if !strings.Contains(out, "proof confere") {
		t.Errorf("esperava 'proof confere': %q", out)
	}
}

func TestVerify_DetectsTamperedOutputBytes(t *testing.T) {
	dir := t.TempDir()
	path, rec := helperWriteLog(t, dir)

	// Adulteração silenciosa: alguém edita o log a mão e muda o tamanho
	// do output sem recomputar o proof.
	rec.OutputBytes = 999
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	var buf bytes.Buffer
	err := verifyTo(&buf, path)
	if err == nil {
		t.Fatalf("esperava erro de mismatch, obteve sucesso. Saída: %q", buf.String())
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("erro deveria mencionar 'mismatch': %v", err)
	}
	if !strings.Contains(buf.String(), "❌") {
		t.Errorf("esperava marca de falha na saída: %q", buf.String())
	}
}

func TestVerify_DetectsTamperedSessionID(t *testing.T) {
	dir := t.TempDir()
	path, rec := helperWriteLog(t, dir)

	rec.SessionID = "ffffffff"
	data, _ := json.Marshal(rec)
	_ = os.WriteFile(path, data, 0o644)

	var buf bytes.Buffer
	if err := verifyTo(&buf, path); err == nil {
		t.Fatalf("esperava erro com session_id alterado")
	}
}

func TestVerify_RejectsMissingProof(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-proof.json")
	body := `{"session_id":"abc","timestamp":"2026-04-18T10:00:00Z","output_bytes":1}`
	_ = os.WriteFile(path, []byte(body), 0o644)

	var buf bytes.Buffer
	err := verifyTo(&buf, path)
	if err == nil {
		t.Fatalf("esperava erro de campo essencial")
	}
	if !strings.Contains(err.Error(), "proof") {
		t.Errorf("erro deveria mencionar 'proof': %v", err)
	}
}

func TestVerify_RejectsMissingSessionID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-session.json")
	body := `{"proof":"deadbeef","timestamp":"2026-04-18T10:00:00Z","output_bytes":1}`
	_ = os.WriteFile(path, []byte(body), 0o644)

	var buf bytes.Buffer
	if err := verifyTo(&buf, path); err == nil {
		t.Fatalf("esperava erro de session_id ausente")
	}
}

func TestVerify_RejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	_ = os.WriteFile(path, []byte("{not json"), 0o644)

	var buf bytes.Buffer
	err := verifyTo(&buf, path)
	if err == nil {
		t.Fatalf("esperava erro de JSON inválido")
	}
}

func TestVerify_RejectsMissingFile(t *testing.T) {
	var buf bytes.Buffer
	err := verifyTo(&buf, "/tmp/orbit-verify-nonexistent-xyz.json")
	if err == nil {
		t.Fatalf("esperava erro de arquivo inexistente")
	}
}

func TestVerify_RejectsEmptyPath(t *testing.T) {
	var buf bytes.Buffer
	if err := verifyTo(&buf, ""); err == nil {
		t.Fatalf("esperava erro de caminho vazio (fail-closed)")
	}
}

func TestVerify_RejectsInvalidTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-ts.json")
	body := `{"proof":"deadbeef","session_id":"abc","timestamp":"not-a-timestamp","output_bytes":1}`
	_ = os.WriteFile(path, []byte(body), 0o644)

	var buf bytes.Buffer
	err := verifyTo(&buf, path)
	if err == nil {
		t.Fatalf("esperava erro de timestamp inválido")
	}
	if !strings.Contains(err.Error(), "timestamp") {
		t.Errorf("erro deveria mencionar 'timestamp': %v", err)
	}
}
