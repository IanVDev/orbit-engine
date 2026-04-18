// diagnose_build_test.go — cobertura do parser de `go build` e do
// dispatcher por evento (extensão natural do parser de go test).
//
// Contratos protegidos:
//
//   1. Formato canônico do compilador (com coluna) → confidence=high
//      com error_type="go_build_error".
//   2. Formato sem coluna ("file.go:line: msg") → também high.
//   3. Outputs sem file:line (import cycle, missing module) → none.
//   4. Dispatcher nunca cruza eventos: output de go build passado num
//      log event=TEST_RUN cai no parser errado e o cenário é vazio.
//   5. Guardião estendido: BuildGuidance(BUILD, ...) concorda com o
//      Diagnosis sobre file:line (mesma invariante já testada p/ TEST).
package main

import (
	"strings"
	"testing"
)

func TestParseGoBuildFailure_WithColumn(t *testing.T) {
	out := `# example.com/proj
./foo.go:15:2: undefined: bar
`
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoBuildFailure(&d, out)

	if d.Confidence != ConfidenceHigh {
		t.Fatalf("confidence = %q, want high", d.Confidence)
	}
	if d.ErrorType != "go_build_error" {
		t.Errorf("error_type = %q, want go_build_error", d.ErrorType)
	}
	if d.File != "./foo.go" {
		t.Errorf("file = %q, want ./foo.go", d.File)
	}
	if d.Line != 15 {
		t.Errorf("line = %d, want 15", d.Line)
	}
	if d.Message != "undefined: bar" {
		t.Errorf("message = %q, want 'undefined: bar'", d.Message)
	}
	if d.TestName != "" {
		t.Errorf("test_name deve ser vazio em build error, foi %q", d.TestName)
	}
}

func TestParseGoBuildFailure_WithoutColumn(t *testing.T) {
	// Alguns erros de build não têm coluna (linker, cgo).
	out := `./main.go:5: cannot use "x" (type string) as type int
`
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoBuildFailure(&d, out)

	if d.Confidence != ConfidenceHigh {
		t.Fatalf("confidence = %q, want high", d.Confidence)
	}
	if d.Line != 5 {
		t.Errorf("line = %d, want 5", d.Line)
	}
	if !strings.Contains(d.Message, "cannot use") {
		t.Errorf("message não preservada: %q", d.Message)
	}
}

func TestParseGoBuildFailure_ImportCycle_FailsClosed(t *testing.T) {
	// Import cycle não tem file:line — NÃO deve inventar.
	out := `package example.com/foo
	imports example.com/bar
	imports example.com/foo: import cycle not allowed
`
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoBuildFailure(&d, out)

	if d.Confidence != ConfidenceNone {
		t.Fatalf("confidence = %q, want none (fail-closed)", d.Confidence)
	}
	if d.File != "" || d.Line != 0 {
		t.Errorf("parser inventou localização: file=%q line=%d", d.File, d.Line)
	}
}

func TestParseGoBuildFailure_MissingModule_FailsClosed(t *testing.T) {
	out := `go: cannot find main module; see 'go help modules'`
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoBuildFailure(&d, out)

	if d.Confidence != ConfidenceNone {
		t.Errorf("confidence = %q, want none", d.Confidence)
	}
}

func TestParseGoBuildFailure_EmptyOutput(t *testing.T) {
	d := Diagnosis{Confidence: ConfidenceNone}
	parseGoBuildFailure(&d, "")
	if d.Confidence != ConfidenceNone {
		t.Errorf("output vazio não deve setar confidence")
	}
}

// ── Dispatcher ───────────────────────────────────────────────────────

func TestBuildDiagnosisForRun_DispatchByEvent(t *testing.T) {
	buildOutput := `./x.go:7:1: syntax error: unexpected }`
	testOutput := "--- FAIL: TestX\n    x_test.go:3: oops\nFAIL\n"

	// BUILD event → parser de build → error_type=go_build_error
	d := BuildDiagnosisForRun(EventBuild, 1, buildOutput)
	if d.ErrorType != "go_build_error" {
		t.Errorf("EventBuild deveria despachar p/ build: %q", d.ErrorType)
	}

	// TEST_RUN event → parser de test → error_type=go_test_assertion
	d = BuildDiagnosisForRun(EventTestRun, 1, testOutput)
	if d.ErrorType != "go_test_assertion" {
		t.Errorf("EventTestRun deveria despachar p/ test: %q", d.ErrorType)
	}

	// Evento sem parser → none mesmo com exit != 0
	d = BuildDiagnosisForRun(EventCodeChange, 1, buildOutput)
	if d.Confidence != ConfidenceNone {
		t.Errorf("eventos sem parser devem calar: %q", d.Confidence)
	}

	// Exit 0 em qualquer evento → none
	d = BuildDiagnosisForRun(EventBuild, 0, buildOutput)
	if d.Confidence != ConfidenceNone {
		t.Errorf("exit 0 deve calar: %q", d.Confidence)
	}
}

// ── Guardião estendido: guidance concorda com diagnose também em BUILD

func TestGuidanceAndDiagnose_AgreeOnBuild(t *testing.T) {
	output := `# example.com/proj
./cmd/app/main.go:42:10: undefined: Foo
`
	guidance := BuildGuidance(EventBuild, 1, output)
	if guidance == "" {
		t.Fatalf("guidance vazio em BUILD — gate não foi estendido")
	}

	d := BuildDiagnosisForRun(EventBuild, 1, output)
	if d.Confidence == ConfidenceNone {
		t.Fatalf("diagnose vazio — parser não rodou")
	}

	want := d.File + ":"
	if !strings.HasPrefix(guidance, want) {
		t.Fatalf("CONVERGÊNCIA BUILD QUEBRADA\n"+
			"  guidance: %q\n"+
			"  diagnose.File: %q", guidance, d.File)
	}

	// Linha também deve bater.
	gotLine := strings.TrimPrefix(guidance, want)
	if gotLine == "" || gotLine != itoa(d.Line) {
		t.Fatalf("linha divergente: guidance=%q diag.Line=%d",
			guidance, d.Line)
	}
}

func itoa(n int) string {
	// Evita import novo só pra isso — 3 dígitos cobrem nosso caso.
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
