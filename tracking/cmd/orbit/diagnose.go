// diagnose.go — comando `orbit diagnose [log_file]`.
//
// Lê o último log (ou um log específico) em $ORBIT_HOME/logs/ e tenta
// extrair causa provável da falha a partir do campo `output`.
//
// Escopo (mínimo deliberado):
//   - parser para `go test`: captura `--- FAIL: TestName` + `file.go:line: msg`
//   - integra snapshot: o caminho já está no log, só propagamos
//
// Níveis de confiança:
//
//	high   — casou `--- FAIL` + `file:line:` na mesma saída
//	medium — casou apenas `file:line:` (sem marcador de teste)
//	none   — não casou nada conhecido (fail-closed: sem inferência)
//
// Fail-closed:
//   - nenhum log em $ORBIT_HOME/logs/         → erro
//   - exit_code == 0                          → confidence=none, "saudável"
//   - event != TEST_RUN                       → confidence=none, não suportado
//   - padrão não reconhecido                  → confidence=none, vazio
//
// Nunca inventa file:line, nunca fabrica mensagens. Se não reconhece,
// cala — coerente com a regra "silêncio quando saudável/indeterminado".
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

// DiagnoseSchemaVersion identifica o contrato do JSON emitido. Subir só em
// mudanças incompatíveis consumidas por callers externos (dashboard, CI).
const DiagnoseSchemaVersion = 1

// Confidence é o nível de certeza da análise.
type Confidence string

const (
	ConfidenceNone   Confidence = "none"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Diagnosis é o resultado estruturado de `orbit diagnose`. Campos de erro
// ficam vazios quando Confidence == "none" (fail-closed, sem inferência).
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

// Padrões Go test. Ambos ancorados ao início de linha para reduzir
// falso-positivo em strings literais embutidas no output.
var (
	// `--- FAIL: TestFoo (0.00s)` — marcador canônico do runner.
	goTestFailRe = regexp.MustCompile(`(?m)^\s*--- FAIL: (\w[\w/]*)`)

	// `foo_test.go:23: expected X got Y` — assertion de test.T.
	// Extensão fixa `.go` evita casar em URLs ou timestamps com ':'.
	goTestLineRe = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_./\\-]+\.go):(\d+):\s*(.+)$`)
)

func runDiagnose(logPath string, jsonMode bool) error {
	return diagnoseTo(os.Stdout, logPath, jsonMode)
}

// diagnoseTo é a forma testável: escreve em w em vez de os.Stdout.
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

	// Fail-closed: só analisamos TEST_RUN com exit != 0.
	if result.Event == string(EventTestRun) && result.ExitCode != 0 {
		parseGoTestFailure(&d, result.Output)
	}

	return emitDiagnosis(w, d, jsonMode)
}

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
		// Sem file:line não há ponto acionável — cala.
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

	// Medium: file:line sem marcador de teste — ainda útil, menos seguro.
	d.ErrorType = "file_line_only"
	d.Confidence = ConfidenceMedium
}

// latestLogPath devolve o caminho do log mais recente em
// $ORBIT_HOME/logs/. Determinístico: ordena por nome decrescente
// (o prefixo é RFC3339Nano, então a maior string lexicográfica é a mais nova).
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

// loadRunResult lê um log .json e devolve o RunResult decodificado.
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
