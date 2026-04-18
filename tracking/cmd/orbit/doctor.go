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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DoctorSchemaVersion identifies the contract version of the JSON output.
// External consumers (CI, dashboards, ORIZON) pin against this — a breaking
// change to DoctorReport/DoctorCheck MUST bump this string.
const DoctorSchemaVersion = "v1"

// DoctorCheck is the public, JSON-serialisable view of a single diagnostic
// result. Tests and automation assert against Status + Name + Detail
// directly — they never parse the human-readable text.
//
// Status is one of "OK", "WARNING", "CRITICAL" (fail-closed: any other
// value is a bug). Name is the check identity. Detail is free-form
// context and MAY be empty.
type DoctorCheck struct {
	Status string `json:"status"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// DoctorSummary is the count of checks per status.
type DoctorSummary struct {
	OK       int `json:"ok"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
}

// DoctorReport is the envelope emitted by `orbit doctor --json`. Version
// is always set by the emitter — consumers should reject reports whose
// Version does not match a supported contract.
type DoctorReport struct {
	Version string        `json:"version"`
	Checks  []DoctorCheck `json:"checks"`
	Summary DoctorSummary `json:"summary"`
}

// DoctorErrorReport is the fail-closed envelope emitted when the doctor
// cannot produce a full report (internal error). Same Version so consumers
// can parse it with the same schema discriminator.
type DoctorErrorReport struct {
	Version string `json:"version"`
	Error   string `json:"error"`
}

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

// toReport converts the internal result to the public DoctorReport shape.
// Name and Detail are emitted as separate fields — consumers that need a
// concatenated form can join them locally; the contract keeps them distinct.
func (r *doctorResult) toReport() DoctorReport {
	checks := make([]DoctorCheck, 0, len(r.checks))
	for _, c := range r.checks {
		checks = append(checks, DoctorCheck{
			Status: c.severity.tag(),
			Name:   c.name,
			Detail: c.detail,
		})
	}
	ok, warn, crit := r.counts()
	return DoctorReport{
		Version: DoctorSchemaVersion,
		Checks:  checks,
		Summary: DoctorSummary{
			OK:       ok,
			Warning:  warn,
			Critical: crit,
		},
	}
}

// newDoctorErrorReport builds the fail-closed envelope from an error.
// Always stamped with the current schema version so consumers can parse
// success and failure with the same discriminator.
func newDoctorErrorReport(err error) DoctorErrorReport {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	return DoctorErrorReport{
		Version: DoctorSchemaVersion,
		Error:   msg,
	}
}

// runDoctor executa o diagnóstico e imprime o relatório.
// strict: WARNINGs causam exit 1. fix: imprime/aplica correções.
// deep: ativa checks adicionais de consistência de ambiente (symlinks,
// wrappers, commit mismatch, origem de narrativa conhecida).
// jsonOut: emite DoctorReport em JSON (stdout) e suprime a saída humana;
// banners, divisores, hints não são impressos.
//
// Fail-closed: o código de saída segue a mesma regra (CRITICAL → erro;
// WARNING + --strict → erro) independente do modo de saída.
//
// Side-effects policy: doctor does NOT mutate the default stdlib logger.
// The `[SECURITY]` banner seen in terminals comes from tracking.init()
// via log.Printf to stderr — our JSON output stream is stdout, so there
// is no contamination to guard against. If a future code path starts
// calling log.* inside doctor, redirect it with a local logger built via
// log.New(io.Discard, "", 0), never by touching log.SetOutput.
func runDoctor(strict, fix, deep, jsonOut bool) error {
	return runDoctorWithMode(strict, fix, deep, jsonOut, false)
}

// runDoctorWithMode é a forma estendida que aceita o modo alertOnly,
// usado tanto por `orbit doctor --alert-only` quanto pelo alias deprecated
// `orbit analyze`. Em alertOnly, suprime banner/relatório completo e
// emite apenas blocos canônicos para checks CRITICAL — silêncio quando
// o ambiente está saudável.
func runDoctorWithMode(strict, fix, deep, jsonOut, alertOnly bool) error {
	if alertOnly {
		// Modo alerta: reusa exatamente o pipeline do antigo `orbit analyze`,
		// preservando o contrato de 4 linhas por CRITICAL e silêncio em OK.
		return analyzeTo(os.Stdout)
	}

	res := &doctorResult{orbitBinPos: -1, localBinPos: -1}

	// Banner e info de binário são puramente humanos; em modo JSON os
	// omitimos para manter a saída como um único objeto parseável.
	if !jsonOut {
		fmt.Println()
		if deep {
			fmt.Println("🩺  orbit doctor --deep — diagnóstico profundo de ambiente")
		} else {
			fmt.Println("🩺  orbit doctor — diagnóstico de ambiente")
		}
		fmt.Println("─────────────────────────────────────────────────")
	}

	collectBinaryInfo(res, jsonOut)
	collectPathInfo(res)
	checkPathOrder(res)
	checkUniqueOrbit(res)
	checkDualInstallPaths(res)
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

	if jsonOut {
		if err := emitJSONReport(os.Stdout, res); err != nil {
			return fmt.Errorf("doctor --json: falha ao serializar: %w", err)
		}
	} else {
		printStructuredReport(res)
		if fix {
			printFixSuggestions(res)
		}
	}

	return finalize(res, strict)
}

// emitJSONReport writes the DoctorReport to w as indented JSON.
//
// Atomic-emission contract (fail-closed):
//
//   - The JSON is always rendered to a bytes.Buffer FIRST. No incremental
//     writes to w happen during encoding — we cannot observe a truncated
//     or interleaved envelope in the middle of emission.
//
//   - Exactly ONE Write call is made to w (with the full buffer). If w
//     partially fails (returns n<len, err), no retry or fallback is
//     attempted on w — appending a second envelope on top of a partial
//     write would produce interleaved garbage. The error is returned.
//
//   - If the internal encode fails (defensively handled; unreachable for
//     the current types since they contain only strings/ints/slices),
//     a DoctorErrorReport is encoded into a fresh buffer and written as
//     the single atomic write instead. Consumers always see at most one
//     complete envelope — never a mix.
//
// Result shape for w after this call:
//   - success: w contains the full indented DoctorReport JSON
//   - encode fail + write ok: w contains the full DoctorErrorReport JSON
//   - w.Write fail: w contains at most a prefix of ONE envelope and the
//     caller gets a non-nil error
func emitJSONReport(w io.Writer, res *doctorResult) error {
	primary, encErr := encodeIndentedJSON(res.toReport())
	if encErr != nil {
		// The success envelope itself could not be encoded (unreachable
		// for current types, but guard defensively). Try the error
		// envelope on a fresh buffer, then perform the single atomic
		// write of whichever envelope we produced.
		fallback, fErr := encodeIndentedJSON(newDoctorErrorReport(encErr))
		if fErr != nil {
			return fmt.Errorf("doctor: primary encode failed (%v); fallback envelope also failed: %w", encErr, fErr)
		}
		if _, wErr := w.Write(fallback); wErr != nil {
			return fmt.Errorf("doctor: encode error %v; fallback write failed: %w", encErr, wErr)
		}
		return encErr
	}

	if _, err := w.Write(primary); err != nil {
		// Atomic contract: no retry, no fallback. The writer is
		// considered broken; any further Write would interleave bytes.
		return err
	}
	return nil
}

// encodeIndentedJSON marshals v into indented JSON (2-space) with a
// trailing newline, identical to json.Encoder's behaviour. Returning
// bytes (not streaming into w) is the core of the atomic contract.
func encodeIndentedJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ── collectors ───────────────────────────────────────────────────────────────

func collectBinaryInfo(res *doctorResult, quiet bool) {
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

	if quiet {
		return
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
			"reinstale: scripts/install.sh")
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

// checkDualInstallPaths detecta coexistência de ~/.orbit/bin/orbit (canônico,
// install.sh) e /usr/local/bin/orbit (alt, build_orbit.sh). Ambos presentes
// = risco de versões divergentes sendo resolvidas por diferentes invocadores.
// ORBIT_STRICT_PATH=1 eleva o resultado de WARNING para CRITICAL → exit 1.
func checkDualInstallPaths(res *doctorResult) {
	home, _ := os.UserHomeDir()
	userPath := filepath.Join(home, ".orbit", "bin", "orbit")
	strict := os.Getenv("ORBIT_STRICT_PATH") == "1"
	checkDualInstallPathsAt(res, userPath, expectedInstallPath, res.currentBinary, strict)
}

// checkDualInstallPathsAt é a forma pura/testável de checkDualInstallPaths.
//
//	userPath      — caminho canônico (~/.orbit/bin/orbit)
//	sysPath       — caminho alternativo (/usr/local/bin/orbit)
//	activeBinary  — binário que o PATH resolve atualmente (res.currentBinary)
//	strict        — ORBIT_STRICT_PATH=1 → CRITICAL em vez de WARNING
func checkDualInstallPathsAt(res *doctorResult, userPath, sysPath, activeBinary string, strict bool) {
	_, userErr := os.Stat(userPath)
	_, sysErr := os.Stat(sysPath)
	userExists := userErr == nil
	sysExists := sysErr == nil

	if userExists && sysExists {
		active := activeBinary
		if active == "" {
			active = "(desconhecido)"
		}
		sev := sevWarning
		if strict {
			sev = sevCritical
		}
		res.add("Dual install paths", sev,
			fmt.Sprintf("ambos presentes: %s  +  %s  (ativo via PATH: %s)", userPath, sysPath, active),
			"rm -f "+shellQuote(sysPath)+"  # mantenha apenas o canônico (~/.orbit/bin)")
		return
	}
	if !userExists && !sysExists {
		return // nenhum instalado — coberto por checkUniqueOrbit
	}
	activeOne := userPath
	if sysExists {
		activeOne = sysPath
	}
	res.add("Dual install paths", sevOK,
		fmt.Sprintf("único caminho ativo: %s", activeOne), "")
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
			"rebuild: scripts/install.sh")
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
