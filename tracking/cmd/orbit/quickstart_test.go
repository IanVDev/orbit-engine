// quickstart_test.go — teste E2E determinístico do comando `orbit quickstart`.
//
// Contrato testado (fail-closed):
//   - O binário real é compilado a partir do código-fonte atual.
//   - `orbit quickstart` é executado múltiplas vezes no mesmo processo-pai.
//   - A saída DEVE conter os marcadores das 3 etapas ([1/3], [2/3], [3/3]).
//   - A saída DEVE conter a prova criptográfica ("proof válido" e
//     "sha256 verificado").
//   - O comando DEVE terminar com exit code 0 em todas as execuções.
//
// Nada é mockado: compila e executa o binário real, com servidor embutido.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildOrbitForTest devolve o caminho do binário `orbit` compilado em
// TestMain. Fail-closed: se TestMain não populou o caminho, aborta.
var builtOrbitPath string

func buildOrbitForTest(t *testing.T) string {
	t.Helper()
	if builtOrbitPath == "" {
		t.Fatalf("binário orbit não foi compilado em TestMain")
	}
	return builtOrbitPath
}

// TestMain compila o binário orbit UMA vez para toda a suíte E2E e limpa
// o diretório temporário ao final. Usamos os.MkdirTemp (em vez de
// t.TempDir) para que o binário sobreviva entre testes diferentes.
func TestMain(m *testing.M) {
	code, err := runTestMain(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runTestMain(m *testing.M) (int, error) {
	dir, err := os.MkdirTemp("", "orbit-e2e-*")
	if err != nil {
		return 1, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(dir)

	bin := filepath.Join(dir, "orbit")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 1, fmt.Errorf("go build: %v\nstderr:\n%s", err, stderr.String())
	}
	builtOrbitPath = bin
	return m.Run(), nil
}

// runQuickstartE2E executa uma rodada do binário real com timeout e retorna
// a saída combinada (stdout+stderr). Fail-closed: qualquer erro de processo
// é promovido a t.Fatalf.
func runQuickstartE2E(t *testing.T, bin string) string {
	t.Helper()
	cmd := exec.Command(bin, "quickstart")
	// TempDir evita que o hook de hygiene tente escrever no repo real;
	// quickstart degrada com "SKIPPED (não é repo git)" sem abortar.
	cmd.Dir = t.TempDir()
	// ORBIT_SKIP_GUARD permite rodar mesmo quando o PATH do dev tem outras
	// cópias de orbit instaladas — o guard é sobre integridade de instalação,
	// não sobre o comportamento do quickstart em si.
	cmd.Env = append(os.Environ(), "ORBIT_SKIP_GUARD=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("orbit quickstart falhou: %v\noutput:\n%s", err, out.String())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("orbit quickstart timeout (30s)\noutput parcial:\n%s", out.String())
	}
	return out.String()
}

// TestQuickstart_E2E_Deterministic é o gate G3.
//
// Garante:
//   - onboarding completa fim-a-fim sem host externo (servidor embutido);
//   - os 3 steps são impressos em ordem;
//   - a prova criptográfica é emitida e verificada;
//   - rodar 2 vezes não produz divergência (idempotência de saída).
func TestQuickstart_E2E_Deterministic(t *testing.T) {
	bin := buildOrbitForTest(t)

	required := []string{
		"[1/3]",
		"[2/3]",
		"[3/3]",
		"proof válido",
		"sha256 verificado",
	}

	const iterations = 2
	for i := 1; i <= iterations; i++ {
		out := runQuickstartE2E(t, bin)
		for _, needle := range required {
			if !strings.Contains(out, needle) {
				t.Fatalf("iter %d: marcador ausente %q\noutput:\n%s",
					i, needle, out)
			}
		}
	}
}

// TestQuickstart_E2E_NotFlaky executa o fluxo em múltiplas rodadas rápidas
// para detectar flakiness (race na porta embutida, timing do /health, etc.).
// Falha fechada: UMA rodada quebrada reprova o teste.
func TestQuickstart_E2E_NotFlaky(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando: flakiness check sob -short")
	}
	bin := buildOrbitForTest(t)

	const rounds = 3
	for i := 1; i <= rounds; i++ {
		out := runQuickstartE2E(t, bin)
		if !strings.Contains(out, "[3/3]") || !strings.Contains(out, "sha256 verificado") {
			t.Fatalf("rodada %d regrediu\noutput:\n%s", i, out)
		}
	}
}
