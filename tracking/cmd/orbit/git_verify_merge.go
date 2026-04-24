// git_verify_merge.go — subcomando `orbit git verify-merge`.
//
// Valida que o commit referenciado é um merge commit com exatamente 2 parents.
// Merges com 1 parent (commit normal) ou 3+ parents (octopus) falham com
// diagnóstico explícito — o GitHub não reconhece octopus merges corretamente.
//
// Contrato (fail-closed):
//   - git indisponível ou não é repo       → exit 1
//   - ref inválida / commit inexistente    → exit 1
//   - número de parents != 2              → exit 1 + diagnóstico
//   - exatamente 2 parents                → exit 0 + MERGE_VALID
//
// Uso:
//
//	orbit git verify-merge            (verifica HEAD)
//	orbit git verify-merge --ref <sha> (verifica commit específico)
package main

import (
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// gitRunner é o tipo da função que executa git e devolve a linha de parents.
// Injetável em testes para eliminar dependência de repositório real.
type gitRunner func(ref string) (string, error)

// defaultGitRunner executa `git show --no-patch --pretty=%P <ref>`.
func defaultGitRunner(ref string) (string, error) {
	out, err := exec.Command("git", "show", "--no-patch", "--pretty=%P", ref).Output()
	if err != nil {
		return "", fmt.Errorf("git show falhou para %q: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// runGitSubcmd é o dispatcher de `orbit git <subcomando>`.
func runGitSubcmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: orbit git <subcomando>\n\nSubcomandos:\n  verify-merge   valida que o commit tem exatamente 2 parents")
	}
	switch args[0] {
	case "verify-merge":
		return runGitVerifyMerge(args[1:], defaultGitRunner)
	default:
		return fmt.Errorf("orbit git: subcomando desconhecido %q (use verify-merge)", args[0])
	}
}

// runGitVerifyMerge é o entrypoint público — parseia flags e delega.
func runGitVerifyMerge(args []string, runner gitRunner) error {
	fs := flag.NewFlagSet("git verify-merge", flag.ContinueOnError)
	ref := fs.String("ref", "HEAD", "commit a verificar (SHA, tag ou HEAD)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return gitVerifyMerge(*ref, runner, nil)
}

// gitVerifyMerge executa a validação e escreve output em w (nil → os.Stdout).
// Separado de runGitVerifyMerge para ser testável com writer controlado.
func gitVerifyMerge(ref string, runner gitRunner, w io.Writer) error {
	if w == nil {
		// uso normal: stdout; testes injetam buffer
		w = stdoutWriter{}
	}

	parentsLine, err := runner(ref)
	if err != nil {
		return fmt.Errorf("verify-merge: %w", err)
	}

	parents := parseParents(parentsLine)
	n := len(parents)

	if n == 2 {
		fmt.Fprintf(w, "MERGE_VALID: %s has 2 parents\n", ref)
		fmt.Fprintf(w, "  parent[0]: %s\n", parents[0])
		fmt.Fprintf(w, "  parent[1]: %s\n", parents[1])
		return nil
	}

	// Caso de falha — monta diagnóstico explícito antes de retornar o erro.
	fmt.Fprintf(w, "NOT_A_MERGE: %s has %d parent(s) (expected exactly 2)\n", ref, n)
	for i, p := range parents {
		fmt.Fprintf(w, "  parent[%d]: %s\n", i, p)
	}
	fmt.Fprintln(w, "")

	switch {
	case n == 0:
		fmt.Fprintln(w, "  Cause: root commit or detached HEAD with no parents.")
		fmt.Fprintln(w, "  Fix: verify you are pointing to a merge commit.")
	case n == 1:
		fmt.Fprintln(w, "  Cause: regular commit — not a merge commit.")
		fmt.Fprintln(w, "  Fix: use 'git merge <branch>' instead of cherry-pick or rebase.")
	default:
		fmt.Fprintf(w, "  Cause: octopus merge (%d parents) — GitHub does not display these correctly.\n", n)
		fmt.Fprintln(w, "  Fix: split into sequential two-parent merges.")
	}

	return fmt.Errorf("verify-merge: %s has %d parent(s), not 2", ref, n)
}

// parseParents divide a linha de parents de git (hashes separados por espaço).
// Linha vazia → slice vazio (commit sem parents = root commit).
func parseParents(line string) []string {
	if line == "" {
		return []string{}
	}
	return strings.Fields(line)
}

// stdoutWriter implementa io.Writer em os.Stdout para uso no path normal.
// Mantém a dependência de os.Stdout fora da assinatura testável.
type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) {
	return fmt.Print(string(p))
}
