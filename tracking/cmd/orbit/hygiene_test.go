// hygiene_test.go — cobertura básica de `orbit hygiene`.
//
// Testa end-to-end sobre um repositório git temporário:
//   - install em repo limpo → INSTALLED
//   - install em repo com hook pré-existente → ALREADY_PRESENT (idempotente)
//   - check com hook presente → sem erro, output INSTALLED
//   - check sem hook → erro + output NOT_INSTALLED
//
// Exige `git` e `bash` no PATH (cenário padrão macOS/Linux).
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setupRepo cria um repo git temporário e aponta ORBIT_HYGIENE_DIR para o
// pacote real localizado em ../../../orbit-hygiene, evitando duplicação.
func setupRepo(t *testing.T) (repoDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git indisponível")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash indisponível")
	}

	repo := t.TempDir()
	for _, args := range [][]string{
		// --template= impede que init.templateDir do usuário copie hooks
		// para o repo temporário, garantindo ambiente isolado para os testes.
		{"init", "-q", "--template="},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v falhou: %v\n%s", args, err, out)
		}
	}

	// Resolve orbit-hygiene/ relativo a este arquivo fonte.
	_, thisFile, _, _ := runtime.Caller(0)
	pkgDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "orbit-hygiene")
	if _, err := os.Stat(filepath.Join(pkgDir, "install.sh")); err != nil {
		t.Fatalf("orbit-hygiene/install.sh não encontrado em %s: %v", pkgDir, err)
	}
	t.Setenv("ORBIT_HYGIENE_DIR", pkgDir)

	// Garante que gitTopLevel() execute dentro do repo de teste.
	oldWd, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	return repo
}

func TestHygieneInstallFresh(t *testing.T) {
	repo := setupRepo(t)
	var buf bytes.Buffer
	if err := hygieneInstall(&buf); err != nil {
		t.Fatalf("install falhou: %v", err)
	}
	if !strings.Contains(buf.String(), "INSTALLED:") {
		t.Fatalf("esperado INSTALLED, got %q", buf.String())
	}
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatalf("hook não foi criado: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("hook não é executável: mode=%v", info.Mode())
	}
}

func TestHygieneInstallIdempotent(t *testing.T) {
	setupRepo(t)
	var buf bytes.Buffer
	if err := hygieneInstall(&buf); err != nil {
		t.Fatalf("primeira install falhou: %v", err)
	}
	buf.Reset()
	if err := hygieneInstall(&buf); err != nil {
		t.Fatalf("segunda install falhou: %v", err)
	}
	if !strings.Contains(buf.String(), "ALREADY_PRESENT:") {
		t.Fatalf("esperado ALREADY_PRESENT, got %q", buf.String())
	}
}

func TestHygieneCheckMissing(t *testing.T) {
	setupRepo(t)
	var buf bytes.Buffer
	err := hygieneCheck(&buf)
	if err == nil {
		t.Fatalf("check devia ter retornado erro quando hook ausente")
	}
	if !strings.Contains(buf.String(), "NOT_INSTALLED:") {
		t.Fatalf("esperado NOT_INSTALLED, got %q", buf.String())
	}
}

func TestAutoInstallInsideRepo(t *testing.T) {
	setupRepo(t)
	var buf bytes.Buffer
	autoInstallHygiene(&buf)
	if !strings.Contains(buf.String(), "INSTALLED:") {
		t.Fatalf("esperado INSTALLED, got %q", buf.String())
	}
	// Segunda chamada deve ser idempotente.
	buf.Reset()
	autoInstallHygiene(&buf)
	if !strings.Contains(buf.String(), "ALREADY_PRESENT:") {
		t.Fatalf("esperado ALREADY_PRESENT na 2ª chamada, got %q", buf.String())
	}
}

func TestAutoInstallOutsideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git indisponível")
	}
	// Diretório temporário SEM git init.
	outside := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(outside); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var buf bytes.Buffer
	autoInstallHygiene(&buf)
	if !strings.Contains(buf.String(), "SKIPPED") {
		t.Fatalf("esperado SKIPPED fora de repo, got %q", buf.String())
	}
}

func TestHygieneCheckPresent(t *testing.T) {
	setupRepo(t)
	var buf bytes.Buffer
	if err := hygieneInstall(&buf); err != nil {
		t.Fatalf("install falhou: %v", err)
	}
	buf.Reset()
	if err := hygieneCheck(&buf); err != nil {
		t.Fatalf("check falhou com hook presente: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "INSTALLED:") {
		t.Fatalf("esperado INSTALLED, got %q", buf.String())
	}
}
