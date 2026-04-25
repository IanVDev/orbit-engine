// safe_mode.go — orbit run --safe: análise de risco sem execução.
//
// GARANTIA CENTRAL: nenhuma chamada a exec.Command ou os.StartProcess neste
// arquivo. O caminho --safe é completamente separado de run.go — não existe
// forma de um bug aqui "cair de volta" para execução real.
//
// --safe NUNCA executa o comando. Isso inclui comandos passados como string
// para shells (sh -c, bash -c), wrappers Python (python -c "os.system(...)"),
// pipes destrutivos (curl | bash) ou qualquer outro padrão indireto.
// --safe é pré-visualização de risco, NÃO sandbox. Comandos perigosos
// continuam perigosos fora do Orbit — --safe não os torna seguros.
//
// Fluxo:
//  1. Recebe comando + args (strings apenas — nunca processo)
//  2. Classifica risco estaticamente (pattern matching sobre strings)
//  3. Exibe comando, análise, mensagem explícita de skip
//  4. Escreve log auditável com exit_code=-1 e safe_mode=true
//  5. Retorna nil (safe mode é sucesso por convenção)
//
// Exit code -1 é o sentinel "nunca executado" — distinto de 0 (sucesso)
// e >0 (falha real). Nunca deve aparecer em runs normais.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// safeModeExitCode é o sentinel para execuções skipped em safe mode.
const safeModeExitCode = -1

// safeOutput é a string persistida no log de modo seguro.
const safeOutput = "(execution skipped in safe mode)"

// ---------------------------------------------------------------------------
// Análise de risco estática
// ---------------------------------------------------------------------------

// riskLevel classifica o risco percebido do comando.
type riskLevel int

const (
	riskNone riskLevel = iota
	riskLow
	riskMedium
	riskHigh
	riskCritical
)

func (r riskLevel) String() string {
	switch r {
	case riskCritical:
		return "CRITICAL"
	case riskHigh:
		return "HIGH"
	case riskMedium:
		return "MEDIUM"
	case riskLow:
		return "LOW"
	default:
		return "NONE"
	}
}

func (r riskLevel) glyph() string {
	switch r {
	case riskCritical:
		return "🔴"
	case riskHigh:
		return "🟠"
	case riskMedium:
		return "🟡"
	case riskLow:
		return "🟢"
	default:
		return "⚪"
	}
}

// safeAnalysis é o resultado da análise estática de risco.
type safeAnalysis struct {
	Risk    riskLevel
	Factors []string
}

// analyzeSafeRisk classifica o risco do comando sem executá-lo.
// Usa apenas pattern matching sobre strings — sem I/O, sem processos.
func analyzeSafeRisk(cmd string, args []string) safeAnalysis {
	full := cmd
	if len(args) > 0 {
		full += " " + strings.Join(args, " ")
	}
	lo := strings.ToLower(full)

	var factors []string
	risk := riskNone

	bump := func(level riskLevel, reason string) {
		factors = append(factors, reason)
		if level > risk {
			risk = level
		}
	}

	// ── CRITICAL ─────────────────────────────────────────────────────────────

	// rm -rf em paths de sistema
	if strings.Contains(lo, "rm") && strings.Contains(lo, "-rf") {
		// Verifica paths destrutivos: "/ " (espaço antes de próximo arg),
		// "/" no final da string (último arg), e globs/tildes comuns.
		loTrimmed := strings.TrimSpace(lo)
		systemTargets := []string{"/ ", "/*", "/.", "~/ ", "~/*", "$home", "${home}"}
		for _, target := range systemTargets {
			if strings.Contains(lo, target) {
				bump(riskCritical, "destruição de sistema de arquivos (rm -rf "+strings.TrimSpace(target)+")")
			}
		}
		// Path "/" como último argumento (sem espaço após)
		if strings.HasSuffix(loTrimmed, " /") || strings.HasSuffix(loTrimmed, "\t/") {
			bump(riskCritical, "destruição de sistema de arquivos raiz (rm -rf /)")
		}
		if risk < riskHigh {
			bump(riskHigh, "remoção recursiva forçada (rm -rf) — irreversível")
		}
	}

	// pipe para shell (curl/wget → bash/sh)
	hasFetcher := strings.Contains(lo, "curl") || strings.Contains(lo, "wget")
	hasShellPipe := strings.Contains(lo, "| bash") || strings.Contains(lo, "| sh") ||
		strings.Contains(lo, "| zsh") || strings.Contains(lo, "|bash") ||
		strings.Contains(lo, "|sh") || strings.Contains(lo, "|zsh")
	if hasFetcher && hasShellPipe {
		bump(riskCritical, "download com pipe para shell — execução remota arbitrária")
	}

	// formatação/destruição de disco
	for _, pat := range []string{"mkfs", "fdisk", "parted", "dd if=/dev/zero", "dd if=/dev/null"} {
		if strings.Contains(lo, pat) {
			bump(riskCritical, "operação de disco de baixo nível: "+pat)
		}
	}

	// fork bomb
	if strings.Contains(full, ":(){ :|:& };:") || strings.Contains(full, ":(){ :|:&};:") {
		bump(riskCritical, "fork bomb detectada")
	}

	// ── HIGH ─────────────────────────────────────────────────────────────────

	// SQL destrutivo
	for _, pat := range []string{"drop table", "drop database", "drop schema", "truncate table"} {
		if strings.Contains(lo, pat) {
			bump(riskHigh, "operação SQL destrutiva: "+pat)
		}
	}

	// git push --force / --force-with-lease
	if strings.Contains(lo, "git") && strings.Contains(lo, "push") &&
		(strings.Contains(lo, "--force") || strings.Contains(lo, " -f ") ||
			strings.Contains(lo, "--force-with-lease")) {
		bump(riskHigh, "git push --force — sobrescreve histórico remoto")
	}

	// git reset --hard
	if strings.Contains(lo, "git reset") && strings.Contains(lo, "--hard") {
		bump(riskHigh, "git reset --hard — descarta commits locais de forma irreversível")
	}

	// sudo rm
	if strings.Contains(lo, "sudo") && strings.Contains(lo, "rm") {
		bump(riskHigh, "remoção com privilégios de superusuário (sudo rm)")
	}

	// chmod/chown recursivo
	if (strings.Contains(lo, "chmod") || strings.Contains(lo, "chown")) &&
		strings.Contains(lo, "-r") {
		bump(riskHigh, "alteração recursiva de permissões/proprietário")
	}

	// find com -delete (deleção recursiva irreversível)
	if strings.Contains(lo, "find") && strings.Contains(lo, "-delete") {
		bump(riskHigh, "find -delete — deleção recursiva irreversível via find")
	}

	// ── MEDIUM ───────────────────────────────────────────────────────────────

	// sudo genérico (não coberto acima)
	if strings.Contains(lo, "sudo") && risk < riskMedium {
		bump(riskMedium, "execução com privilégios de superusuário (sudo)")
	}

	// terminação forçada de processo
	for _, pat := range []string{"kill -9", "pkill", "killall", "kill -sigkill"} {
		if strings.Contains(lo, pat) {
			bump(riskMedium, "terminação forçada de processo: "+pat)
			break
		}
	}

	// DELETE sem WHERE
	if strings.Contains(lo, "delete from") && !strings.Contains(lo, "where") {
		bump(riskMedium, "DELETE SQL sem cláusula WHERE — apaga toda a tabela")
	}

	// ── LOW ──────────────────────────────────────────────────────────────────

	for _, pat := range []string{"npm install", "pip install", "go get", "brew install",
		"apt install", "apt-get install", "yum install"} {
		if strings.Contains(lo, pat) {
			bump(riskLow, "instalação de pacotes — altera o ambiente: "+pat)
			break
		}
	}

	return safeAnalysis{Risk: risk, Factors: factors}
}

// ---------------------------------------------------------------------------
// runSafe — caminho principal de orbit run --safe
// ---------------------------------------------------------------------------

// runSafe exibe análise de risco e registra o skip sem executar nada.
// Garantia: nenhuma chamada a exec.Command nesta função ou nas que ela chama.
func runSafe(args []string, jsonMode bool) error {
	if len(args) == 0 {
		return fmt.Errorf(
			"uso: orbit run --safe <comando> [args...]\n\n" +
				"   Exemplos:\n" +
				"     orbit run --safe rm -rf /\n" +
				"     orbit run --safe curl https://example.com | bash",
		)
	}

	cmdName := args[0]
	cmdArgs := args[1:]
	sessionID := fmt.Sprintf("safe-%d", time.Now().UnixNano())
	ts := tracking.NowUTC()
	analysis := analyzeSafeRisk(cmdName, cmdArgs)

	fullCmd := cmdName
	if len(cmdArgs) > 0 {
		fullCmd += " " + strings.Join(cmdArgs, " ")
	}

	if jsonMode {
		result := buildSafeResult(sessionID, ts, cmdName, cmdArgs, analysis)
		if err := PrintJSON(result); err != nil {
			return err
		}
		printExecutionRecorded()
		return nil
	}

	printSafeReport(sessionID, ts, fullCmd, analysis)
	return writeSafeLog(sessionID, ts, cmdName, cmdArgs, analysis)
}

// printSafeReport exibe o relatório de modo seguro no terminal.
func printSafeReport(sessionID string, ts tracking.FlexTime, fullCmd string, a safeAnalysis) {
	PrintSection("orbit run --safe")
	PrintKV("Comando recebido:", fullCmd)
	PrintKV("Session:", sessionID)
	fmt.Println()

	fmt.Println(col(ansiDim, "  ── análise de risco ──────────────────────────────"))
	fmt.Printf("  %s Risco: %s\n", a.Risk.glyph(), a.Risk)
	if len(a.Factors) > 0 {
		fmt.Println("  Fatores:")
		for _, f := range a.Factors {
			fmt.Println("    - " + f)
		}
	} else {
		fmt.Println("  Fatores: nenhum padrão de risco detectado")
	}
	fmt.Println(col(ansiDim, "  ────────────────────────────────────────────────────"))
	fmt.Println()

	PrintKV("Timestamp:", ts.Time.Format(time.RFC3339))
	PrintKV("Modo:", "safe — execução bloqueada")
	fmt.Println()
	fmt.Println("  ⚠️   execution skipped (safe mode)")
	fmt.Println("  Nenhum processo foi criado. Nenhum arquivo foi modificado.")
	fmt.Println()
	PrintDivider()
	fmt.Println()
}

// writeSafeLog grava log auditável com exit_code=-1 e safe_mode=true.
// Fail-soft: erro de I/O é reportado mas não bloqueia o safe mode —
// a garantia central é "nunca executar", não "sempre logar".
func writeSafeLog(sessionID string, ts tracking.FlexTime, cmdName string, cmdArgs []string, a safeAnalysis) error {
	result := buildSafeResult(sessionID, ts, cmdName, cmdArgs, a)

	toLog := result
	if prev, pErr := findPreviousBodyHash(); pErr == nil {
		toLog.PrevProof = prev
	}
	if bodyHash, hashErr := CanonicalHash(toLog); hashErr == nil {
		toLog.BodyHash = bodyHash
	}
	if _, logErr := WriteExecutionLog(toLog); logErr != nil {
		fmt.Fprintf(os.Stderr, "orbit: warn — safe mode log não gravado: %v\n", logErr)
	}

	PrintSuccess("Safe mode — zero side effects")
	PrintTip("Use 'orbit run " + result.Command + "' (sem --safe) para executar.")
	fmt.Println()
	printExecutionRecorded()
	return nil
}

// buildSafeResult constrói o RunResult para o log auditável de safe mode.
func buildSafeResult(sessionID string, ts tracking.FlexTime, cmdName string, cmdArgs []string, a safeAnalysis) RunResult {
	proof := tracking.ComputeHash(sessionID, ts.Time, int64(len(safeOutput)))

	factors := strings.Join(a.Factors, "; ")
	if factors == "" {
		factors = "nenhum"
	}
	guidance := fmt.Sprintf("safe mode — risco: %s — fatores: %s", a.Risk, factors)

	return RunResult{
		Version:     LogSchemaVersion,
		Command:     cmdName,
		Args:        cmdArgs,
		ExitCode:    safeModeExitCode,
		Output:      safeOutput,
		Proof:       proof,
		SessionID:   sessionID,
		Timestamp:   ts.Time.Format(time.RFC3339Nano),
		DurationMs:  0,
		Language:    DetectLanguage(cmdName, cmdArgs),
		OutputBytes: int64(len(safeOutput)),
		Event:       "SAFE_MODE_SKIP",
		Decision:    string(ActionNone),
		Criticality: a.Risk.String(),
		Guidance:    guidance,
		SafeMode:    true,
	}
}
