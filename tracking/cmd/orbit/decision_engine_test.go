// decision_engine_test.go — teste anti-regressão do MVP do Orbit
// Decision Engine.
//
// Cobre duas responsabilidades em uma suite unificada para manter o
// footprint mínimo:
//
//  1. Classificação: `git commit` → CODE_CHANGE, `go test` → TEST_RUN,
//     comandos desconhecidos → UNKNOWN.
//  2. Decisão: TEST_RUN com exit != 0 → TRIGGER_ANALYZE; CODE_CHANGE →
//     TRIGGER_SNAPSHOT; caminhos neutros (exit 0 em TEST_RUN, evento
//     UNKNOWN) → NONE, preservando o comportamento original de
//     `orbit run`.
package main

import "testing"

func TestDecisionEngineMVP(t *testing.T) {
	cases := []struct {
		name       string
		cmd        string
		args       []string
		exitCode   int
		wantEvent  EventType
		wantAction ActionType
	}{
		{
			name:       "git commit dispara snapshot",
			cmd:        "git",
			args:       []string{"commit", "-m", "wip"},
			exitCode:   0,
			wantEvent:  EventCodeChange,
			wantAction: ActionTriggerSnapshot,
		},
		{
			name:       "go test falhando dispara analyze",
			cmd:        "go",
			args:       []string{"test", "./..."},
			exitCode:   1,
			wantEvent:  EventTestRun,
			wantAction: ActionTriggerAnalyze,
		},
		{
			name:       "go test passando nao dispara acao",
			cmd:        "go",
			args:       []string{"test", "./..."},
			exitCode:   0,
			wantEvent:  EventTestRun,
			wantAction: ActionNone,
		},
		{
			name:       "comando desconhecido preserva comportamento atual",
			cmd:        "echo",
			args:       []string{"hello"},
			exitCode:   0,
			wantEvent:  EventUnknown,
			wantAction: ActionNone,
		},
		{
			name:       "git status nao e code change",
			cmd:        "git",
			args:       []string{"status"},
			exitCode:   0,
			wantEvent:  EventUnknown,
			wantAction: ActionNone,
		},
		{
			name:       "fail-closed: cmd vazio",
			cmd:        "",
			args:       nil,
			exitCode:   0,
			wantEvent:  EventUnknown,
			wantAction: ActionNone,
		},
		{
			name:       "git merge dispara snapshot (CODE_MERGE)",
			cmd:        "git",
			args:       []string{"merge", "feature"},
			exitCode:   0,
			wantEvent:  EventCodeMerge,
			wantAction: ActionTriggerSnapshot,
		},
		{
			name:       "git rebase dispara snapshot (CODE_MERGE)",
			cmd:        "git",
			args:       []string{"rebase", "main"},
			exitCode:   0,
			wantEvent:  EventCodeMerge,
			wantAction: ActionTriggerSnapshot,
		},
		{
			name:       "pytest falhando dispara analyze",
			cmd:        "pytest",
			args:       []string{"tests/"},
			exitCode:   1,
			wantEvent:  EventTestRun,
			wantAction: ActionTriggerAnalyze,
		},
		{
			name:       "npm test passando nao dispara acao",
			cmd:        "npm",
			args:       []string{"test"},
			exitCode:   0,
			wantEvent:  EventTestRun,
			wantAction: ActionNone,
		},
		{
			name:       "cargo test falhando dispara analyze",
			cmd:        "cargo",
			args:       []string{"test"},
			exitCode:   1,
			wantEvent:  EventTestRun,
			wantAction: ActionTriggerAnalyze,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotEvent := ClassifyCommand(tc.cmd, tc.args)
			if gotEvent != tc.wantEvent {
				t.Fatalf("ClassifyCommand(%q, %v) = %q, want %q",
					tc.cmd, tc.args, gotEvent, tc.wantEvent)
			}

			got := Decide(gotEvent, tc.exitCode)
			if got.Action != tc.wantAction {
				t.Fatalf("Decide(%q, %d) action = %q, want %q (reason=%q)",
					gotEvent, tc.exitCode, got.Action, tc.wantAction, got.Reason)
			}
			if got.Reason == "" {
				t.Errorf("Decide(%q, %d) retornou Reason vazio — logs ficariam sem justificativa",
					gotEvent, tc.exitCode)
			}
		})
	}
}

// TestDecideFailClosedEmptyEvent documenta o contrato fail-closed:
// evento vazio nunca dispara ação, independente do exit code.
func TestDecideFailClosedEmptyEvent(t *testing.T) {
	for _, exit := range []int{0, 1, 137} {
		d := Decide("", exit)
		if d.Action != ActionNone {
			t.Errorf("Decide(\"\", %d) = %q, want NONE (fail-closed)", exit, d.Action)
		}
	}
}
