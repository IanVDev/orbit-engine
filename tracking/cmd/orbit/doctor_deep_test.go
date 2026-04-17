package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctorDeep_CommitMismatchAndWrapperFailClosed cobre duas regras
// fail-closed do --deep:
//
//  1. Binário ativo é um script shell (wrapper) → CRITICAL
//  2. Commit baked != commit reportado pelo binário no PATH → CRITICAL
//
// Ambos são simulados via binário fake gravado em tempdir, sem tocar em
// nada do ambiente real.
func TestDoctorDeep_CommitMismatchAndWrapperFailClosed(t *testing.T) {
	tmp := t.TempDir()
	fake := filepath.Join(tmp, "orbit")
	script := "#!/bin/sh\n" +
		"echo 'orbit version dev (commit=DIFFERENT build=x)'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}

	// --- caso 1: wrapper script ---
	t.Run("wrapper script → CRITICAL", func(t *testing.T) {
		res := &doctorResult{currentBinary: fake}
		checkWrapperScript(res)
		if len(res.checks) != 1 {
			t.Fatalf("esperava 1 check, obteve %d", len(res.checks))
		}
		if res.checks[0].severity != sevCritical {
			t.Errorf("esperava CRITICAL para shebang, obteve %s", res.checks[0].severity.tag())
		}
		if !strings.Contains(res.checks[0].detail, "script") {
			t.Errorf("detail deveria citar script: %q", res.checks[0].detail)
		}
	})

	// --- caso 2: commit mismatch ---
	t.Run("commit mismatch → CRITICAL", func(t *testing.T) {
		orig := Commit
		Commit = "SELF_ABC"
		t.Cleanup(func() { Commit = orig })

		res := &doctorResult{currentBinary: fake}
		checkCommitMismatch(res)
		if len(res.checks) != 1 {
			t.Fatalf("esperava 1 check, obteve %d: %+v", len(res.checks), res.checks)
		}
		if res.checks[0].severity != sevCritical {
			t.Errorf("esperava CRITICAL, obteve %s (detail=%q)",
				res.checks[0].severity.tag(), res.checks[0].detail)
		}
		if !strings.Contains(res.checks[0].detail, "SELF_ABC") ||
			!strings.Contains(res.checks[0].detail, "DIFFERENT") {
			t.Errorf("detail deveria citar ambos os commits: %q", res.checks[0].detail)
		}
	})

	// --- caso 3: commit igual → OK ---
	t.Run("commit match → OK", func(t *testing.T) {
		matchingFake := filepath.Join(tmp, "orbit_match")
		ms := "#!/bin/sh\necho 'orbit version 1.0 (commit=SAME build=x)'\n"
		if err := os.WriteFile(matchingFake, []byte(ms), 0o755); err != nil {
			t.Fatal(err)
		}
		orig := Commit
		Commit = "SAME"
		t.Cleanup(func() { Commit = orig })

		res := &doctorResult{currentBinary: matchingFake}
		checkCommitMismatch(res)
		if res.checks[0].severity != sevOK {
			t.Errorf("esperava OK para commits iguais, obteve %s", res.checks[0].severity.tag())
		}
	})

	// --- caso 4: upgrade de duplicatas WARNING→CRITICAL sob --deep ---
	t.Run("duplicates upgraded to CRITICAL under --deep", func(t *testing.T) {
		res := &doctorResult{checks: []check{{
			name:     "Binários orbit únicos",
			severity: sevWarning,
			detail:   "2 encontrados",
		}}}
		upgradeDuplicatesToCritical(res)
		if res.checks[0].severity != sevCritical {
			t.Errorf("esperava CRITICAL após upgrade, obteve %s",
				res.checks[0].severity.tag())
		}
	})

	// --- caso 5: extractCommit parser ---
	t.Run("extractCommit parses standard format", func(t *testing.T) {
		got := extractCommit("orbit version 1.2.3 (commit=abc1234 build=2026-04-16)")
		if got != "abc1234" {
			t.Errorf("esperava 'abc1234', obteve %q", got)
		}
		if extractCommit("no commit here") != "" {
			t.Error("esperava vazio quando ausente")
		}
	})
}
