// diagnose_convergence_test.go — guardiões da unificação entre
// guidance.go e diagnose.go.
//
// Contratos protegidos:
//
//  1. CONVERGÊNCIA: para o mesmo output de `go test` falho, a string
//     devolvida por BuildGuidance == "file:line" extraído pelo parser
//     de diagnose. Se alguém reintroduzir regex paralela em guidance,
//     este teste quebra.
//
//  2. PERSISTÊNCIA: `orbit run` de um `go test` falho grava um
//     Diagnosis no campo `diagnosis` do log (RunResult.Diagnosis).
//     Garante que dashboard/CLI não precisem re-parsear.
//
//  3. FAST-PATH: `orbit diagnose` de um log que JÁ contém `diagnosis`
//     retorna exatamente os campos persistidos, sem chamar o parser
//     novamente. Protegido via um sentinela: se o parser rodasse, o
//     output seria reconstituído a partir do RunResult.Output, que
//     neste teste é deliberadamente vazio.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── #1 — Convergência ────────────────────────────────────────────────

func TestGuidanceAndDiagnoseAgreeOnLocation(t *testing.T) {
	cases := []struct {
		name   string
		output string
	}{
		{
			"go test com --- FAIL + file:line:msg",
			"--- FAIL: TestAlpha (0.00s)\n    alpha_test.go:7: expected 1, got 2\nFAIL\n",
		},
		{
			"go test só com file:line:msg (medium)",
			"panic: runtime error\nmain.go:19: nil map access\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guidance := BuildGuidance(EventTestRun, 1, tc.output)
			if guidance == "" {
				t.Fatalf("guidance vazio — parser falhou para output válido")
			}

			d := BuildDiagnosisForRun(EventTestRun, 1, tc.output)
			if d.Confidence == ConfidenceNone {
				t.Fatalf("diagnose também falhou — teste não está exercendo match")
			}

			want := fmt.Sprintf("%s:%d", d.File, d.Line)
			if guidance != want {
				t.Fatalf("CONVERGÊNCIA QUEBRADA\n"+
					"  guidance: %q\n"+
					"  diagnose: %q (file=%s line=%d)\n"+
					"Se você reintroduziu regex paralela em guidance.go, consolide.",
					guidance, want, d.File, d.Line)
			}
		})
	}
}

// ── #2 — Persistência no log via run.go ──────────────────────────────
//
// Este teste roda o binário real em um tempdir isolado, produzindo um
// log de go test falho, e verifica que o log traz `diagnosis` inline.

func TestRunPersistsDiagnosisInLog(t *testing.T) {
	binary := buildOrbitBinary(t)

	tmp := t.TempDir()
	proj := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeFile(t, filepath.Join(proj, "go.mod"),
		"module example.com/proj\n\ngo 1.21\n")
	writeFile(t, filepath.Join(proj, "fail_test.go"),
		"package proj\nimport \"testing\"\n"+
			"func TestPersistMe(t *testing.T){ t.Fatalf(\"forced failure\") }\n")

	runOrbitIn(t, binary, tmp, proj, "run", "--", "go", "test", "./...")

	logPath := latestLogIn(t, filepath.Join(tmp, "logs"))

	var rr RunResult
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ler log: %v", err)
	}
	if err := json.Unmarshal(raw, &rr); err != nil {
		t.Fatalf("decode log: %v\n%s", err, raw)
	}

	if rr.Diagnosis == nil {
		t.Fatalf("log não persistiu diagnosis:\n%s", raw)
	}
	if rr.Diagnosis.Confidence != ConfidenceHigh {
		t.Errorf("confidence = %q, want high", rr.Diagnosis.Confidence)
	}
	if rr.Diagnosis.TestName != "TestPersistMe" {
		t.Errorf("test_name = %q, want TestPersistMe", rr.Diagnosis.TestName)
	}
	if !strings.HasSuffix(rr.Diagnosis.File, "fail_test.go") {
		t.Errorf("file = %q, want */fail_test.go", rr.Diagnosis.File)
	}
	if rr.Diagnosis.Line == 0 {
		t.Errorf("line = 0 — não capturou linha")
	}
}

// ── #3 — Fast-path: log com diagnosis é retornado sem re-parse ──────

func TestDiagnoseFastPathUsesPersisted(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "custom.json")

	// Sentinela: Output VAZIO. Se o parser rodar, não encontra nada e
	// o resultado viria como confidence=none. Apenas o fast-path
	// consegue devolver "high" aqui — é o único caminho que não depende
	// de re-parsear Output.
	rr := RunResult{
		Version:   1,
		Command:   "go",
		ExitCode:  1,
		Event:     string(EventTestRun),
		Output:    "",
		SessionID: "fastpath",
		Timestamp: "2026-04-18T10:00:00Z",
		Diagnosis: &DiagnosisPayload{
			Version:    DiagnoseSchemaVersion,
			ErrorType:  "go_test_assertion",
			TestName:   "TestPersisted",
			File:       "persisted_test.go",
			Line:       99,
			Message:    "já diagnosticado no run",
			Confidence: ConfidenceHigh,
		},
	}
	data, _ := json.MarshalIndent(rr, "", "  ")
	if err := os.WriteFile(logPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := diagnoseTo(&buf, logPath, true); err != nil {
		t.Fatalf("diagnoseTo: %v", err)
	}

	var d Diagnosis
	if err := json.Unmarshal(buf.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}

	if d.Confidence != ConfidenceHigh {
		t.Fatalf("confidence = %q, want high (fast-path deveria trazer do payload)",
			d.Confidence)
	}
	if d.TestName != "TestPersisted" {
		t.Errorf("test_name = %q, want TestPersisted", d.TestName)
	}
	if d.File != "persisted_test.go" || d.Line != 99 {
		t.Errorf("file:line errados: %s:%d", d.File, d.Line)
	}
	if d.Message != "já diagnosticado no run" {
		t.Errorf("message veio de parse, não do payload: %q", d.Message)
	}
}

// ── Slow-path: log sem diagnosis ainda funciona (retrocompat) ───────

func TestDiagnoseSlowPathRecomputesWhenPayloadAbsent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "legacy.json")

	// Log "antigo": sem campo diagnosis. Parser deve rodar no output.
	rr := RunResult{
		Version:   1,
		Command:   "go",
		ExitCode:  1,
		Event:     string(EventTestRun),
		Output:    "--- FAIL: TestLegacy\n    legacy_test.go:3: old\nFAIL\n",
		SessionID: "slowpath",
		Timestamp: "2026-04-18T10:00:00Z",
	}
	data, _ := json.MarshalIndent(rr, "", "  ")
	_ = os.WriteFile(logPath, data, 0o644)

	var buf bytes.Buffer
	if err := diagnoseTo(&buf, logPath, true); err != nil {
		t.Fatalf("diagnoseTo: %v", err)
	}

	var d Diagnosis
	_ = json.Unmarshal(buf.Bytes(), &d)

	if d.Confidence != ConfidenceHigh {
		t.Fatalf("slow-path falhou: confidence=%q", d.Confidence)
	}
	if d.TestName != "TestLegacy" {
		t.Errorf("test_name = %q, want TestLegacy", d.TestName)
	}
}

// ── Helpers exclusivos deste arquivo ─────────────────────────────────

func buildOrbitBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "orbit-persist-bin")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = mustCwd(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return binary
}

func runOrbitIn(t *testing.T, binary, orbitHome, workdir string, args ...string) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"ORBIT_HOME="+orbitHome,
		"ORBIT_SKIP_GUARD=1",
	)
	// `orbit run go test ./...` vai sair com exit!=0 (teste falho) —
	// não quebramos aqui, é exatamente o que queremos capturar.
	_, _ = cmd.CombinedOutput()
}

func latestLogIn(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	var newest os.DirEntry
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if newest == nil || e.Name() > newest.Name() {
			newest = e
		}
	}
	if newest == nil {
		t.Fatalf("nenhum log em %s", dir)
	}
	return filepath.Join(dir, newest.Name())
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustCwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return cwd
}
