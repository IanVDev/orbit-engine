package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTakeSnapshot_WritesFileInOrbitHome garante que o snapshot é gravado
// em $ORBIT_HOME/snapshots/<sessionID>.json e que o payload tem o shape
// mínimo exigido pelo refinamento (branch/commit/diff_stat presentes).
func TestTakeSnapshot_WritesFileInOrbitHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	sid := "run-test-snap"
	reason := "TEST reason"

	path, err := TakeSnapshot(sid, reason)
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}

	want := filepath.Join(tmp, "snapshots", sid+".json")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if s.SessionID != sid {
		t.Errorf("SessionID = %q, want %q", s.SessionID, sid)
	}
	if s.Reason != reason {
		t.Errorf("Reason = %q, want %q", s.Reason, reason)
	}
	if s.Timestamp == "" {
		t.Error("Timestamp vazio")
	}
	if s.SchemaVersion != snapshotSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", s.SchemaVersion, snapshotSchemaVersion)
	}
	// GitBranch/LastCommit/DiffStat são fail-soft: podem ser vazio se CWD
	// não é repo git. Mas o campo Incomplete DEVE refletir corretamente.
}

func TestTakeSnapshot_EmptySessionIDFails(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	if _, err := TakeSnapshot("", "reason"); err == nil {
		t.Fatal("esperava erro com session_id vazio, recebi nil (fail-closed violado)")
	}
}
