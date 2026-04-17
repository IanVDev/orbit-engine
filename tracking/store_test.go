package tracking

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// TestStoreLoopInvariant
//
// ANTI-REGRESSION GATE: se este teste falhar, o loop de valor do orbit está
// quebrado. Cobre as 3 invariantes críticas de persistência em uma única
// rodada fim-a-fim:
//
//   1. APPEND → READ round-trip: cada append vira exatamente um SessionRecord
//      válido e recuperável, com proof preservado.
//   2. CHAIN: prev_proof do registro N é igual ao proof do registro N-1.
//      Reordenar ou editar uma linha deve ser detectado como ChainBreak.
//   3. FAIL-SOFT em read: linha corrompida é pulada e contada, mas a leitura
//      completa (TotalRecords) reflete apenas registros válidos.
//
// Usa ORBIT_HOME isolado via t.TempDir() — não mexe no ~/.orbit do usuário.
// -----------------------------------------------------------------------------

func TestStoreLoopInvariant(t *testing.T) {
	// Isolate: ORBIT_HOME → dir temporário privado.
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	// --- 1. Append 3 records and verify round-trip + chain ---

	rec1, err := AppendSessionRecord(SessionRecord{
		SessionID:   "sess-001",
		Timestamp:   NowUTC(),
		Command:     "echo",
		Args:        []string{"hello"},
		ExitCode:    0,
		OutputBytes: 6,
		Proof:       ComputeHash("sess-001", NowUTC().Time, 6),
	})
	if err != nil {
		t.Fatalf("append rec1 failed: %v", err)
	}
	if rec1.PrevProof != "" {
		t.Fatalf("genesis record must have empty prev_proof, got %q", rec1.PrevProof)
	}

	rec2, err := AppendSessionRecord(SessionRecord{
		SessionID:   "sess-002",
		Timestamp:   NowUTC(),
		Command:     "ls",
		Args:        []string{"-la"},
		ExitCode:    0,
		OutputBytes: 256,
		Proof:       ComputeHash("sess-002", NowUTC().Time, 256),
	})
	if err != nil {
		t.Fatalf("append rec2 failed: %v", err)
	}
	if rec2.PrevProof != rec1.Proof {
		t.Fatalf("chain broken between rec1 and rec2: prev_proof=%q want %q",
			rec2.PrevProof, rec1.Proof)
	}

	rec3, err := AppendSessionRecord(SessionRecord{
		SessionID:   "sess-003",
		Timestamp:   NowUTC(),
		Command:     "go",
		Args:        []string{"test", "./..."},
		ExitCode:    1,
		OutputBytes: 1024,
		Proof:       ComputeHash("sess-003", NowUTC().Time, 1024),
	})
	if err != nil {
		t.Fatalf("append rec3 failed: %v", err)
	}
	if rec3.PrevProof != rec2.Proof {
		t.Fatalf("chain broken between rec2 and rec3")
	}

	// --- 2. Read-all and verify aggregation ---

	path := filepath.Join(tmp, "sessions.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read back JSONL: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if got := len(lines); got != 3 {
		t.Fatalf("expected 3 lines in JSONL, got %d", got)
	}

	stats, err := ReadStats(10)
	if err != nil {
		t.Fatalf("ReadStats failed: %v", err)
	}
	if stats.TotalRecords != 3 {
		t.Fatalf("TotalRecords=%d, want 3", stats.TotalRecords)
	}
	if stats.Successful != 2 { // rec1, rec2 com exit=0; rec3 exit=1
		t.Fatalf("Successful=%d, want 2", stats.Successful)
	}
	if stats.OutputBytesTotal != 6+256+1024 {
		t.Fatalf("OutputBytesTotal=%d, want %d", stats.OutputBytesTotal, 6+256+1024)
	}
	if stats.ChainBreaks != 0 {
		t.Fatalf("ChainBreaks=%d, want 0 (clean chain)", stats.ChainBreaks)
	}
	if stats.Corrupted != 0 {
		t.Fatalf("Corrupted=%d, want 0", stats.Corrupted)
	}
	if len(stats.Last) != 3 {
		t.Fatalf("Last len=%d, want 3", len(stats.Last))
	}
	if stats.Last[2].Proof != rec3.Proof {
		t.Fatalf("Last[2].Proof mismatch: got %q want %q", stats.Last[2].Proof, rec3.Proof)
	}

	// --- 3. FAIL-SOFT: inject a corrupted line + verify it's skipped ---

	// Corrompe acrescentando uma linha inválida ao arquivo.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if _, err := f.WriteString("{ this is not valid json\n"); err != nil {
		t.Fatalf("write corrupted line failed: %v", err)
	}
	_ = f.Close()

	statsCorrupt, err := ReadStats(10)
	if err != nil {
		t.Fatalf("ReadStats must be fail-soft, got error: %v", err)
	}
	if statsCorrupt.TotalRecords != 3 {
		t.Fatalf("after corruption TotalRecords=%d, want 3 (valid only)",
			statsCorrupt.TotalRecords)
	}
	if statsCorrupt.Corrupted != 1 {
		t.Fatalf("Corrupted=%d, want 1 (exactly one bad line)", statsCorrupt.Corrupted)
	}

	// --- 4. CHAIN BREAK detection: tamper with middle record ---

	// Reescreve o arquivo com rec2.prev_proof alterado → chain quebrada.
	tampered := bytes.Buffer{}
	var r1, r2, r3 SessionRecord
	if err := json.Unmarshal([]byte(lines[0]), &r1); err != nil {
		t.Fatalf("unmarshal lines[0]: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &r2); err != nil {
		t.Fatalf("unmarshal lines[1]: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[2]), &r3); err != nil {
		t.Fatalf("unmarshal lines[2]: %v", err)
	}
	r2.PrevProof = "deadbeef" // tamper
	for _, rr := range []SessionRecord{r1, r2, r3} {
		b, _ := json.Marshal(rr)
		tampered.Write(b)
		tampered.WriteByte('\n')
	}
	if err := os.WriteFile(path, tampered.Bytes(), 0o600); err != nil {
		t.Fatalf("rewrite tampered: %v", err)
	}

	statsTamper, err := ReadStats(10)
	if err != nil {
		t.Fatalf("ReadStats on tampered must be fail-soft, got: %v", err)
	}
	if statsTamper.ChainBreaks < 1 {
		t.Fatalf("ChainBreaks=%d, want >=1 (tamper must be detected)",
			statsTamper.ChainBreaks)
	}

	// --- 5. FAIL-CLOSED on write: invalid record must NOT be persisted ---

	sizeBefore, _ := os.Stat(path)
	_, err = AppendSessionRecord(SessionRecord{
		// missing SessionID, Command, Proof — must fail Validate
		Timestamp: NowUTC(),
	})
	if err == nil {
		t.Fatalf("AppendSessionRecord must fail-closed on invalid record")
	}
	sizeAfter, _ := os.Stat(path)
	if sizeBefore.Size() != sizeAfter.Size() {
		t.Fatalf("file grew after invalid append: before=%d after=%d (must be unchanged)",
			sizeBefore.Size(), sizeAfter.Size())
	}
}
