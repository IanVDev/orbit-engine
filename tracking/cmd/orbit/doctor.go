// doctor.go — diagnóstico de ambiente do orbit-engine CLI.
//
// Detecta:
//   - conflitos de PATH / binários duplicados
//   - binário em uso vs. caminho esperado (/usr/local/bin/orbit)
//   - commit stamp de build ausente (binário não rastreável)
//   - ORBIT_HMAC_SECRET ausente (WARNING em dev, CRITICAL em prod)
//   - conectividade com tracking-server
//
// Exit codes:
//   - 0  tudo OK (ou apenas WARNINGs sem --strict)
//   - 1  qualquer check CRITICAL, ou WARNING com --strict
//
// Fail-closed: commit vazio e binário inconsistente são sempre CRITICAL.
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// expectedInstallPath é o destino canônico do binário `orbit`.
const expectedInstallPath = "/usr/local/bin/orbit"

// trackingHealthURL é onde o tracking-server expõe /health.
const trackingHealthURL = "http://localhost:9100/health"

// severity classifica cada check no relatório.
type severity int

const (
	sevOK severity = iota
	sevWarning
	sevCritical
)

func (s severity) tag() string {
	switch s {
	case sevOK:
		return "OK"
	case sevWarning:
		return "WARNING"
	case sevCritical:
		return "CRITICAL"
	}
	return "?"
}

func (s severity) glyph() string {
	switch s {
	case sevOK:
		return "✅"
	case sevWarning:
		return "⚠️ "
	case sevCritical:
		return "❌"
	}
	return "?"
}

// check é um item do relatório estruturado.
type check struct {
	name     string
	severity severity
	detail   string
	fixHint  string // comando/sugestão copiável para --fix
}

// doctorResult armazena o diagnóstico completo.
type doctorResult struct {
	currentBinary string
	selfPath      string
	allFound      []string
	pathDirs      []string
	orbitBinPos   int
	localBinPos   int
	checks        []check
}

func (r *doctorResult) add(name string, sev severity, detail, fixHint string) {
	r.checks = append(r.checks, check{name: name, severity: sev, detail: detail, fixHint: fixHint})
}

func (r *doctorResult) counts() (ok, warn, crit int) {
	for _, c := range r.checks {
		switch c.severity {
		case sevOK:
			ok++
		case sevWarning:
			warn++
		case sevCritical:
			crit++
		}
	}
	return
}

// runDoctor executa o diagnóstico e imprime o relatório.
// strict: WARNINGs causam exit 1. fix: imprime/aplica correções.
// deep: ativa checks adicionais de consistência de ambiente (symlinks,
// wrappers, commit mismatch, origem de narrativa conhecida).
func runDoctor(strict, fix, deep bool) error {
	res := &doctorResult{orbitBinPos: -1, localBinPos: -1}

	fmt.Println()
	if deep {
		fmt.Println("🩺  orbit doctor --deep — diagnóstico profundo de ambiente")
	} else {
		fmt.Println("🩺  orbit doctor — diagnóstico de ambiente")
	}
	fmt.Println("─────────────────────────────────────────────────")

	collectBinaryInfo(res)
	collectPathInfo(res)
	checkPathOrder(res)
	checkUniqueOrbit(res)
	checkActiveBinary(res)
	checkExecutable(res)
	checkExpectedInstallPath(res)
	checkCommitStamp(res)
	checkHMACSecret(res)
	checkTrackingConnectivity(res)

	if deep {
		upgradeDuplicatesToCritical(res)
		checkSymlinkChain(res)
		checkWrapperScript(res)
		checkCommitMismatch(res)
		checkNarrativeOrigin(res)
	}

	printStructuredReport(res)

	if fix {
		printFixSuggestions(res)
	}

	return finalize(res, strict)
}

// ── collectors ───────────────────────────────────────────────────────────────

func collectBinaryInfo(res *doctorResult) {
	self, err := os.Executable()
	if err != nil {
		self = "(desconhecido)"
	} else if resolved, rErr := filepath.EvalSymlinks(self); rErr == nil {
		self = resolved
	}
	res.selfPath = self

	whichOut, whichErr := exec.Command("which", "orbit").Output()
	if whichErr == nil {
		res.currentBinary = strings.TrimSpace(string(whichOut))
	} else {
		res.currentBinary = ""
	}

	fmt.Printf("  Binário em execução     : %s\n", res.selfPath)
	if res.currentBinary == "" {
		fmt.Println("  orbit no PATH (which)   : (não encontrado)")
	} else {
		fmt.Printf("  orbit no PATH (which)   : %s\n", res.currentBinary)
	}
}

func collectPathInfo(res *doctorResult) {
	res.pathDirs = filepath.SplitList(os.Getenv("PATH"))
	home, _ := os.UserHomeDir()

	for i, dir := range res.pathDirs {
		normalized := normalizePath(dir, home)
		if isOrbitBinDir(normalized, home) && res.orbitBinPos == -1 {
			res.orbitBinPos = i
		}
		if isLocalBinDir(normalized, home) && res.localBinPos == -1 {
			res.localBinPos = i
		}
	}

	for _, dir := range res.pathDirs {
		candidate := filepath.Join(dir, "orbit")
		if _, statErr := os.Stat(candidate); statErr == nil {
			res.allFound = append(res.allFound, candidate)
		}
	}
}

// ── checks ───────────────────────────────────────────────────────────────────

func checkPathOrder(res *doctorResult) {
	if res.orbitBinPos == -1 {
		res.add("~/.orbit/bin no PATH", sevWarning,
			"ausente",
			`export PATH="${HOME}/.orbit/bin:${PATH}"`)
		return
	}
	res.add("~/.orbit/bin no PATH", sevOK,
		fmt.Sprintf("posição [%d]", res.orbitBinPos), "")

	if res.localBinPos != -1 && res.orbitBinPos > res.localBinPos {
		res.add("Ordem ~/.orbit/bin < ~/.local/bin", sevWarning,
			fmt.Sprintf("invertido: local=[%d] orbit=[%d]", res.localBinPos, res.orbitBinPos),
			`export PATH="${HOME}/.orbit/bin:${PATH}"  # reposiciona à frente`)
	}
}

func checkUniqueOrbit(res *doctorResult) {
	switch {
	case len(res.allFound) == 0:
		res.add("Binários orbit no PATH", sevCritical,
			"nenhum encontrado",
			"reinstale: scripts/build_orbit.sh")
	case len(res.allFound) == 1:
		res.add("Binários orbit únicos", sevOK, res.allFound[0], "")
	default:
		dupes := strings.Join(res.allFound, ", ")
		fixes := make([]string, 0, len(res.allFound)-1)
		for _, p := range res.allFound[1:] {
			fixes = append(fixes, "rm -f "+shellQuote(p))
		}
		res.add("Binários orbit únicos", sevWarning,
			fmt.Sprintf("%d encontrados: %s", len(res.allFound), dupes),
			strings.Join(fixes, " && "))
	}
}

func checkActiveBinary(res *doctorResult) {
	if res.currentBinary == "" {
		return
	}
	home, _ := os.UserHomeDir()
	expectedDir := filepath.Join(home, ".orbit", "bin")
	if res.orbitBinPos != -1 && !strings.HasPrefix(res.currentBinary, expectedDir) {
		res.add("orbit ativo == ~/.orbit/bin/orbit", sevWarning,
			fmt.Sprintf("resolveu para %s", res.currentBinary),
			"verifique ordem do PATH")
	}
}

func checkExecutable(res *doctorResult) {
	if res.currentBinary == "" {
		return
	}
	info, statErr := os.Stat(res.currentBinary)
	if statErr != nil {
		return
	}
	if info.Mode()&0o111 == 0 {
		res.add("Permissão de execução", sevCritical,
			"sem +x em "+res.currentBinary,
			"chmod +x "+shellQuote(res.currentBinary))
	}
}

// checkExpectedInstallPath compara o binário ativo com /usr/local/bin/orbit.
// Inconsistência → CRITICAL (conforme fail-closed do requisito).
func checkExpectedInstallPath(res *doctorResult) {
	_, statErr := os.Stat(expectedInstallPath)
	if statErr != nil {
		res.add("Binário em "+expectedInstallPath, sevWarning,
			"ausente",
			"sudo install -m 0755 <build> "+expectedInstallPath)
		return
	}
	if res.currentBinary == "" {
		res.add("Binário em "+expectedInstallPath, sevWarning,
			"existe, mas orbit não resolve via PATH", "")
		return
	}
	// Resolve symlinks dos dois lados antes de comparar.
	activeReal, _ := filepath.EvalSymlinks(res.currentBinary)
	expectedReal, _ := filepath.EvalSymlinks(expectedInstallPath)
	if activeReal == "" {
		activeReal = res.currentBinary
	}
	if expectedReal == "" {
		expectedReal = expectedInstallPath
	}
	if activeReal != expectedReal {
		res.add("Binário ativo == "+expectedInstallPath, sevCritical,
			fmt.Sprintf("ativo=%s esperado=%s", activeReal, expectedReal),
			"realinhe o PATH ou reinstale em "+expectedInstallPath)
		return
	}
	res.add("Binário ativo == "+expectedInstallPath, sevOK, activeReal, "")
}

// checkCommitStamp valida que o binário foi buildado com -ldflags injetando Commit.
// Commit vazio/"unknown" → CRITICAL (fail-closed: binário não rastreável).
func checkCommitStamp(res *doctorResult) {
	c := strings.TrimSpace(Commit)
	if c == "" || c == "unknown" {
		res.add("Commit stamp (ldflags)", sevCritical,
			fmt.Sprintf("Commit=%q — build sem -X main.Commit", Commit),
			"rebuild: scripts/build_orbit.sh")
		return
	}
	res.add("Commit stamp (ldflags)", sevOK,
		fmt.Sprintf("commit=%s build=%s", Commit, BuildTime), "")
}

// checkHMACSecret: WARNING em dev, CRITICAL em prod.
// Prod é detectado via ORBIT_ENV=prod ou ORBIT_ENV=production.
func checkHMACSecret(res *doctorResult) {
	if os.Getenv("ORBIT_HMAC_SECRET") != "" {
		res.add("ORBIT_HMAC_SECRET", sevOK, "configurado", "")
		return
	}
	env := strings.ToLower(os.Getenv("ORBIT_ENV"))
	if env == "prod" || env == "production" {
		res.add("ORBIT_HMAC_SECRET", sevCritical,
			"ausente em ORBIT_ENV=prod",
			`export ORBIT_HMAC_SECRET="<secret>"`)
		return
	}
	res.add("ORBIT_HMAC_SECRET", sevWarning,
		"ausente (modo dev)",
		`export ORBIT_HMAC_SECRET="<secret>"`)
}

// checkTrackingConnectivity faz GET em /health com timeout curto.
func checkTrackingConnectivity(res *doctorResult) {
	probeHealth(res, trackingHealthURL, 2)
}

// probeHealth é a lógica testável por trás de checkTrackingConnectivity.
// timeoutSec permite que testes usem valores pequenos sem bloquear CI.
func probeHealth(res *doctorResult, url string, timeoutSec int) {
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		res.add("Tracking-server /health", sevCritical,
			fmt.Sprintf("inacessível: %v", err),
			"inicie o tracking-server em :9100")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		res.add("Tracking-server /health", sevCritical,
			fmt.Sprintf("HTTP %d", resp.StatusCode),
			"verifique logs do tracking-server")
		return
	}
	res.add("Tracking-server /health", sevOK, "HTTP 200", "")
}

// ── output ───────────────────────────────────────────────────────────────────

func printStructuredReport(res *doctorResult) {
	fmt.Println()
	fmt.Println("  Verificações:")
	for _, c := range res.checks {
		fmt.Printf("    %s  [%-8s] %-42s %s\n",
			c.severity.glyph(), c.severity.tag(), c.name, c.detail)
	}

	ok, warn, crit := res.counts()
	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Printf("  Resumo: %d OK · %d WARNING · %d CRITICAL\n", ok, warn, crit)
	if crit == 0 && warn == 0 {
		fmt.Println("  ✅  Ambiente íntegro")
	}
	fmt.Println()
}

func printFixSuggestions(res *doctorResult) {
	var hints []check
	for _, c := range res.checks {
		if c.severity != sevOK && c.fixHint != "" {
			hints = append(hints, c)
		}
	}
	if len(hints) == 0 {
		return
	}
	fmt.Println("  🔧  Sugestões (--fix):")
	for _, c := range hints {
		fmt.Printf("    # %s\n    %s\n", c.name, c.fixHint)
	}
	fmt.Println()
	fmt.Println("  (--fix não executa comandos destrutivos automaticamente; copie e aplique.)")
	fmt.Println()
}

// finalize decide o exit code conforme severidade acumulada.
func finalize(res *doctorResult, strict bool) error {
	_, warn, crit := res.counts()
	if crit > 0 {
		return fmt.Errorf("doctor: %d check(s) CRITICAL", crit)
	}
	if strict && warn > 0 {
		return fmt.Errorf("doctor --strict: %d WARNING(s)", warn)
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

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

// shellQuote faz quoting simples para sugestões copiáveis.
func shellQuote(s string) string {
	if !strings.ContainsAny(s, " '\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
