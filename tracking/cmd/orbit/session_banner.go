// session_banner.go — mensagem leve de "início de sessão" do CLI.
//
// Imprime uma única linha em stderr quando o usuário executa `orbit` de forma
// interativa. Silencioso para subcomandos não-interativos (hooks, JSON,
// `version`/`help`) e quando stderr não é um TTY — evita poluir pipelines.
package main

import (
	"fmt"
	"os"
)

// sessionBannerText é a mensagem exibida. Mantida curta por design.
const sessionBannerText = "orbit: monitoring session..."

// bannerSkipCommands lista subcomandos que não devem disparar o banner.
var bannerSkipCommands = map[string]struct{}{
	"version": {}, "--version": {}, "-v": {},
	"help": {}, "--help": {}, "-h": {},
	"analyze":      {}, // silêncio é contrato do próprio comando
	"context-pack": {}, // pode rodar em --auto dentro de hooks
	"ctx":          {},
}

// printSessionBanner decide se deve emitir a mensagem.
func printSessionBanner(subcommand string) {
	if _, skip := bannerSkipCommands[subcommand]; skip {
		return
	}
	if !stderrIsTTY() {
		return
	}
	fmt.Fprintln(os.Stderr, sessionBannerText)
}

func stderrIsTTY() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
