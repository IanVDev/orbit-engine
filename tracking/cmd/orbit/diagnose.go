// diagnose.go — análise pós-hoc de um log de execução.
//
// Este arquivo é o DOMICÍLIO ÚNICO de toda detecção de local/causa de
// falha: regex de file:line, marcadores de frameworks de teste, e a
// heurística de confiança high/medium/none. guidance.go consome daqui
// (firstFileLine) em vez de manter parser próprio — ver guardião
// `TestGuidanceAndDiagnoseAgreeOnLocation`.
//
// Contratos:
//
//	CLI:    orbit diagnose [log_file] [--json]
//	Input:  $ORBIT_HOME/logs/*.json (RunResult)
//	Output: Diagnosis (JSON ou bloco humano)
//
// Fast-path: se o log já trouxer `diagnosis` persistido (escrito pelo
// run.go no momento da execução), reaproveitamos sem re-parsear — o
// parser roda uma única vez por execução, por definição.
//
// Níveis de confiança:
//
//	high   — `--- FAIL: TestName` + `file.go:line: msg` no output
//	medium — file:line com mensagem, sem marcador de teste
//	none   — fail-closed: padrão desconhecido, campos vazios
//
// Nunca inventa file:line a partir de texto arbitrário.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/IanVDev/orbit-engine/tracking"
)

// DiagnoseSchemaVersion identifica o contrato do JSON emitido e persistido
// em RunResult.Diagnosis. Subir só em mudanças incompatíveis consumidas
// por callers externos (dashboard, CI).
const DiagnoseSchemaVersion = 1

// Confidence é o nível de certeza da análise.
type Confidence string

const (
	ConfidenceNone   Confidence = "none"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Diagnosis é o resultado completo emitido pelo CLI `orbit diagnose`.
// Campos de erro ficam vazios quando Confidence == "none" (fail-closed).
type Diagnosis struct {
	Version      int        `json:"version"`
	LogPath      string     `json:"log_path"`
	SnapshotPath string     `json:"snapshot_path,omitempty"`
	Event        string     `json:"event"`
	ExitCode     int        `json:"exit_code"`
	ErrorType    string     `json:"error_type,omitempty"`
	TestName     string     `json:"test_name,omitempty"`
	File         string     `json:"file,omitempty"`
	Line         int        `json:"line,omitempty"`
	Message      string     `json:"message,omitempty"`
	Confidence   Confidence `json:"confidence"`
}

// DiagnosisPayload é o subset persistido em RunResult.Diagnosis.
// Exclui LogPath/SnapshotPath (ambos deriváveis do contexto do log no
// momento da leitura) e Event/ExitCode (já presentes no RunResult).
// Manter o payload minimal preserva a proof (sha256 não inclui este
// campo) e reduz o diff no log.
type DiagnosisPayload struct {
	Version    int        `json:"version"`
	ErrorType  string     `json:"error_type,omitempty"`
	TestName   string     `json:"test_name,omitempty"`
	File       string     `json:"file,omitempty"`
	Line       int        `json:"line,omitempty"`
	Message    string     `json:"message,omitempty"`
	Confidence Confidence `json:"confidence"`
}

// ── Regex de detecção (todas aqui, deliberadamente) ──────────────────

// fileLineLooseRe casa tokens "path/file.ext:123" em qualquer lugar
// do texto. Extensão obrigatória (letra/número) evita casar URLs
// simples tipo "host:8080" — mas URLs com TLD ("example.com:8080")
// ainda casam, então o caller DEVE gatinguear por evento/exitcode
// antes de chamar. Usada por BuildGuidance e como fallback em
// parseGoTestFailure.
var fileLineLooseRe = regexp.MustCompile(`([A-Za-z0-9_./\\\-]+\.[A-Za-z0-9]+):(\d+)`)

// goTestFailRe casa o marcador canônico do runner Go:
// `--- FAIL: TestName` ou `--- FAIL: TestSuite/sub_name`.
var goTestFailRe = regexp.MustCompile(`(?m)^\s*--- FAIL: (\w[\w/]*)`)

// goTestLineRe casa assertions de test.T: `file.go:23: mensagem`.
// Ancorado ao início da linha para evitar falso-positivo em strings
// literais embutidas. Extensão fixa .go reduz ambiguidade.
var goTestLineRe = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_./\\-]+\.go):(\d+):\s*(.+)$`)

// firstFileLine devolve a primeira ocorrência de file:line em output,
// com parsing numérico já aplicado. API interna usada tanto por
// BuildGuidance quanto por parseGoTestFailure.
func firstFileLine(output string) (file string, line int, ok bool) {
	if output == "" {
		return "", 0, false
	}
	m := fileLineLooseRe.FindStringSubmatch(output)
	if len(m) < 3 {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// ── CLI entrypoint ───────────────────────────────────────────────────

func runDiagnose(logPath string, jsonMode bool) error {
	return diagnoseTo(os.Stdout, logPath, jsonMode)
}

// diagnoseTo é a forma testável: escreve em w em vez de os.Stdout.
// Fast-path: se o RunResult já traz `diagnosis` persistido, reaproveita.
// Slow-path: computa a partir do output, mesmo pipeline do run.go.
func diagnoseTo(w io.Writer, logPath string, jsonMode bool) error {
	if logPath == "" {
		latest, err := latestLogPath()
		if err != nil {
			return err
		}
		logPath = latest
	}

	result, err := loadRunResult(logPath)
	if err != nil {
		return fmt.Errorf("diagnose: %w", err)
	}

	d := Diagnosis{
		Version:      DiagnoseSchemaVersion,
		LogPath:      logPath,
		SnapshotPath: result.SnapshotPath,
		Event:        result.Event,
		ExitCode:     result.ExitCode,
		Confidence:   ConfidenceNone,
	}

	if result.Diagnosis != nil {
		// Fast-path: parser já rodou no momento do run; só reconstitui.
		applyPayload(&d, result.Diagnosis)
	} else if result.Event == string(EventTestRun) && result.ExitCode != 0 {
		// Slow-path (log antigo, pré-persistência): re-parse.
		parseGoTestFailure(&d, result.Output)
	}

	return emitDiagnosis(w, d, jsonMode)
}

// ── Builder reutilizado pelo run.go ──────────────────────────────────

// BuildDiagnosisForRun é a superfície chamada pelo run.go no momento da
// execução. Roda o mesmo parser do CLI `orbit diagnose`, mas sem IO:
// o caller usa o retorno para (a) preencher guidance e (b) persistir
// no log via RunResult.Diagnosis.
func BuildDiagnosisForRun(event EventType, exitCode int, output string) Diagnosis {
	d := Diagnosis{
		Version:    DiagnoseSchemaVersion,
		Event:      string(event),
		ExitCode:   exitCode,
		Confidence: ConfidenceNone,
	}
	if event == EventTestRun && exitCode != 0 {
		parseGoTestFailure(&d, output)
	}
	return d
}

// ToPayload devolve o subset que deve ir para o log. Nil se a análise
// não produziu informação útil — evita poluir o log com "none".
func (d Diagnosis) ToPayload() *DiagnosisPayload {
	if d.Confidence == ConfidenceNone {
		return nil
	}
	return &DiagnosisPayload{
		Version:    d.Version,
		ErrorType:  d.ErrorType,
		TestName:   d.TestName,
		File:       d.File,
		Line:       d.Line,
		Message:    d.Message,
		Confidence: d.Confidence,
	}
}

// applyPayload copia os campos persistidos para uma Diagnosis completa,
// preservando Version/LogPath/Event/ExitCode já setados pelo caller.
func applyPayload(d *Diagnosis, p *DiagnosisPayload) {
	if p == nil {
		return
	}
	d.ErrorType = p.ErrorType
	d.TestName = p.TestName
	d.File = p.File
	d.Line = p.Line
	d.Message = p.Message
	d.Confidence = p.Confidence
	// DiagnoseSchemaVersion já está em d.Version (pode divergir se log
	// é de versão futura — mantemos a versão do payload por honestidade).
	if p.Version != 0 {
		d.Version = p.Version
	}
}

// ── Parser do Go test ────────────────────────────────────────────────

// parseGoTestFailure popula campos de d a partir do output de `go test`.
// Fail-closed: qualquer formato desconhecido deixa d inalterado
// (Confidence="none", demais campos vazios).
func parseGoTestFailure(d *Diagnosis, output string) {
	if output == "" {
		return
	}

	failMatch := goTestFailRe.FindStringSubmatch(output)
	lineMatch := goTestLineRe.FindStringSubmatch(output)

	if lineMatch == nil {
		// Sem file:line com mensagem acionável — cala.
		return
	}

	line, err := strconv.Atoi(lineMatch[2])
	if err != nil {
		return
	}

	d.File = lineMatch[1]
	d.Line = line
	d.Message = strings.TrimSpace(lineMatch[3])

	if failMatch != nil {
		d.ErrorType = "go_test_assertion"
		d.TestName = failMatch[1]
		d.Confidence = ConfidenceHigh
		return
	}

	d.ErrorType = "file_line_only"
	d.Confidence = ConfidenceMedium
}

// ── IO helpers ───────────────────────────────────────────────────────

func latestLogPath() (string, error) {
	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return "", fmt.Errorf("diagnose: resolve home: %w", err)
	}
	dir := filepath.Join(base, logsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("diagnose: listar %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return "", fmt.Errorf("diagnose: nenhum log em %s", dir)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return filepath.Join(dir, names[0]), nil
}

func loadRunResult(path string) (*RunResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("abrir %s: %w", path, err)
	}
	defer f.Close()
	var r RunResult
	if err := json.NewDecoder(f).Decode(&r); err != nil {
		return nil, fmt.Errorf("decodificar %s: %w", path, err)
	}
	return &r, nil
}

// emitDiagnosis escreve o resultado em w. JSON em modo --json; caso
// contrário um bloco humano curto e determinístico.
func emitDiagnosis(w io.Writer, d Diagnosis, jsonMode bool) error {
	if jsonMode {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(d)
	}

	fmt.Fprintf(w, "orbit diagnose — %s\n", d.LogPath)
	fmt.Fprintf(w, "  event: %s (exit %d)\n", d.Event, d.ExitCode)
	if d.SnapshotPath != "" {
		fmt.Fprintf(w, "  snapshot: %s\n", d.SnapshotPath)
	}
	fmt.Fprintln(w)

	switch {
	case d.Confidence == ConfidenceHigh || d.Confidence == ConfidenceMedium:
		fmt.Fprintf(w, "  error_type: %s\n", d.ErrorType)
		if d.TestName != "" {
			fmt.Fprintf(w, "  test:       %s\n", d.TestName)
		}
		fmt.Fprintf(w, "  at:         %s:%d\n", d.File, d.Line)
		if d.Message != "" {
			fmt.Fprintf(w, "  msg:        %s\n", d.Message)
		}
		fmt.Fprintf(w, "  confidence: %s\n", d.Confidence)
	case d.ExitCode == 0:
		fmt.Fprintln(w, "  nothing to diagnose — execução saudável.")
	case d.Event != string(EventTestRun):
		fmt.Fprintf(w, "  no diagnosis — event %q não suportado neste MVP.\n", d.Event)
	default:
		fmt.Fprintln(w, "  no diagnosis — output não casou padrão conhecido (fail-closed).")
	}
	return nil
}
