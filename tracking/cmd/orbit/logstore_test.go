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
