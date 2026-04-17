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
	"os/exec"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// RunResult é o resultado estruturado de um orbit run.
// Serializado como JSON quando --json está ativo.
type RunResult struct {
	Command     string   `json:"command"`
	Args        []string `json:"args,omitempty"`
	ExitCode    int      `json:"exit_code"`
	Output      string   `json:"output"`
	Proof       string   `json:"proof"`
	SessionID   string   `json:"session_id"`
	Timestamp   string   `json:"timestamp"`
	OutputBytes int64    `json:"output_bytes"`
}

// runRun executa o comando fornecido e exibe o resultado com proof.
// Se jsonMode==true, emite JSON estruturado em vez de texto colorido.
func runRun(args []string, jsonMode bool) error {
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

	runErr := c.Run()

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
	proof := tracking.ComputeHash(sessionID, ts.Time, outputBytes)

	// ── Montagem do resultado ─────────────────────────────────────────────

	result := RunResult{
		Command:     cmdName,
		Args:        cmdArgs,
		ExitCode:    exitCode,
		Output:      output,
		Proof:       proof,
		SessionID:   sessionID,
		Timestamp:   ts.Time.Format(time.RFC3339),
		OutputBytes: outputBytes,
	}

	if jsonMode {
		return PrintJSON(result)
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
	PrintKV("Session:", sessionID)
	PrintKV("Timestamp:", ts.Time.Format(time.RFC3339))
	PrintDivider()
	fmt.Println()

	if exitCode != 0 {
		PrintError(fmt.Sprintf("Comando retornou exit %d", exitCode))
		PrintTip("Verifique o output acima para detalhes do erro.")
		fmt.Println()
		return fmt.Errorf("comando %q retornou exit code %d", cmdName, exitCode)
	}

	PrintSuccess("Comando concluído com sucesso (exit 0)")
	PrintTip("Proof registrado — use 'orbit stats' para ver métricas de execução.")
	fmt.Println()
	return nil
}
