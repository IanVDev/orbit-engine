// update.go — implementa `orbit update`.
//
// Fluxo (fail-closed):
//  1. Detecta caminho do binário atual via os.Executable()
//  2. Baixa o latest release do GitHub para um arquivo temporário
//  3. Valida o novo binário executando `<tmp> version`
//  4. Cria backup em <destino>.bak
//  5. Substitui atomicamente via os.Rename
//
// O binário instalado nunca é modificado antes da validação passar.
// Qualquer falha retorna error — o caller em main.go faz os.Exit(1).
//
// Variável de ambiente ORBIT_UPDATE_URL_OVERRIDE: sobrescreve a URL de
// download (útil em testes e ambientes air-gapped).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	updateGitHubRepo    = "IanVDev/orbit-engine"
	updateBinaryName    = "orbit"
	updateTimeout        = 30 * time.Second
	updateVersionTimeout = 5 * time.Second
)

// releaseURLs retorna as URLs de download do binário e SHA256 para a plataforma atual.
// Resolve a versão latest via GitHub redirect (mesmo padrão de install_remote.sh).
// Pode ser sobrescrita por ORBIT_UPDATE_URL_OVERRIDE (testes/air-gap).
func releaseURLs() (binURL, shaURL string, err error) {
	if override := os.Getenv("ORBIT_UPDATE_URL_OVERRIDE"); override != "" {
		return override, override + ".sha256", nil
	}
	version, err := resolveLatestVersion(updateGitHubRepo)
	if err != nil {
		return "", "", fmt.Errorf("falha ao resolver versão latest: %w", err)
	}
	bin := fmt.Sprintf(
		"https://github.com/%s/releases/download/%s/%s-%s-%s-%s",
		updateGitHubRepo, version, updateBinaryName, version, runtime.GOOS, runtime.GOARCH,
	)
	return bin, bin + ".sha256", nil
}

// resolveLatestVersion faz GET para /releases/latest sem seguir redirect,
// extrai a versão do Location header (ex: .../releases/tag/v0.1.2 → v0.1.2).
func resolveLatestVersion(repo string) (string, error) {
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: updateTimeout,
	}
	resp, err := client.Get(fmt.Sprintf("https://github.com/%s/releases/latest", repo))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("github não retornou redirect para latest release")
	}
	parts := strings.Split(strings.TrimSuffix(loc, "/"), "/")
	v := parts[len(parts)-1]
	if !strings.HasPrefix(v, "v") {
		return "", fmt.Errorf("versão inesperada no redirect %q", loc)
	}
	return v, nil
}

// runUpdate é o ponto de entrada do comando `orbit update`.
// Não produz saída diretamente — usa fmt.Println para progresso.
func runUpdate() error {
	destPath, err := resolveInstallDest()
	if err != nil {
		return err
	}

	// ── [1] Versão atual ──────────────────────────────────────────────────
	fmt.Println("")
	fmt.Println("🔄  orbit update")
	fmt.Println("")

	currentVersion, err := queryBinaryVersion(destPath)
	if err != nil {
		return fmt.Errorf("falha ao verificar versão atual (%s): %w", destPath, err)
	}
	fmt.Printf("      versão atual: %s\n", currentVersion)

	// ── [2] Resolver versão latest + URLs ─────────────────────────────────────
	fmt.Printf("[2/4] Resolvendo versão latest...\n")

	binURL, shaURL, err := releaseURLs()
	if err != nil {
		return fmt.Errorf("falha ao resolver URLs: %w", err)
	}
	fmt.Printf("      ✓  URLs construídas\n")

	// ── [3] Download binário + SHA256 ──────────────────────────────────────
	fmt.Printf("[3/4] Baixando binário + checksum...\n")

	tmpPath, err := downloadToTemp(binURL)
	if err != nil {
		return fmt.Errorf("download do binário falhou: %w", err)
	}
	defer os.Remove(tmpPath)

	tmpShaPath, err := downloadToTemp(shaURL)
	if err != nil {
		return fmt.Errorf("download do .sha256 falhou: %w", err)
	}
	defer os.Remove(tmpShaPath)

	fmt.Printf("      ✓  binário + checksum baixados\n")

	// ── [4] Validar SHA256 ────────────────────────────────────────────────
	fmt.Printf("[4/4] Verificando SHA256...\n")

	if err := verifyChecksum(tmpPath, tmpShaPath); err != nil {
		return fmt.Errorf("falha na verificação de integridade: %w", err)
	}
	fmt.Printf("      ✓  SHA256 confere\n")

	// ── [5] Validar novo binário + instalação ──────────────────────────────
	fmt.Printf("[5/5] Instalando...\n")

	newVersion, err := queryBinaryVersion(tmpPath)
	if err != nil {
		return fmt.Errorf("novo binário inválido (falhou em 'version'): %w", err)
	}
	fmt.Printf("      ✓  nova versão: %s\n", newVersion)

	if currentVersion == newVersion {
		fmt.Println("")
		fmt.Println("✅  orbit já está na versão mais recente.")
		fmt.Println("")
		return nil
	}

	backupPath := destPath + ".bak"
	fmt.Printf("      criando backup em %s...\n", backupPath)

	if err := copyFile(destPath, backupPath); err != nil {
		return fmt.Errorf("falha ao criar backup: %w", err)
	}
	fmt.Printf("      ✓  backup criado\n")

	fmt.Printf("      instalando em %s...\n", destPath)

	if err := replaceFile(tmpPath, destPath); err != nil {
		return fmt.Errorf(
			"falha ao instalar (backup disponível em %s): %w",
			backupPath, err,
		)
	}
	fmt.Printf("      ✓  binário instalado\n")

	installedVersion, err := queryBinaryVersion(destPath)
	if err != nil {
		return fmt.Errorf(
			"binário pós-instalação falhou em 'version' — restaure: cp %s %s",
			backupPath, destPath,
		)
	}

	fmt.Println("")
	fmt.Printf("✅  orbit atualizado com sucesso!\n")
	fmt.Printf("    %s  →  %s\n", currentVersion, installedVersion)
	fmt.Printf("    backup: %s\n", backupPath)
	fmt.Println("")
	return nil
}

// resolveInstallDest determina o caminho canônico de instalação.
// Preferência: os.Executable() → expectedInstallPath.
func resolveInstallDest() (string, error) {
	self, err := os.Executable()
	if err != nil || self == "" {
		return expectedInstallPath, nil
	}
	if _, err := os.Stat(self); err != nil {
		return expectedInstallPath, nil
	}
	return self, nil
}

// queryBinaryVersion executa `<path> version` e retorna o output.
// Falha se o binário não responder em updateVersionTimeout.
func queryBinaryVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateVersionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("output vazio de 'version'")
	}
	return v, nil
}

// downloadToTemp faz o download da URL para um arquivo temporário executável.
func downloadToTemp(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d ao baixar %s", resp.StatusCode, url)
	}

	tmp, err := os.CreateTemp("", "orbit-update-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}

	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}

	return tmp.Name(), nil
}

// copyFile copia src para dst, preservando permissões do src.
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// replaceFile move src para dst atomicamente (via os.Rename quando possível).
func replaceFile(src, dst string) error {
	// os.Rename falha cross-device; fallback para copy+remove.
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// verifyChecksum valida o SHA256 do binário contra o arquivo .sha256.
// Fail-closed: error se mismatch ou arquivo .sha256 malformado.
func verifyChecksum(binPath, shaPath string) error {
	shaContent, err := os.ReadFile(shaPath)
	if err != nil {
		return fmt.Errorf("falha ao ler .sha256: %w", err)
	}
	expected := strings.Fields(string(shaContent))
	if len(expected) < 1 {
		return fmt.Errorf("arquivo .sha256 malformado")
	}

	f, err := os.Open(binPath)
	if err != nil {
		return fmt.Errorf("falha ao abrir binário: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("erro ao calcular sha256: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))

	if actual != expected[0] {
		return fmt.Errorf("sha256 mismatch — binário adulterado ou corrompido\n  esperado: %s\n  obtido:   %s", expected[0], actual)
	}
	return nil
}
