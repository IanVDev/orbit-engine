// run.go — executa um comando externo com geração de proof de execução.
//
// Uso: orbit run [--json] <comando> [args...]
//
// Fluxo:
//  1. Cria session_id único baseado em timestamp
//  2. Executa o comando via exec.Command, capturando stdout e stderr
//  3. Computa proof via ComputeHash(sessionID, timestamp, outputBytes)
//  4. Exibe: output do comando → métricas → proof → próximo passo
//
// Fail-closed:
//   - Comando retorna exit != 0  → error retornado → caller faz os.Exit(1)
//   - Comando não encontrado     → error retornado → caller faz os.Exit(1)
//
// Flag --json: emite JSON estruturado em vez de texto colorido.
// Útil para integração com scripts e pipelines.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// RunResult é o resultado estruturado de um orbit run.
// Serializado como JSON quando --json está ativo e como log por-execução
// em $ORBIT_HOME/logs/.
type RunResult struct {
	Version      int               `json:"version"`
	Command      string            `json:"command"`
	Args         []string          `json:"args,omitempty"`
	ExitCode     int               `json:"exit_code"`
	Output       string            `json:"output"`
	Proof        string            `json:"proof"`
	SessionID    string            `json:"session_id"`
	Timestamp    string            `json:"timestamp"`
	DurationMs   int64             `json:"duration_ms"`
	Language     string            `json:"language"`
	OutputBytes  int64             `json:"output_bytes"`
	Event        string            `json:"event"`
	Decision     string            `json:"decision"`
	Reason       string            `json:"decision_reason,omitempty"`
	Criticality  string            `json:"criticality,omitempty"`
	SnapshotPath string            `json:"snapshot_path,omitempty"`
	Guidance     string            `json:"guidance,omitempty"`
	Diagnosis    *DiagnosisPayload `json:"diagnosis,omitempty"`
}

// runRun executa o comando fornecido e exibe o resultado com proof.
// Se jsonMode==true, emite JSON estruturado em vez de texto colorido.
func runRun(args []string, jsonMode bool) error {
	if err := enforceRuntimePathIntegrity(); err != nil {
		return err
	}

	if len(args) == 0 {
		return fmt.Errorf(
			"uso: orbit run [--json] <comando> [args...]\n\n" +
				"   Exemplos:\n" +
				"     orbit run echo hello world\n" +
				"     orbit run --json ls -la\n" +
				"     orbit run go test ./...",
		)
	}

	cmdName := args[0]
	cmdArgs := args[1:]

	sessionID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	ts := tracking.NowUTC()

	printActiveHeartbeat()
	printTrackingStart()

	if !jsonMode {
		PrintSection("orbit run")
		fullCmd := cmdName
		if len(cmdArgs) > 0 {
			fullCmd += " " + strings.Join(cmdArgs, " ")
		}
		PrintKV("Comando:", fullCmd)
		PrintKV("Session:", sessionID)
		fmt.Println()
	}

	// ── Execução ──────────────────────────────────────────────────────────

	var stdout, stderr bytes.Buffer
	c := exec.Command(cmdName, cmdArgs...)
	c.Stdout = &stdout
	c.Stderr = &stderr

	startedAt := time.Now()
	runErr := c.Run()
	durationMs := time.Since(startedAt).Milliseconds()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			// Processo executou mas retornou exit != 0.
			exitCode = exitErr.ExitCode()
		} else {
			// Falha ao lançar o processo (não encontrado, permissão, etc.)
			return fmt.Errorf(
				"falha ao executar %q: %w\n\n"+
					"   Verifique se o comando existe no PATH.",
				cmdName, runErr,
			)
		}
	}

	// Combina stdout + stderr na mesma saída (reflete o que o usuário veria).
	output := stdout.String()
	if stderr.Len() > 0 {
		output += stderr.String()
	}
	outputBytes := int64(len(output))

	// ── Proof ─────────────────────────────────────────────────────────────
	// proof = sha256(sessionID + timestamp + outputBytes)
	// IMPORTANTE: calculado sobre o len ORIGINAL, antes da redação (I12).
	// Redaction altera o texto persistido mas não `outputBytes` → I2 e I3
	// permanecem consistentes com o verify.
	proof := tracking.ComputeHash(sessionID, ts.Time, outputBytes)

	// I12 SECRET_SAFETY: redige secrets conhecidos antes de persistir.
	// Aplica em output E em args (secret em argv também é vetor comum).
	// Fail-closed: remover qualquer linha quebra TestFailsIfSecretIsPersisted.
	output = tracking.RedactSecrets(output)
	redactedArgs := make([]string, len(cmdArgs))
	for i, a := range cmdArgs {
		redactedArgs[i] = tracking.RedactSecrets(a)
	}

	// ── Montagem do resultado ─────────────────────────────────────────────

	// ── Decision engine (MVP) ─────────────────────────────────────────────
	// Classifica o comando e avalia a próxima ação. Fail-closed: se a
	// classificação não reconhecer o comando, Decide retorna ActionNone
	// e o fluxo original de orbit run segue inalterado.
	event := ClassifyCommand(cmdName, cmdArgs)
	decision := Decide(event, exitCode)
	criticality := ComputeCriticality(event, exitCode)
	guidance := BuildGuidance(event, exitCode, output)
	diagPayload := BuildDiagnosisForRun(event, exitCode, output).ToPayload()

	// Snapshot só é tomado quando a decisão pede — fail-soft: erro de git
	// não derruba o run; só marca incomplete dentro do próprio snapshot.
	snapshotPath := ""
	if decision.Action == ActionTriggerSnapshot || decision.Action == ActionTriggerAnalyze {
		if p, err := TakeSnapshot(sessionID, decision.Reason); err == nil {
			snapshotPath = p
		}
	}

	result := RunResult{
		Version:      LogSchemaVersion,
		Command:      cmdName,
		Args:         redactedArgs,
		ExitCode:     exitCode,
		Output:       output,
		Proof:        proof,
		SessionID:    sessionID,
		Timestamp:    ts.Time.Format(time.RFC3339Nano),
		DurationMs:   durationMs,
		Language:     DetectLanguage(cmdName, cmdArgs),
		OutputBytes:  outputBytes,
		Event:        string(event),
		Decision:     string(decision.Action),
		Reason:       decision.Reason,
		Criticality:  string(criticality),
		SnapshotPath: snapshotPath,
		Guidance:     guidance,
		Diagnosis:    diagPayload,
	}

	// Persistência append-only em $ORBIT_HOME/logs/. Fail-closed: erro é
	// reportado via stderr, mas não altera o exit code do comando executado.
	if _, logErr := WriteExecutionLog(result); logErr != nil {
		fmt.Fprintf(os.Stderr, "orbit: warning — log não persistido: %v\n", logErr)
	}

	// I15 HISTORY_ANCHOR: atualiza o snapshot fora de ~/.orbit após run
	// bem-sucedido. Erro é tolerado (não derruba o run) mas avisado — o
	// teste TestFailsOnHistoryWipe quebra se esta linha desaparecer.
	if err := tracking.SaveAnchor(proof, ts.Time.Format(time.RFC3339Nano)); err != nil {
		fmt.Fprintf(os.Stderr, "orbit: warning — anchor não atualizado: %v\n", err)
	}

	if jsonMode {
		// Emit the closing status line AFTER the JSON body so stderr
		// shows: heartbeat → tracking → [JSON on stdout] → recorded.
		if err := PrintJSON(result); err != nil {
			return err
		}
		printExecutionRecorded()
		return nil
	}

	// ── Output text ───────────────────────────────────────────────────────

	fmt.Println(col(ansiDim, "  ── output ──────────────────────────────────────────"))
	if output != "" {
		for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
			fmt.Println("  " + line)
		}
	} else {
		fmt.Println(col(ansiDim, "  (sem output)"))
	}
	fmt.Println(col(ansiDim, "  ─────────────────────────────────────────────────────"))
	fmt.Println()

	// Contexto → resultado → significado → próximo passo
	PrintKV("Exit code:", fmt.Sprintf("%d", exitCode))
	PrintKV("Output bytes:", fmt.Sprintf("%d", outputBytes))
	PrintKV("Proof (sha256):", proof[:16]+"...")
	fmt.Println("  ✨ proof generated")
	PrintKV("Session:", sessionID)
	PrintKV("Timestamp:", ts.Time.Format(time.RFC3339))
	PrintKV("Event:", string(event))
	PrintKV("Decision:", string(decision.Action))
	if criticality != CriticalityNone {
		PrintKV("Criticality:", string(criticality))
	}
	if snapshotPath != "" {
		PrintKV("Snapshot:", snapshotPath)
	}
	if guidance != "" {
		PrintKV("Guidance:", guidance)
	}
	if decision.Action != ActionNone {
		PrintTip("Decision: " + decision.Reason)
	}
	PrintDivider()
	fmt.Println()

	if exitCode != 0 {
		PrintError(fmt.Sprintf("Comando retornou exit %d", exitCode))
		PrintTip("Verifique o output acima para detalhes do erro.")
		fmt.Println()
		// Even a failed exec is tracked with a proof — emit the closing
		// status so the narrative (tracking → recorded) stays intact.
		printExecutionRecorded()
		return fmt.Errorf("comando %q retornou exit code %d", cmdName, exitCode)
	}

	PrintSuccess("Comando concluído com sucesso (exit 0)")
	PrintTip("Proof registrado — use 'orbit stats' para ver métricas de execução.")
	maybePrintRunFirstRunTip()
	fmt.Println()
	printExecutionRecorded()
	return nil
}

// runFirstMarkerName is the sentinel file (inside $ORBIT_HOME) whose
// absence signals that this is the first successful `orbit run` on this
// machine. It is touched the first time and never read again.
const runFirstMarkerName = ".run-first-done"

// maybePrintRunFirstRunTip prints an onboarding hint after the user's
// very first successful `orbit run`. Idempotent: after the first run, a
// marker file in $ORBIT_HOME prevents the hint from reappearing.
//
// Fail-soft: any error resolving the home dir or writing the marker is
// ignored — a missing hint is a UX regression, not a correctness issue.
func maybePrintRunFirstRunTip() {
	home, err := tracking.ResolveStoreHome()
	if err != nil {
		return
	}
	marker := filepath.Join(home, runFirstMarkerName)
	if _, statErr := os.Stat(marker); statErr == nil {
		return // not the first run
	}
	// Emit the hint and persist the marker. MkdirAll is required because
	// ResolveStoreHome only resolves the path; the directory may not exist
	// yet on a clean user install.
	PrintTip("Primeira execução — veja também 'orbit stats' e 'orbit analyze'.")
	if mkErr := os.MkdirAll(home, 0o700); mkErr != nil {
		return
	}
	_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}
