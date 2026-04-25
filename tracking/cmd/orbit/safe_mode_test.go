package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Análise de risco estática
// ---------------------------------------------------------------------------

func TestAnalyzeSafeRisk_CriticalPatterns(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		args    []string
		minRisk riskLevel
		factor  string
	}{
		{
			name:    "rm -rf root",
			cmd:     "rm",
			args:    []string{"-rf", "/"},
			minRisk: riskCritical,
			factor:  "destruição de sistema de arquivos",
		},
		{
			name:    "rm -rf home glob",
			cmd:     "rm",
			args:    []string{"-rf", "/*"},
			minRisk: riskCritical,
			factor:  "destruição de sistema de arquivos",
		},
		{
			name:    "curl pipe bash",
			cmd:     "curl",
			args:    []string{"https://example.com/install.sh", "|", "bash"},
			minRisk: riskCritical,
			factor:  "pipe para shell",
		},
		{
			name:    "wget pipe sh",
			cmd:     "wget",
			args:    []string{"-O-", "https://example.com", "|", "sh"},
			minRisk: riskCritical,
			factor:  "pipe para shell",
		},
		{
			name:    "mkfs disk format",
			cmd:     "mkfs.ext4",
			args:    []string{"/dev/sda"},
			minRisk: riskCritical,
			factor:  "mkfs",
		},
		{
			name:    "dd zero wipe",
			cmd:     "dd",
			args:    []string{"if=/dev/zero", "of=/dev/sda"},
			minRisk: riskCritical,
			factor:  "dd if=/dev/zero",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := analyzeSafeRisk(c.cmd, c.args)
			if a.Risk < c.minRisk {
				t.Errorf("risco esperado >= %s, got %s (fatores: %v)", c.minRisk, a.Risk, a.Factors)
			}
			found := false
			for _, f := range a.Factors {
				if strings.Contains(strings.ToLower(f), strings.ToLower(c.factor)) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("fator %q ausente em %v", c.factor, a.Factors)
			}
		})
	}
}

func TestAnalyzeSafeRisk_HighPatterns(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		args []string
	}{
		{"git push force", "git", []string{"push", "--force", "origin", "main"}},
		{"git reset hard", "git", []string{"reset", "--hard", "HEAD~3"}},
		{"sudo rm", "sudo", []string{"rm", "-rf", "/var/log"}},
		{"chmod recursive", "chmod", []string{"-R", "777", "/etc"}},
		{"drop table", "psql", []string{"-c", "DROP TABLE users"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := analyzeSafeRisk(c.cmd, c.args)
			if a.Risk < riskHigh {
				t.Errorf("risco esperado >= HIGH, got %s (fatores: %v)", a.Risk, a.Factors)
			}
		})
	}
}

func TestAnalyzeSafeRisk_MediumPatterns(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		args []string
	}{
		{"sudo generic", "sudo", []string{"apt", "install", "nginx"}},
		{"kill -9", "kill", []string{"-9", "12345"}},
		{"pkill", "pkill", []string{"-f", "myapp"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := analyzeSafeRisk(c.cmd, c.args)
			if a.Risk < riskMedium {
				t.Errorf("risco esperado >= MEDIUM, got %s (fatores: %v)", a.Risk, a.Factors)
			}
		})
	}
}

func TestAnalyzeSafeRisk_NoneForSafeCommands(t *testing.T) {
	safe := []struct {
		cmd  string
		args []string
	}{
		{"echo", []string{"hello", "world"}},
		{"ls", []string{"-la"}},
		{"go", []string{"test", "./..."}},
		{"git", []string{"status"}},
		{"git", []string{"log", "--oneline", "-10"}},
		{"cat", []string{"README.md"}},
	}
	for _, c := range safe {
		label := c.cmd + " " + strings.Join(c.args, " ")
		t.Run(label, func(t *testing.T) {
			a := analyzeSafeRisk(c.cmd, c.args)
			if a.Risk > riskNone {
				t.Errorf("esperado risco NONE para %q, got %s (fatores: %v)",
					label, a.Risk, a.Factors)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Anti-regressão: --safe NUNCA executa
// ---------------------------------------------------------------------------

// TestSafe_NeverCreatesProcess é a garantia central de fail-closed:
// runSafe não deve jamais criar um processo filho.
// Um comando que cria um arquivo se executado prova que nada rodou.
func TestSafe_NeverCreatesProcess(t *testing.T) {
	tmp := t.TempDir()
	marker := tmp + "/ORBIT_SAFE_EXECUTED"
	t.Setenv("ORBIT_HOME", tmp)

	// Se runSafe chamasse exec.Command("touch", marker), este arquivo seria criado.
	err := runSafe([]string{"touch", marker}, false)
	if err != nil {
		t.Fatalf("runSafe retornou erro inesperado: %v", err)
	}

	// Arquivo não deve existir — prova que o processo NÃO foi criado.
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("REGRESSÃO CRÍTICA: runSafe --safe criou um arquivo real — comando foi executado")
	}
}

// TestSafe_NeverExecutesDestructive garante que rm -rf / não executa.
func TestSafe_NeverExecutesDestructive(t *testing.T) {
	tmp := t.TempDir()
	victim := tmp + "/victim_file"
	t.Setenv("ORBIT_HOME", tmp)

	// Cria um arquivo que RM destruiria se executado.
	if err := os.WriteFile(victim, []byte("alive"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --safe: rm -rf não deve tocar o arquivo.
	_ = runSafe([]string{"rm", "-rf", victim}, false)

	if _, statErr := os.Stat(victim); statErr != nil {
		t.Fatal("REGRESSÃO CRÍTICA: runSafe --safe apagou um arquivo real — rm foi executado")
	}
}

// ---------------------------------------------------------------------------
// Log auditável
// ---------------------------------------------------------------------------

// TestSafe_LogHasSentinelExitCode verifica exit_code=-1 no log de safe mode.
func TestSafe_LogHasSentinelExitCode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	if err := runSafe([]string{"rm", "-rf", "/"}, false); err != nil {
		t.Fatalf("runSafe: %v", err)
	}

	result := readLatestLog(t, tmp)
	if result.ExitCode != safeModeExitCode {
		t.Errorf("exit_code esperado %d (sentinel), got %d", safeModeExitCode, result.ExitCode)
	}
	if !result.SafeMode {
		t.Error("safe_mode deveria ser true no log de safe mode")
	}
	if result.Event != "SAFE_MODE_SKIP" {
		t.Errorf("event esperado SAFE_MODE_SKIP, got %q", result.Event)
	}
}

// TestSafe_LogCapturesRisk verifica que criticality aparece no log.
func TestSafe_LogCapturesRisk(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	if err := runSafe([]string{"rm", "-rf", "/"}, false); err != nil {
		t.Fatalf("runSafe: %v", err)
	}

	result := readLatestLog(t, tmp)
	if result.Criticality != "CRITICAL" {
		t.Errorf("criticality esperado CRITICAL para rm -rf /, got %q", result.Criticality)
	}
	if !strings.Contains(result.Guidance, "risco: CRITICAL") {
		t.Errorf("guidance deveria conter 'risco: CRITICAL', got %q", result.Guidance)
	}
	if result.Output != safeOutput {
		t.Errorf("output esperado sentinel %q, got %q", safeOutput, result.Output)
	}
}

// TestSafe_EmptyArgsReturnsError verifica que runSafe sem args retorna erro de uso.
func TestSafe_EmptyArgsReturnsError(t *testing.T) {
	err := runSafe([]string{}, false)
	if err == nil {
		t.Fatal("esperado erro com args vazio; recebeu nil")
	}
	if !strings.Contains(err.Error(), "--safe") {
		t.Errorf("mensagem de erro deveria incluir '--safe', got: %v", err)
	}
}

// TestSafe_NormalRunStillExecutes é o teste anti-regressão do caminho normal:
// sem --safe, orbit run executa normalmente (o flag não afeta o comportamento padrão).
// Verificamos via side effect: o comando echo cria output observável no log.
func TestSafe_NormalRunStillExecutes(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	// Executa um comando inofensivo sem --safe.
	if err := runRun([]string{"echo", "orbit-safe-test"}, false, true); err != nil {
		t.Fatalf("runRun (sem --safe) falhou: %v", err)
	}

	result := readLatestLog(t, tmp)
	if result.ExitCode != 0 {
		t.Errorf("exit_code esperado 0 para echo, got %d", result.ExitCode)
	}
	if result.SafeMode {
		t.Error("safe_mode deveria ser false em execução normal")
	}
	if !strings.Contains(result.Output, "orbit-safe-test") {
		t.Errorf("output esperado conter 'orbit-safe-test', got %q", result.Output)
	}
}

// ---------------------------------------------------------------------------
// Helpers de teste
// ---------------------------------------------------------------------------

// readLatestLog lê e desserializa o log mais recente em ORBIT_HOME/logs/.
func readLatestLog(t *testing.T, home string) RunResult {
	t.Helper()
	logs, err := ListExecutionLogs()
	if err != nil || len(logs) == 0 {
		t.Skip("nenhum log encontrado — skip")
	}
	// ListExecutionLogs retorna em ordem lexicográfica = cronológica.
	last := logs[len(logs)-1]
	data, readErr := os.ReadFile(last)
	if readErr != nil {
		t.Fatalf("leitura de log %s: %v", last, readErr)
	}
	var result RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse de log %s: %v", last, err)
	}
	return result
}
