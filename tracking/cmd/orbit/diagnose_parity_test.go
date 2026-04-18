// diagnose_parity_test.go — sentinela da fonte única de verdade.
//
// Afirmamos que `dispatchParser` é a ÚNICA lógica de seleção event→parser,
// usada pelos dois caminhos do sistema:
//
//   - run-path:  BuildDiagnosisForRun (momento do `orbit run`)
//   - slow-path: diagnoseTo em log sem diagnosis persistido (logs antigos)
//
// Este teste prova a afirmação: para cada tupla (event, exit, output),
// os campos diagnósticos produzidos pelos dois caminhos DEVEM ser
// byte-idênticos. Se alguém amanhã inserir lógica em só um dos lados
// (ex.: um early-return, um normalização de mensagem), este teste
// quebra — o dispatcher deixou de ser canônico.
//
// Campos comparados: ErrorType, TestName, File, Line, Message,
// Confidence. LogPath/SnapshotPath/Event/ExitCode ficam fora porque
// são contextuais da leitura (não do parser).
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosisRunSlowPathParity(t *testing.T) {
	cases := []struct {
		name   string
		event  EventType
		exit   int
		output string
	}{
		{
			name:   "TEST_RUN com --- FAIL + file:line:msg (high)",
			event:  EventTestRun,
			exit:   1,
			output: "--- FAIL: TestX (0.00s)\n    x_test.go:42: wrong\nFAIL\n",
		},
		{
			name:   "TEST_RUN só com file:line:msg (medium)",
			event:  EventTestRun,
			exit:   1,
			output: "panic: nil\nmain.go:10: crash inesperado\n",
		},
		{
			name:   "BUILD com coluna (high)",
			event:  EventBuild,
			exit:   1,
			output: "./foo.go:15:2: undefined: bar\n",
		},
		{
			name:   "BUILD sem file:line (fail-closed)",
			event:  EventBuild,
			exit:   1,
			output: "import cycle not allowed\n",
		},
		{
			name:   "TEST_RUN exit 0 (healthy)",
			event:  EventTestRun,
			exit:   0,
			output: "ok pkg 0.003s\n",
		},
		{
			name:   "evento sem parser (CODE_CHANGE)",
			event:  EventCodeChange,
			exit:   1,
			output: "main.go:3: whatever\n",
		},
		{
			name:   "output vazio",
			event:  EventTestRun,
			exit:   1,
			output: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// ── Caminho 1: run-path ──────────────────────────────────
			run := BuildDiagnosisForRun(tc.event, tc.exit, tc.output)

			// ── Caminho 2: slow-path ─────────────────────────────────
			// Grava um log SEM diagnosis persistido, força diagnoseTo
			// a cair no dispatcher.
			dir := t.TempDir()
			logPath := filepath.Join(dir, "no-diag.json")
			rr := RunResult{
				Version:   1,
				Command:   "probe",
				ExitCode:  tc.exit,
				Event:     string(tc.event),
				Output:    tc.output,
				SessionID: "parity",
				Timestamp: "2026-04-18T10:00:00Z",
				// NOTA: Diagnosis deliberadamente nil.
			}
			data, err := json.MarshalIndent(rr, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := os.WriteFile(logPath, data, 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}

			var buf bytes.Buffer
			if err := diagnoseTo(&buf, logPath, true); err != nil {
				t.Fatalf("diagnoseTo: %v", err)
			}
			var slow Diagnosis
			if err := json.Unmarshal(buf.Bytes(), &slow); err != nil {
				t.Fatalf("decode slow-path: %v\n%s", err, buf.String())
			}

			// ── Comparação campo a campo ─────────────────────────────
			if run.Confidence != slow.Confidence {
				t.Errorf("confidence divergiu: run=%q slow=%q",
					run.Confidence, slow.Confidence)
			}
			if run.ErrorType != slow.ErrorType {
				t.Errorf("error_type divergiu: run=%q slow=%q",
					run.ErrorType, slow.ErrorType)
			}
			if run.TestName != slow.TestName {
				t.Errorf("test_name divergiu: run=%q slow=%q",
					run.TestName, slow.TestName)
			}
			if run.File != slow.File {
				t.Errorf("file divergiu: run=%q slow=%q", run.File, slow.File)
			}
			if run.Line != slow.Line {
				t.Errorf("line divergiu: run=%d slow=%d", run.Line, slow.Line)
			}
			if run.Message != slow.Message {
				t.Errorf("message divergiu: run=%q slow=%q",
					run.Message, slow.Message)
			}
		})
	}
}
