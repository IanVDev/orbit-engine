// ui.go — helpers de output para o orbit CLI.
//
// Suporta cores ANSI quando stdout é um terminal (desativadas em pipe/CI).
// Defina NO_COLOR=1 ou TERM=dumb para desativar cores explicitamente.
//
// Funções exportadas:
//
//	PrintSection  — cabeçalho de seção (◆ título + linha)
//	PrintSuccess  — ✅ mensagem de sucesso em verde
//	PrintError    — ❌ mensagem de erro em vermelho (stderr)
//	PrintWarn     — ⚠️  aviso em amarelo
//	PrintTip      — 💡 dica de próximo passo
//	PrintKV       — par chave:valor com alinhamento fixo
//	PrintDivider  — linha separadora sutil
//	PrintJSON     — serializa v como JSON indentado em stdout
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ── ANSI color codes ──────────────────────────────────────────────────────────

const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiCyan   = "\033[36m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
)

// colorEnabled é calculado uma vez na inicialização do processo.
var colorEnabled = detectColor()

// detectColor retorna true se stdout é um terminal interativo sem overrides.
func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// col aplica um código ANSI à string se cores estiverem habilitadas.
func col(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

// ── Funções de UI ─────────────────────────────────────────────────────────────

// PrintSection imprime um cabeçalho de seção com linha decorativa.
//
//	◆  Título
//	─────────────────────────────────────────────────
func PrintSection(title string) {
	fmt.Println()
	fmt.Println(col(ansiBold+ansiCyan, "◆  "+title))
	fmt.Println(col(ansiDim, strings.Repeat("─", 49)))
}

// PrintSuccess imprime uma mensagem de sucesso (verde).
func PrintSuccess(msg string) {
	fmt.Println(col(ansiGreen, "  ✅  ") + msg)
}

// PrintError imprime uma mensagem de erro em stderr (vermelho).
func PrintError(msg string) {
	fmt.Fprintln(os.Stderr, col(ansiRed, "  ❌  ")+msg)
}

// PrintWarn imprime um aviso (amarelo).
func PrintWarn(msg string) {
	fmt.Println(col(ansiYellow, "  ⚠️   ") + msg)
}

// PrintTip imprime uma dica de próximo passo (ciano + dim).
func PrintTip(msg string) {
	fmt.Println(col(ansiCyan, "  💡  ") + col(ansiDim, msg))
}

// PrintKV imprime um par chave:valor com alinhamento fixo de 26 caracteres.
//
//	Chave:                     Valor
func PrintKV(label, value string) {
	fmt.Printf("  %-26s %s\n", col(ansiDim, label), col(ansiBold, value))
}

// PrintDivider imprime uma linha separadora sutil.
func PrintDivider() {
	fmt.Println(col(ansiDim, strings.Repeat("─", 49)))
}

// printActiveHeartbeat emits a single-line status signal to stderr. The
// wording makes the usage model explicit: Orbit does NOT run
// automatically in the background; tracking only happens inside an
// `orbit run <cmd>` invocation. Outside of that, Orbit is idle/ready.
//
// Goes to stderr because it is status, not content — piping stdout to
// a file or to `--json` consumers is unaffected.
//
// Format is stable; changing it is a UX-observable change.
func printActiveHeartbeat() {
	fmt.Fprintln(os.Stderr, col(ansiDim, "orbit: ● ready (use 'orbit run' to track)"))
}

// printTrackingStart emits the "this run is being tracked" status line
// at the top of an `orbit run` invocation. Stderr, so `--json` stdout
// stays clean.
func printTrackingStart() {
	fmt.Fprintln(os.Stderr, col(ansiDim, "orbit: tracking this execution"))
}

// printExecutionRecorded emits the closing status line of an `orbit run`
// invocation, signalling the execution was persisted (proof + tracking
// event). Stderr, same reasoning as printTrackingStart.
func printExecutionRecorded() {
	fmt.Fprintln(os.Stderr, col(ansiDim, "orbit: execution recorded"))
}

// PrintJSON serializa v como JSON indentado e escreve em stdout usando
// o contrato de emissão atômica (ver writeJSONAtomic).
func PrintJSON(v any) error {
	return writeJSONAtomic(os.Stdout, v)
}

// writeJSONAtomic encodes v into an in-memory buffer and performs at most
// one Write call against w. This guarantees:
//
//   - No partial writes from the encoder itself (the encoder targets a
//     bytes.Buffer, which cannot partial-fail).
//   - At most one Write call to w per invocation. If w partial-fails, the
//     error is propagated and no retry is attempted — a second write on a
//     broken writer would risk interleaving.
//   - If encoding v fails (e.g., unsupported type), no bytes are written
//     to w at all; the error is returned.
//
// This is the general-purpose counterpart to emitJSONReport in doctor.go,
// which adds a schema-specific error-envelope fallback on encode failure.
// Callers that need that envelope behaviour use emitJSONReport; everyone
// else uses PrintJSON / writeJSONAtomic.
func writeJSONAtomic(w io.Writer, v any) error {
	buf, err := encodeIndentedJSON(v)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}
