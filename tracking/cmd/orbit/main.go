// Command orbit — CLI local do orbit-engine.
//
// Subcomandos:
//
//	quickstart    Jornada completa: init → echo hello → proof → verify
//	run           Executa comando externo com geração de proof
//	stats         Tokens processados, execuções e decisões automáticas
//	context-pack  Gera context-pack para transição entre conversas (alias: ctx)
//	doctor        Diagnóstico de instalação e conflitos de PATH
//	version       Versão instalada
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
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// defaultTrackingHost é o endereço padrão do tracking-server em produção.
const defaultTrackingHost = "http://localhost:9100"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	printSessionBanner(os.Args[1])
	enforceStartupIntegrity(os.Args[1])
	printTrustBanner(currentTrustLevel(os.Args[1]))

	switch os.Args[1] {
	case "quickstart":
		fs := flag.NewFlagSet("quickstart", flag.ExitOnError)
		host := fs.String("host", "", "URL do tracking-server (vazio = servidor embutido)")
		_ = fs.Parse(os.Args[2:])
		if err := runQuickstart(*host); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "run":
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		jsonMode := fs.Bool("json", false, "output JSON estruturado em vez de texto")
		_ = fs.Parse(os.Args[2:])
		if err := runRun(fs.Args(), *jsonMode); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "stats":
		fs := flag.NewFlagSet("stats", flag.ExitOnError)
		host := fs.String("host", defaultTrackingHost, "URL do tracking-server")
		share := fs.Bool("share", false, "gerar texto curto para compartilhar resultados")
		_ = fs.Parse(os.Args[2:])
		if err := runStats(*host, *share); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "context-pack", "ctx":
		fs := flag.NewFlagSet("context-pack", flag.ExitOnError)
		auto        := fs.Bool("auto",          false, "modo silencioso para hooks (sem stdout)")
		setObj      := fs.String("set-objective", "", "define o objetivo atual")
		addDecision := fs.String("add-decision",  "", "registra uma decisão tomada")
		addRisk     := fs.String("add-risk",       "", "registra um risco conhecido")
		addNext     := fs.String("add-next",       "", "adiciona próximo passo")
		reset       := fs.Bool("reset",           false, "limpa decisions/risks/next")
		_ = fs.Parse(os.Args[2:])
		if err := runContextPack(*auto, *setObj, *addDecision, *addRisk, *addNext, *reset); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "analyze":
		_ = flag.NewFlagSet("analyze", flag.ExitOnError).Parse(os.Args[2:])
		if err := runAnalyze(); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "hygiene":
		if err := runHygiene(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "❌  %v\n", err)
			os.Exit(1)
		}

	case "doctor":
		fs := flag.NewFlagSet("doctor", flag.ExitOnError)
		strict := fs.Bool("strict", false, "falha com exit 1 se houver WARNINGs")
		fix := fs.Bool("fix", false, "sugere/aplica correções para problemas detectados")
		deep := fs.Bool("deep", false, "diagnóstico profundo: symlinks, wrappers, commit mismatch, origem de texto")
		_ = fs.Parse(os.Args[2:])
		if err := runDoctor(*strict, *fix, *deep); err != nil {
			fmt.Fprintf(os.Stderr, "❌  %v\n", err)
			os.Exit(1)
		}

	case "version":
		fmt.Printf("orbit version %s (commit=%s build=%s)\n", Version, Commit, BuildTime)

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
	fmt.Fprintln(os.Stderr, "  run           Executa comando externo com geração de proof")
	fmt.Fprintln(os.Stderr, "  stats         Tokens processados, execuções e decisões automáticas")
	fmt.Fprintln(os.Stderr, "  analyze       Alerta silencioso: imprime apenas se risco >= HIGH")
	fmt.Fprintln(os.Stderr, "  context-pack  Gera context-pack para transição entre conversas (alias: ctx)")
	fmt.Fprintln(os.Stderr, "  hygiene       Gerencia o pre-commit hook (install|check)")
	fmt.Fprintln(os.Stderr, "  doctor        Diagnóstico de instalação e conflitos de PATH")
	fmt.Fprintln(os.Stderr, "  version       Versão instalada")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --host <url>    URL do tracking-server (default: http://localhost:9100)")
	fmt.Fprintln(os.Stderr, "                  Em quickstart, deixe vazio para usar servidor embutido.")
	fmt.Fprintln(os.Stderr, "  --json          (run) output JSON estruturado")
	fmt.Fprintln(os.Stderr, "  --share         (stats) gerar texto curto para compartilhar resultados")
	fmt.Fprintln(os.Stderr, "  --strict        (doctor) falha com exit 1 se houver WARNINGs")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Exemplos:")
	fmt.Fprintln(os.Stderr, "  orbit quickstart")
	fmt.Fprintln(os.Stderr, "  orbit stats")
	fmt.Fprintln(os.Stderr, "  orbit stats --host http://meu-servidor:9100")
	fmt.Fprintln(os.Stderr, "  orbit doctor")
	fmt.Fprintln(os.Stderr, "  orbit doctor --strict")
}
