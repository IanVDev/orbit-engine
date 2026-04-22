package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// filenameRe replica a regex do parser Python para garantir que o nome
// gerado passa a validação do dashboard.
var filenameRe = regexp.MustCompile(`_([0-9a-f]{8})_exit\d+\.json$`)

func TestWriteExecutionLog_PersistsAndMatchesParserPattern(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	result := RunResult{
		Version:     LogSchemaVersion,
		Command:     "echo",
		Args:        []string{"hi"},
		ExitCode:    0,
		Output:      "hi\n",
		Proof:       "deadbeef",
		SessionID:   "run-1234567890",
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		DurationMs:  5,
		Language:    "other",
		OutputBytes: 3,
		Event:       string(EventUnknown),
		Decision:    string(ActionNone),
		Reason:      "teste",
	}

	path, err := WriteExecutionLog(result)
	if err != nil {
		t.Fatalf("WriteExecutionLog: %v", err)
	}
	if !strings.HasPrefix(path, filepath.Join(tmp, "logs")) {
		t.Fatalf("path fora de logs/: %s", path)
	}
	if !filenameRe.MatchString(path) {
		t.Fatalf("nome do arquivo %q não casa com regex do parser", filepath.Base(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var round RunResult
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.SessionID != result.SessionID {
		t.Errorf("SessionID ida-e-volta: got %q, want %q", round.SessionID, result.SessionID)
	}
	if round.Version != LogSchemaVersion {
		t.Errorf("Version ida-e-volta: got %d, want %d", round.Version, LogSchemaVersion)
	}
}

func TestWriteExecutionLog_FailsOnMissingSessionID(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	_, err := WriteExecutionLog(RunResult{Timestamp: time.Now().UTC().Format(time.RFC3339Nano)})
	if err == nil {
		t.Fatal("esperava erro com session_id vazio (fail-closed)")
	}
}

func TestWriteExecutionLog_FailsOnMissingTimestamp(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	_, err := WriteExecutionLog(RunResult{SessionID: "x"})
	if err == nil {
		t.Fatal("esperava erro com timestamp vazio (fail-closed)")
	}
}

// TestWriteExecutionLog_FailsWhenLogsDirIsFile simula FS quebrado: o
// caminho $ORBIT_HOME/logs já existe como arquivo, impedindo MkdirAll.
// WriteExecutionLog deve retornar erro (fail-closed), e o caller (run.go)
// escala para CRITICAL.
func TestWriteExecutionLog_FailsWhenLogsDirIsFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	// Cria "logs" como arquivo regular — MkdirAll deve falhar.
	if err := os.WriteFile(filepath.Join(tmp, "logs"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	result := RunResult{
		Version:   LogSchemaVersion,
		SessionID: "run-x",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := WriteExecutionLog(result); err == nil {
		t.Fatal("esperava erro quando logs/ é arquivo (fail-closed)")
	}
}

// ---------------------------------------------------------------------------
// VerifyExecutionLog: defesa em profundidade pós-escrita
// ---------------------------------------------------------------------------

func writeValidLog(t *testing.T) (string, RunResult) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)
	r := RunResult{
		Version:   LogSchemaVersion,
		Command:   "echo",
		ExitCode:  0,
		SessionID: "run-verify-1",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Language:  "other",
	}
	path, err := WriteExecutionLog(r)
	if err != nil {
		t.Fatalf("WriteExecutionLog: %v", err)
	}
	return path, r
}

func TestVerifyExecutionLog_HappyPath(t *testing.T) {
	path, r := writeValidLog(t)
	if err := VerifyExecutionLog(path, r); err != nil {
		t.Fatalf("VerifyExecutionLog: %v", err)
	}
}

func TestVerifyExecutionLog_MissingFile(t *testing.T) {
	r := RunResult{SessionID: "x", Timestamp: "t", Version: 1}
	if err := VerifyExecutionLog(filepath.Join(t.TempDir(), "nope.json"), r); err == nil {
		t.Fatal("esperava erro para arquivo ausente")
	}
}

func TestVerifyExecutionLog_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.json")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	r := RunResult{SessionID: "x", Timestamp: "t", Version: 1}
	if err := VerifyExecutionLog(path, r); err == nil {
		t.Fatal("esperava erro para arquivo vazio")
	}
}

func TestVerifyExecutionLog_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := RunResult{SessionID: "x", Timestamp: "t", Version: 1}
	if err := VerifyExecutionLog(path, r); err == nil {
		t.Fatal("esperava erro para JSON inválido")
	}
}

func TestVerifyExecutionLog_SessionMismatch(t *testing.T) {
	path, r := writeValidLog(t)
	r.SessionID = "run-OUTRO"
	if err := VerifyExecutionLog(path, r); err == nil {
		t.Fatal("esperava erro quando session_id diverge")
	}
}

func TestVerifyExecutionLog_ExitCodeMismatch(t *testing.T) {
	path, r := writeValidLog(t)
	r.ExitCode = 99
	if err := VerifyExecutionLog(path, r); err == nil {
		t.Fatal("esperava erro quando exit_code diverge")
	}
}

func TestVerifyExecutionLog_VersionZeroRejected(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "v0.json")
	// version=0 não é valida — nunca deveria existir em disco.
	payload := `{"version":0,"session_id":"s","timestamp":"t","exit_code":0}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	r := RunResult{SessionID: "s", Timestamp: "t", ExitCode: 0}
	if err := VerifyExecutionLog(path, r); err == nil {
		t.Fatal("esperava erro para version=0")
	}
}
