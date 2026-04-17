// hygiene.go — comando `orbit hygiene` (install | check).
//
// Subcomandos:
//
//	orbit hygiene install   instala o pre-commit em .git/hooks/ (idempotente)
//	orbit hygiene check     verifica se o hook está presente; exit 1 se não
//
// A lógica de instalação é delegada a orbit-hygiene/install.sh — esta camada
// apenas resolve o diretório do pacote, detecta o repo root e decide entre
// INSTALLED / ALREADY_PRESENT. Nenhuma duplicação de lógica shell.
//
// Resolução do pacote (primeiro que existir):
//  1. $ORBIT_HYGIENE_DIR
//  2. caminhando para cima a partir de CWD, procurando orbit-hygiene/install.sh
//
// Fail-closed: se git não resolver o repo, ou se o pacote não for localizado,
// retorna erro e exit 1.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runHygiene é o entrypoint do subcomando.
func runHygiene(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: orbit hygiene <install|check>")
	}
	switch args[0] {
	case "install":
		return hygieneInstall(os.Stdout)
	case "check":
		return hygieneCheck(os.Stdout)
	default:
		return fmt.Errorf("orbit hygiene: subcomando desconhecido %q (use install|check)", args[0])
	}
}

// hygieneInstall copia o pre-commit para .git/hooks/ via install.sh.
// Idempotente: se o hook já existir, retorna ALREADY_PRESENT sem sobrescrever.
func hygieneInstall(w io.Writer) error {
	root, err := gitTopLevel()
	if err != nil {
		return err
	}
	hookPath := filepath.Join(root, ".git", "hooks", "pre-commit")

	if _, err := os.Stat(hookPath); err == nil {
		fmt.Fprintf(w, "ALREADY_PRESENT: %s\n", hookPath)
		return nil
	}

	pkgDir, err := locateHygieneDir()
	if err != nil {
		return err
	}
	installer := filepath.Join(pkgDir, "install.sh")
	cmd := exec.Command("bash", installer)
	cmd.Dir = root
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("orbit hygiene install: %w", err)
	}
	fmt.Fprintf(w, "INSTALLED: %s\n", hookPath)
	return nil
}

// hygieneCheck verifica se o hook está presente e executável.
// Retorna erro (→ exit 1) caso contrário.
func hygieneCheck(w io.Writer) error {
	root, err := gitTopLevel()
	if err != nil {
		return err
	}
	hookPath := filepath.Join(root, ".git", "hooks", "pre-commit")
	info, err := os.Stat(hookPath)
	if err != nil {
		fmt.Fprintf(w, "NOT_INSTALLED: %s\n", hookPath)
		return fmt.Errorf("pre-commit hook ausente")
	}
	if info.Mode()&0o111 == 0 {
		fmt.Fprintf(w, "NOT_EXECUTABLE: %s\n", hookPath)
		return fmt.Errorf("pre-commit hook não é executável")
	}
	fmt.Fprintf(w, "INSTALLED: %s\n", hookPath)
	return nil
}

// gitTopLevel retorna o root do repositório via git rev-parse.
func gitTopLevel() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("orbit hygiene: não foi possível detectar o repo (git rev-parse falhou): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// locateHygieneDir resolve o diretório do pacote orbit-hygiene.
// Ordem: $ORBIT_HYGIENE_DIR, depois caminhada ascendente a partir de CWD.
func locateHygieneDir() (string, error) {
	if env := os.Getenv("ORBIT_HYGIENE_DIR"); env != "" {
		if fileExists(filepath.Join(env, "install.sh")) {
			return env, nil
		}
		return "", fmt.Errorf("ORBIT_HYGIENE_DIR=%q não contém install.sh", env)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		candidate := filepath.Join(dir, "orbit-hygiene")
		if fileExists(filepath.Join(candidate, "install.sh")) {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("orbit hygiene: pacote não encontrado; defina ORBIT_HYGIENE_DIR ou execute a partir de um repo que contenha orbit-hygiene/")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// autoInstallHygiene é a forma não-bloqueante usada por fluxos de onboarding
// (ex.: quickstart). Contrato:
//
//   - fora de um repo git → imprime "hygiene: SKIPPED (não é repo git)" e
//     retorna nil (caller não falha).
//   - dentro de um repo → delega a hygieneInstall (INSTALLED / ALREADY_PRESENT).
//   - qualquer outro erro → imprime warning e retorna nil (caller não falha).
//
// Fail-closed vive dentro de hygieneInstall; aqui, por design, a ausência
// de governança hygiene nunca aborta o onboarding.
func autoInstallHygiene(w io.Writer) {
	if _, err := gitTopLevel(); err != nil {
		fmt.Fprintln(w, "hygiene: SKIPPED (não é repo git)")
		return
	}
	if err := hygieneInstall(w); err != nil {
		fmt.Fprintf(w, "hygiene: WARNING — %v (seguindo sem hook)\n", err)
	}
}
