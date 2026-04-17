// startup_guard.go — guarda fail-closed executada no início do `orbit`.
//
// Motivação: diagnósticos anteriores mostraram que o sintoma mais comum de
// "bug fantasma" no orbit-engine é ter múltiplas cópias do binário no PATH
// com commits divergentes, fazendo cada invocação rodar um artefato
// diferente. A guarda aborta a execução antes que comandos escrevam estado
// inconsistente — e aponta o comando exato para diagnosticar.
//
// Política:
//   - múltiplos binários `orbit` no PATH  → abort
//   - commit baked vs commit do PATH diferentes → abort
//   - binário ativo é wrapper/script (shebang) → abort
//
// Escape hatch: `ORBIT_SKIP_GUARD=1` para situações explícitas onde o
// usuário sabe o que está fazendo (bootstrap, recovery).
//
// Subcomandos de diagnóstico sempre bypassam a guarda — caso contrário
// seria impossível reparar um ambiente divergente.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// guardBypassCommands sempre rodam, mesmo com ambiente inconsistente.
var guardBypassCommands = map[string]struct{}{
	"doctor":    {},
	"version":   {}, "--version": {}, "-v": {},
	"help":      {}, "--help": {}, "-h": {},
	"analyze":   {},
}

// startupVerdict é o resultado puro da análise — separado da ação
// (log.Fatal) para permitir teste determinístico.
type startupVerdict struct {
	OK       bool
	Reasons  []string // razões acumuladas (vazio se OK)
	FixHints []string // uma hint por razão, na mesma ordem
}

// enforceStartupIntegrity é o ponto de integração chamado no main().
// Não retorna se detectar divergência — chama log.Fatal.
func enforceStartupIntegrity(subcommand string) {
	if os.Getenv("ORBIT_SKIP_GUARD") == "1" {
		return
	}
	if _, bypass := guardBypassCommands[subcommand]; bypass {
		return
	}
	// Durante `go run` não há binário instalado — pular guarda para não
	// bloquear desenvolvimento local.
	if isEphemeralBuild() {
		return
	}

	self, _ := os.Executable()
	found := findAllOrbitsInPath()
	active := firstInPath()
	pathCommit := ""
	if active != "" {
		pathCommit = queryVersionCommit(active)
	}

	v := evaluateStartupIntegrity(self, Commit, found, active, pathCommit)
	if v.OK {
		return
	}

	log.SetFlags(0)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "❌  orbit: ambiente inconsistente — abortando antes de executar.")
	for i, r := range v.Reasons {
		fmt.Fprintf(os.Stderr, "    → %s\n", r)
		if i < len(v.FixHints) && v.FixHints[i] != "" {
			fmt.Fprintf(os.Stderr, "       fix: %s\n", v.FixHints[i])
		}
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "    Diagnóstico completo:  orbit doctor --deep")
	fmt.Fprintln(os.Stderr, "    Bypass emergencial:    ORBIT_SKIP_GUARD=1 orbit <cmd>")
	fmt.Fprintln(os.Stderr, "")
	log.Fatal("startup guard: fail-closed")
}

// evaluateStartupIntegrity é a forma pura/testável. Nenhum I/O.
//
//	selfPath    — os.Executable() do binário em execução
//	selfCommit  — main.Commit baked neste binário
//	found       — todos os `orbit` encontrados percorrendo PATH
//	active      — primeiro resolvido pelo PATH (ou "")
//	pathCommit  — commit reportado por `<active> version` (ou "")
func evaluateStartupIntegrity(selfPath, selfCommit string, found []string, active, pathCommit string) *startupVerdict {
	v := &startupVerdict{OK: true}
	add := func(reason, fix string) {
		v.OK = false
		v.Reasons = append(v.Reasons, reason)
		v.FixHints = append(v.FixHints, fix)
	}

	// 1. Múltiplos binários distintos no PATH.
	if n := countDistinct(found); n > 1 {
		add(
			fmt.Sprintf("%d binários orbit distintos no PATH: %s", n, strings.Join(dedupe(found), ", ")),
			"remova as cópias extras ou realinhe o PATH para uma única origem",
		)
	}

	// 2. Commit baked ausente — binário não rastreável.
	if selfCommit == "" || selfCommit == "unknown" {
		add(
			"binário ativo sem commit stamp (build sem -ldflags -X main.Commit)",
			"rebuild via scripts/build_orbit.sh",
		)
	}

	// 3. Commit mismatch entre self e binário no PATH.
	if selfCommit != "" && selfCommit != "unknown" &&
		pathCommit != "" && pathCommit != "unknown" &&
		selfCommit != pathCommit {
		add(
			fmt.Sprintf("commit mismatch: self=%s  PATH(%s)=%s", selfCommit, active, pathCommit),
			"reinstale o binário esperado em "+expectedInstallPath,
		)
	}

	return v
}

// ── helpers de I/O ───────────────────────────────────────────────────────────

func findAllOrbitsInPath() []string {
	var out []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, "orbit")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func firstInPath() string {
	p, err := exec.LookPath("orbit")
	if err != nil {
		return ""
	}
	return p
}

func queryVersionCommit(binary string) string {
	out, err := runOrbitVersion(binary)
	if err != nil {
		return ""
	}
	return extractCommit(out)
}

// isEphemeralBuild detecta execução via `go run` — onde os.Executable()
// aponta para um diretório de build temporário.
func isEphemeralBuild() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(self, "/go-build") ||
		strings.Contains(self, string(os.PathSeparator)+"T"+string(os.PathSeparator)+"go-build")
}

// countDistinct conta entradas após resolução de symlinks.
func countDistinct(paths []string) int {
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			real = p
		}
		seen[real] = struct{}{}
	}
	return len(seen)
}

func dedupe(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
