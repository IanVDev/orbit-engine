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
	"encoding/json"
	"fmt"
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

// PrintJSON serializa v como JSON indentado e escreve em stdout.
// Retorna error se a serialização falhar.
func PrintJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
