package main
// Command orbit — CLI local do orbit-engine.
//
// Subcomandos:
//
//	quickstart   Jornada completa: init → echo hello → proof → verify
//	stats        Tokens processados, execuções e decisões automáticas
//	version      Versão instalada
//
// Fail-closed: qualquer erro retorna exit 1.
package main

import (
	"flag"
	"fmt"
	"os"
)

// Build-time variables — injetadas via -ldflags.
//
//	go build -ldflags "-X main.Version=1.0.0 -X main.Commit=abc1234" ./cmd/orbit
var (
	Version = "dev"
	Commit  = "unknown"
)

// defaultTrackingHost é o endereço padrão do tracking-server em produção.
const defaultTrackingHost = "http://localhost:9100"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "quickstart":
		fs := flag.NewFlagSet("quickstart", flag.ExitOnError)
		host := fs.String("host", "", "URL do tracking-server (vazio = servidor embutido)")
		_ = fs.Parse(os.Args[2:])
		if err := runQuickstart(*host); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "stats":
		fs := flag.NewFlagSet("stats", flag.ExitOnError)
		host := fs.String("host", defaultTrackingHost, "URL do tracking-server")
		_ = fs.Parse(os.Args[2:])
		if err := runStats(*host); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "version":
		fmt.Printf("orbit version %s (commit=%s)\n", Version, Commit)

	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "orbit: comando desconhecido %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "uso: orbit <comando> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Comandos:")
	fmt.Fprintln(os.Stderr, "  quickstart    Jornada completa: init → run → proof → verify")
	fmt.Fprintln(os.Stderr, "  stats         Tokens processados, execuções e decisões automáticas")
	fmt.Fprintln(os.Stderr, "  version       Versão instalada")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --host <url>  URL do tracking-server (default: http://localhost:9100)")
	fmt.Fprintln(os.Stderr, "                Em quickstart, deixe vazio para usar servidor embutido.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Exemplos:")
	fmt.Fprintln(os.Stderr, "  orbit quickstart")
	fmt.Fprintln(os.Stderr, "  orbit stats")
	fmt.Fprintln(os.Stderr, "  orbit stats --host http://meu-servidor:9100")
}
