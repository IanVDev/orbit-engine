// doctor.go — diagnóstico de instalação do orbit-engine CLI.
//
// Detecta conflitos de PATH, binários duplicados e problemas de ordem.
// Retorna exit 0 se tudo OK, exit 1 se --strict e houver WARNINGs.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// doctorResult armazena o diagnóstico completo.
type doctorResult struct {
	currentBinary string   // caminho do binário sendo executado agora
	selfPath      string   // os.Executable()
	allFound      []string // todos os "orbit" encontrados no PATH
	pathDirs      []string // diretórios do PATH, em ordem
	orbitBinPos   int      // posição de ~/.orbit/bin no PATH (-1 se ausente)
	localBinPos   int      // posição de ~/.local/bin no PATH (-1 se ausente)
	warnings      []string // avisos não-fatais
	errors        []string // problemas que impedem uso correto
}

// runDoctor executa o diagnóstico e imprime o relatório.
// Se strict==true, qualquer WARNING também causa exit 1.
func runDoctor(strict bool) error {
	res := &doctorResult{orbitBinPos: -1, localBinPos: -1}

	fmt.Println()
	fmt.Println("🩺  orbit doctor — diagnóstico de instalação")
	fmt.Println("─────────────────────────────────────────────────")

	// ── 1. Binário em execução ────────────────────────────────────────────
	self, err := os.Executable()
	if err != nil {
		self = "(desconhecido)"
	} else {
		// Resolve symlinks para mostrar o caminho real.
		if resolved, rErr := filepath.EvalSymlinks(self); rErr == nil {
			self = resolved
		}
	}
	res.selfPath = self

	// which orbit — o que o shell resolveria
	whichOut, whichErr := exec.Command("which", "orbit").Output()
	if whichErr == nil {
		res.currentBinary = strings.TrimSpace(string(whichOut))
	} else {
		res.currentBinary = "(orbit não encontrado no PATH)"
	}

	printCheck("Binário em execução", res.selfPath)
	printCheck("orbit no PATH (which)", res.currentBinary)

	// ── 2. Scan completo do PATH ──────────────────────────────────────────
	rawPath := os.Getenv("PATH")
	res.pathDirs = filepath.SplitList(rawPath)
	home, _ := os.UserHomeDir()

	fmt.Println()
	fmt.Println("  Ordem do PATH:")
	for i, dir := range res.pathDirs {
		marker := ""
		normalized := normalizePath(dir, home)

		if isOrbitBinDir(normalized, home) {
			res.orbitBinPos = i
			marker = "  ← ~/.orbit/bin"
		} else if isLocalBinDir(normalized, home) {
			res.localBinPos = i
			marker = "  ← ~/.local/bin"
		}
		fmt.Printf("    [%d] %s%s\n", i, dir, marker)
	}

	// ── 3. Todos os orbits no sistema ─────────────────────────────────────
	fmt.Println()
	fmt.Println("  Orbits encontrados no PATH:")
	for _, dir := range res.pathDirs {
		candidate := filepath.Join(dir, "orbit")
		if _, statErr := os.Stat(candidate); statErr == nil {
			res.allFound = append(res.allFound, candidate)
			fmt.Printf("    • %s\n", candidate)
		}
	}
	if len(res.allFound) == 0 {
		fmt.Println("    (nenhum orbit encontrado)")
	}

	// ── 4. Validações ─────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  Verificações:")

	// 4a. ~/.orbit/bin no PATH
	if res.orbitBinPos == -1 {
		res.warnings = append(res.warnings,
			"~/.orbit/bin não está no PATH — adicione: export PATH=\"${HOME}/.orbit/bin:${PATH}\"")
		printWarn("~/.orbit/bin no PATH", "AUSENTE")
	} else {
		printOK("~/.orbit/bin no PATH", fmt.Sprintf("posição [%d]", res.orbitBinPos))
	}

	// 4b. Ordem: ~/.orbit/bin deve vir antes de ~/.local/bin
	if res.orbitBinPos != -1 && res.localBinPos != -1 {
		if res.orbitBinPos < res.localBinPos {
			printOK("~/.orbit/bin antes de ~/.local/bin",
				fmt.Sprintf("[%d] < [%d]", res.orbitBinPos, res.localBinPos))
		} else {
			res.warnings = append(res.warnings,
				fmt.Sprintf("~/.local/bin [%d] está antes de ~/.orbit/bin [%d] — "+
					"outro binário pode ser executado em vez do orbit instalado",
					res.localBinPos, res.orbitBinPos))
			printWarn("~/.orbit/bin antes de ~/.local/bin",
				fmt.Sprintf("INVERTIDO: ~/.local/bin=[%d] ~/.orbit/bin=[%d]",
					res.localBinPos, res.orbitBinPos))
		}
	}

	// 4c. Múltiplos orbits
	if len(res.allFound) > 1 {
		res.warnings = append(res.warnings,
			fmt.Sprintf("%d binários orbit encontrados no PATH — o primeiro será usado: %s",
				len(res.allFound), res.allFound[0]))
		printWarn("Binários orbit únicos",
			fmt.Sprintf("%d encontrados (possível conflito)", len(res.allFound)))
	} else if len(res.allFound) == 1 {
		printOK("Binários orbit únicos", "1 (sem conflito)")
	} else {
		printWarn("Binários orbit únicos", "nenhum encontrado")
	}

	// 4d. Binário ativo bate com ~/.orbit/bin
	if res.currentBinary != "" && res.orbitBinPos != -1 {
		expectedDir := filepath.Join(home, ".orbit", "bin")
		if strings.HasPrefix(res.currentBinary, expectedDir) {
			printOK("orbit ativo = ~/.orbit/bin/orbit", "✓")
		} else {
			res.warnings = append(res.warnings,
				fmt.Sprintf("orbit ativo (%s) não é o de ~/.orbit/bin — verifique a ordem do PATH",
					res.currentBinary))
			printWarn("orbit ativo = ~/.orbit/bin/orbit",
				fmt.Sprintf("resolveu para %s", res.currentBinary))
		}
	}

	// 4e. Binário atual é executável e tem permissão
	if res.currentBinary != "" && res.currentBinary != "(orbit não encontrado no PATH)" {
		if info, statErr := os.Stat(res.currentBinary); statErr == nil {
			if info.Mode()&0o111 != 0 {
				printOK("Permissão de execução", "✓")
			} else {
				res.errors = append(res.errors,
					fmt.Sprintf("orbit em %s não tem permissão de execução (chmod +x)", res.currentBinary))
				printErr("Permissão de execução", "FALTANDO — execute: chmod +x "+res.currentBinary)
			}
		}
	}

	// ── 5. Resumo ─────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────")

	hasProblems := len(res.warnings) > 0 || len(res.errors) > 0

	if len(res.errors) > 0 {
		fmt.Printf("  ❌  %d erro(s) encontrado(s):\n", len(res.errors))
		for _, e := range res.errors {
			fmt.Printf("      → %s\n", e)
		}
	}
	if len(res.warnings) > 0 {
		fmt.Printf("  ⚠️   %d aviso(s):\n", len(res.warnings))
		for _, w := range res.warnings {
			fmt.Printf("      → %s\n", w)
		}
	}
	if !hasProblems {
		fmt.Println("  ✅  Tudo OK — instalação sem conflitos")
	}

	fmt.Println()

	// Fail-closed se --strict e houver qualquer aviso ou erro
	if len(res.errors) > 0 {
		return fmt.Errorf("doctor: %d erro(s) de instalação encontrado(s)", len(res.errors))
	}
	if strict && len(res.warnings) > 0 {
		return fmt.Errorf("doctor --strict: %d aviso(s) encontrado(s)", len(res.warnings))
	}
	return nil
}

// ── helpers de output ────────────────────────────────────────────────────────

func printCheck(label, value string) {
	fmt.Printf("  %-35s %s\n", label+":", value)
}

func printOK(label, detail string) {
	fmt.Printf("    ✅  %-38s %s\n", label, detail)
}

func printWarn(label, detail string) {
	fmt.Printf("    ⚠️   %-37s %s\n", label, detail)
}

func printErr(label, detail string) {
	fmt.Printf("    ❌  %-38s %s\n", label, detail)
}

// ── helpers de PATH ──────────────────────────────────────────────────────────

// normalizePath substitui o prefixo real do home por "~".
func normalizePath(dir, home string) string {
	if home == "" {
		return dir
	}
	if strings.HasPrefix(dir, home) {
		return "~" + dir[len(home):]
	}
	return dir
}

func isOrbitBinDir(normalized, home string) bool {
	return normalized == "~/.orbit/bin" ||
		normalized == filepath.Join(home, ".orbit", "bin")
}

func isLocalBinDir(normalized, home string) bool {
	return normalized == "~/.local/bin" ||
		normalized == filepath.Join(home, ".local", "bin")
}
