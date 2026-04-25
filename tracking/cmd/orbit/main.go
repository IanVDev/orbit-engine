// Command orbit — CLI local do orbit-engine.
//
// Subcomandos:
//
//	quickstart    Jornada completa: init → echo hello → proof → verify
//	run           Executa comando externo com geração de proof
//	stats         Tokens processados, execuções e decisões automáticas
//	context-pack  Gera context-pack para transição entre conversas (alias: ctx)
//	git           Subcomandos git (verify-merge)
//	doctor        Diagnóstico de instalação e conflitos de PATH
//	verify        Re-valida o proof SHA256 de um log de execução
//	diagnose      Analisa o último log e extrai causa provável da falha
//	update        Atualiza o binário orbit via GitHub Releases
//	version       Versão instalada
//
// Fail-closed: qualquer erro retorna exit 1.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
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
	enforceHistoryAnchor(os.Args[1])
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
		jsonMode  := fs.Bool("json",       false, "output JSON estruturado em vez de texto")
		noSpinner := fs.Bool("no-spinner", false, "desativa spinner de progresso (automático em pipes/CI)")
		safe      := fs.Bool("safe",       false, "simula execução: exibe análise de risco sem executar nada")
		_ = fs.Parse(os.Args[2:])
		if *safe {
			if err := runSafe(fs.Args(), *jsonMode); err != nil {
				fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if err := runRun(fs.Args(), *jsonMode, *noSpinner); err != nil {
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

	case "git":
		if err := runGitSubcmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "❌  %v\n", err)
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
		jsonOut := fs.Bool("json", false, "emite relatório estruturado em JSON (suprime saída humana)")
		alertOnly := fs.Bool("alert-only", false, "silencioso: imprime apenas blocos para risco >= HIGH (substitui `orbit analyze`)")
		security := fs.Bool("security", false, "checklist de segurança para exposição pública (sempre strict)")
		_ = fs.Parse(os.Args[2:])
		if *security {
			if err := runDoctorSecurity(*jsonOut); err != nil {
				if !*jsonOut {
					fmt.Fprintf(os.Stderr, "❌  %v\n", err)
				}
				os.Exit(1)
			}
			return
		}
		if err := runDoctorWithMode(*strict, *fix, *deep, *jsonOut, *alertOnly); err != nil {
			if !*jsonOut {
				fmt.Fprintf(os.Stderr, "❌  %v\n", err)
			}
			os.Exit(1)
		}

	case "verify":
		fs := flag.NewFlagSet("verify", flag.ExitOnError)
		chain := fs.Bool("chain", false, "valida a chain inteira em $ORBIT_HOME/logs/ (I18)")
		_ = fs.Parse(os.Args[2:])
		if *chain {
			if err := runVerifyChain(os.Stdout); err != nil {
				fmt.Fprintf(os.Stderr, "\n❌  %v\n", err)
				os.Exit(1)
			}
			return
		}
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "uso: orbit verify <log_file>")
			fmt.Fprintln(os.Stderr, "     orbit verify --chain   (valida todos os logs)")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Exemplo:")
			fmt.Fprintln(os.Stderr, "  orbit verify ~/.orbit/logs/2026-04-18T01-12-12.341818001Z_c37e3217_exit0.json")
			os.Exit(1)
		}
		if err := runVerify(fs.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  %v\n", err)
			os.Exit(1)
		}

	case "diagnose":
		fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
		jsonOut := fs.Bool("json", false, "emite JSON estruturado em vez de texto")
		_ = fs.Parse(os.Args[2:])
		logArg := ""
		if fs.NArg() > 0 {
			logArg = fs.Arg(0)
		}
		if err := runDiagnose(logArg, *jsonOut); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  %v\n", err)
			os.Exit(1)
		}

	case "prompt":
		fs := flag.NewFlagSet("prompt", flag.ExitOnError)
		copyFlag := fs.Bool("copy", false, "copia o prompt para o clipboard (macOS)")
		_ = fs.Parse(os.Args[2:])
		if err := runPrompt(fs.Args(), *copyFlag); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "update":
		if err := runUpdate(); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  ERRO: %v\n", err)
			os.Exit(1)
		}

	case "release":
		fs := flag.NewFlagSet("release", flag.ExitOnError)
		skipGate := fs.Bool("skip-gate", false, "pula make gate-cli antes de taguear (NÃO recomendado)")
		waitCI := fs.Bool("wait-ci", false, "aguarda release.yml finalizar e roda release_gate automaticamente")
		waitTimeout := fs.Duration("wait-timeout", 15*60*time.Second, "timeout do --wait-ci (ex: 15m, 30m)")
		repo := fs.String("repo", "", "override do repo (default: IanVDev/orbit-engine)")
		_ = fs.Parse(os.Args[2:])
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "uso: orbit release [flags] <version>")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Flags (devem vir ANTES da version, convenção Go):")
			fmt.Fprintln(os.Stderr, "  --skip-gate              pula make gate-cli (NÃO recomendado)")
			fmt.Fprintln(os.Stderr, "  --wait-ci                aguarda release.yml + roda release_gate")
			fmt.Fprintln(os.Stderr, "  --wait-timeout 15m       timeout do --wait-ci (default 15m)")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Exemplos:")
			fmt.Fprintln(os.Stderr, "  orbit release v0.1.2")
			fmt.Fprintln(os.Stderr, "  orbit release --wait-ci v0.1.2")
			fmt.Fprintln(os.Stderr, "  orbit release --skip-gate v0.1.2")
			os.Exit(1)
		}
		opts := ReleaseOptions{
			Version:    fs.Arg(0),
			SkipGate:   *skipGate,
			WaitCI:     *waitCI,
			WaitCITime: *waitTimeout,
			Repo:       *repo,
		}
		if err := runRelease(opts, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "\n❌  %v\n", err)
			os.Exit(1)
		}

	case "version":
		fmt.Printf("orbit version %s (commit=%s build=%s)\n", Version, Commit, BuildTime)

	case "help", "--help", "-h":
		printUsage()

	default:
		fmt.Fprintf(os.Stderr, "orbit: comando desconhecido %q\n", os.Args[1])
		if s := suggestCommand(os.Args[1]); s != "" {
			fmt.Fprintf(os.Stderr, "\n    Você quis dizer:  orbit %s\n", s)
		}
		fmt.Fprintln(os.Stderr, "")
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
	fmt.Fprintln(os.Stderr, "  analyze       [DEPRECATED] alias de `orbit doctor --alert-only`")
	fmt.Fprintln(os.Stderr, "  context-pack  Gera context-pack para transição entre conversas (alias: ctx)")
	fmt.Fprintln(os.Stderr, "  git           Subcomandos git (verify-merge)")
	fmt.Fprintln(os.Stderr, "  hygiene       Gerencia o pre-commit hook (install|check)")
	fmt.Fprintln(os.Stderr, "  doctor        Diagnóstico de instalação e conflitos de PATH")
	fmt.Fprintln(os.Stderr, "  verify        Re-valida o proof SHA256 de um log de execução (--chain: valida todos)")
	fmt.Fprintln(os.Stderr, "  diagnose      Analisa o último log e extrai causa provável da falha")
	fmt.Fprintln(os.Stderr, "  prompt        Gera prompt estruturado para o Claude a partir de um objetivo")
	fmt.Fprintln(os.Stderr, "  update        Atualiza o binário orbit via GitHub Releases")
	fmt.Fprintln(os.Stderr, "  release       Cria tag + push + (opcional) espera CI + valida release_gate")
	fmt.Fprintln(os.Stderr, "  version       Versão instalada")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --host <url>    URL do tracking-server (default: http://localhost:9100)")
	fmt.Fprintln(os.Stderr, "                  Em quickstart, deixe vazio para usar servidor embutido.")
	fmt.Fprintln(os.Stderr, "  --json          (run) output JSON estruturado")
	fmt.Fprintln(os.Stderr, "  --no-spinner    (run) desativa spinner de progresso")
	fmt.Fprintln(os.Stderr, "  --share         (stats) gerar texto curto para compartilhar resultados")
	fmt.Fprintln(os.Stderr, "  --strict        (doctor) falha com exit 1 se houver WARNINGs")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Exemplos:")
	fmt.Fprintln(os.Stderr, "  orbit quickstart")
	fmt.Fprintln(os.Stderr, "  orbit stats")
	fmt.Fprintln(os.Stderr, "  orbit stats --host http://meu-servidor:9100")
	fmt.Fprintln(os.Stderr, "  orbit doctor")
	fmt.Fprintln(os.Stderr, "  orbit doctor --strict")
	fmt.Fprintln(os.Stderr, "  orbit verify ~/.orbit/logs/<arquivo>.json")
	fmt.Fprintln(os.Stderr, "  orbit diagnose")
	fmt.Fprintln(os.Stderr, "  orbit diagnose --json")
}
