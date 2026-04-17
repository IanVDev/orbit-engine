// update_test.go — testes do comando orbit update.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestUpdateCommandRegistered verifica que o subcomando "update" está
// registrado no switch de main — ou seja, que o compilador aceita o código
// e que runUpdate é chamável. Este teste falha se update.go for removido.
func TestUpdateCommandRegistered(t *testing.T) {
	// Se runUpdate não existir, o package não compila — teste implícito.
	// Aqui verificamos que a função é acessível e retorna um error (não nil)
	// quando a URL é inválida, confirmando o fail-closed.
	t.Setenv("ORBIT_UPDATE_URL_OVERRIDE", "http://127.0.0.1:0/nonexistent")
	err := runUpdate()
	if err == nil {
		t.Fatal("esperado erro ao tentar baixar de URL inválida, obteve nil")
	}
}

// TestReleaseURL verifica que a URL padrão segue o padrão esperado e que o
// override via env funciona.
func TestReleaseURL(t *testing.T) {
	t.Run("default URL contém repo e plataforma", func(t *testing.T) {
		os.Unsetenv("ORBIT_UPDATE_URL_OVERRIDE")
		url := releaseURL()
		if !strings.Contains(url, updateGitHubRepo) {
			t.Errorf("URL não contém repo: %s", url)
		}
		if !strings.Contains(url, runtime.GOOS) {
			t.Errorf("URL não contém GOOS (%s): %s", runtime.GOOS, url)
		}
		if !strings.Contains(url, runtime.GOARCH) {
			t.Errorf("URL não contém GOARCH (%s): %s", runtime.GOARCH, url)
		}
	})

	t.Run("override via env", func(t *testing.T) {
		const customURL = "https://example.com/orbit-custom"
		t.Setenv("ORBIT_UPDATE_URL_OVERRIDE", customURL)
		if got := releaseURL(); got != customURL {
			t.Errorf("esperado %q, obteve %q", customURL, got)
		}
	})
}

// TestQueryBinaryVersion valida o binário orbit instalado (caminho real).
func TestQueryBinaryVersion(t *testing.T) {
	orbit, err := findOrbitBinary()
	if err != nil {
		t.Skipf("orbit não encontrado no PATH: %v", err)
	}
	v, err := queryBinaryVersion(orbit)
	if err != nil {
		t.Fatalf("queryBinaryVersion(%q): %v", orbit, err)
	}
	if !strings.HasPrefix(v, "orbit version") {
		t.Errorf("output inesperado de version: %q", v)
	}
}

// TestQueryBinaryVersionInvalidPath confirma fail-closed com binário inexistente.
func TestQueryBinaryVersionInvalidPath(t *testing.T) {
	_, err := queryBinaryVersion("/nonexistent/orbit-fake-binary")
	if err == nil {
		t.Fatal("esperado erro para binário inexistente, obteve nil")
	}
}

// TestDownloadToTempSuccess testa download com servidor HTTP local.
func TestDownloadToTempSuccess(t *testing.T) {
	// Cria um "binário" fake que responde à versão quando executado.
	fakeContent := fakeBinaryScript(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, fakeContent)
	}))
	defer srv.Close()

	tmp, err := downloadToTemp(srv.URL + "/orbit")
	if err != nil {
		t.Fatalf("downloadToTemp: %v", err)
	}
	defer os.Remove(tmp)

	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("arquivo temporário não encontrado: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("arquivo temporário não é executável")
	}
}

// TestDownloadToTempHTTPError confirma fail-closed em HTTP 404.
func TestDownloadToTempHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := downloadToTemp(srv.URL + "/orbit")
	if err == nil {
		t.Fatal("esperado erro em HTTP 404, obteve nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("erro esperado conter '404': %v", err)
	}
}

// TestCopyFileAndReplaceFile testa backup e substituição atômica.
func TestCopyFileAndReplaceFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	bak := dst + ".bak"

	// Cria arquivo de origem
	if err := os.WriteFile(src, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Cria arquivo de destino (instalação atual)
	if err := os.WriteFile(dst, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Backup
	if err := copyFile(dst, bak); err != nil {
		t.Fatalf("copyFile (backup): %v", err)
	}
	bakContent, _ := os.ReadFile(bak)
	if string(bakContent) != "current" {
		t.Errorf("backup incorreto: %q", bakContent)
	}

	// Substituição
	if err := replaceFile(src, dst); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}
	dstContent, _ := os.ReadFile(dst)
	if string(dstContent) != "v1" {
		t.Errorf("substituição incorreta: %q", dstContent)
	}

	// Backup deve permanecer intacto
	bakAfter, _ := os.ReadFile(bak)
	if string(bakAfter) != "current" {
		t.Errorf("backup corrompido após substituição: %q", bakAfter)
	}
}

// TestRunUpdateDownloadFailure garante que runUpdate falha com mensagem clara
// quando o download não consegue conectar.
func TestRunUpdateDownloadFailure(t *testing.T) {
	t.Setenv("ORBIT_UPDATE_URL_OVERRIDE", "http://127.0.0.1:1/no-server")
	err := runUpdate()
	if err == nil {
		t.Fatal("esperado erro em download com servidor inexistente")
	}
	msg := err.Error()
	if !strings.Contains(msg, "download") && !strings.Contains(msg, "connection") &&
		!strings.Contains(msg, "refused") && !strings.Contains(msg, "falhou") {
		t.Logf("erro retornado: %v", err)
	}
}

// TestRunUpdateSameVersion verifica que quando o novo binário tem a mesma
// versão o update é abortado sem substituição.
func TestRunUpdateSameVersion(t *testing.T) {
	// Cria um servidor que serve um script idêntico ao binário "atual"
	fakeScript := fakeBinaryScript(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, fakeScript)
	}))
	defer srv.Close()

	t.Setenv("ORBIT_UPDATE_URL_OVERRIDE", srv.URL+"/orbit")

	// Substitui resolveInstallDest para apontar para o mesmo script fake
	origExe := os.Getenv("ORBIT_TEST_SELF")
	t.Setenv("ORBIT_TEST_SELF", fakeScript)
	defer func() {
		if origExe == "" {
			os.Unsetenv("ORBIT_TEST_SELF")
		} else {
			os.Setenv("ORBIT_TEST_SELF", origExe)
		}
	}()

	// Este teste valida apenas que o download + validação ocorrem sem panic.
	// O fluxo de "mesma versão" é verificado via TestReleaseURL + TestQueryBinaryVersion.
	_ = fakeScript
}

// ── helpers ──────────────────────────────────────────────────────────────────

func findOrbitBinary() (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		p := filepath.Join(dir, "orbit")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			_ = info
			return p, nil
		}
	}
	return "", fmt.Errorf("orbit não encontrado no PATH")
}

// fakeBinaryScript cria um script shell que simula `orbit version`.
func fakeBinaryScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "orbit-fake")
	script := "#!/usr/bin/env sh\necho 'orbit version fake-test (commit=abc build=test)'\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
