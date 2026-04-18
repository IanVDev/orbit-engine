package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestAnalyzeEmitHighRisk_FormatAndSilence cobre os dois contratos do comando:
//
//  1. SILÊNCIO quando não há risco >= HIGH → saída vazia, exit 0.
//  2. FORMATO EXATO quando há CRITICAL: três linhas canônicas por pattern.
//
// É um anti-regression de contrato: se alguém downgradar CRITICAL para
// WARNING ou mudar a renderização, o teste quebra.
func TestAnalyzeEmitHighRisk_FormatAndSilence(t *testing.T) {
	t.Run("silent when no high risk", func(t *testing.T) {
		res := &doctorResult{checks: []check{
			{name: "path-order", severity: sevWarning, detail: "x", fixHint: "y"},
			{name: "ok-check", severity: sevOK, detail: "fine"},
		}}
		var buf bytes.Buffer
		emitHighRisk(&buf, res)
		if got := buf.String(); got != "" {
			t.Fatalf("esperava silêncio, obteve: %q", got)
		}
	})

	t.Run("emits canonical format on CRITICAL", func(t *testing.T) {
		res := &doctorResult{checks: []check{
			{name: "ok-check", severity: sevOK, detail: "fine"},
			{
				name:     "Commit stamp (ldflags)",
				severity: sevCritical,
				detail:   `Commit="unknown" — build sem -X`,
				fixHint:  "scripts/install.sh",
			},
			{name: "warn-only", severity: sevWarning, detail: "noise", fixHint: "ignore"},
		}}
		var buf bytes.Buffer
		emitHighRisk(&buf, res)
		out := buf.String()

		// Deve conter exatamente o CRITICAL; nenhuma trace do OK/WARNING.
		if !strings.Contains(out, "⚠️ Pattern detected: Commit stamp (ldflags)") {
			t.Errorf("header ausente: %q", out)
		}
		if !strings.Contains(out, "Risk: HIGH") {
			t.Errorf("nível de risco ausente/errado: %q", out)
		}
		if !strings.Contains(out, "Context:") {
			t.Errorf("linha Context ausente: %q", out)
		}
		if !strings.Contains(out, "Action: scripts/install.sh") {
			t.Errorf("action ausente/errado: %q", out)
		}
		if strings.Contains(out, "warn-only") || strings.Contains(out, "ok-check") {
			t.Errorf("vazou check não-HIGH: %q", out)
		}
	})

	t.Run("four-line block with ordered header/Risk/Context/Action", func(t *testing.T) {
		res := &doctorResult{checks: []check{{
			name:     "Commit stamp (ldflags)",
			severity: sevCritical,
			detail:   "Commit=\"unknown\"",
			fixHint:  "scripts/install.sh",
		}}}
		var buf bytes.Buffer
		emitHighRisk(&buf, res)

		lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
		if len(lines) != 4 {
			t.Fatalf("esperava exatamente 4 linhas de conteúdo, obteve %d: %q", len(lines), lines)
		}
		if !strings.HasPrefix(lines[0], "⚠️ Pattern detected: ") {
			t.Errorf("linha 1 deveria ser header, foi: %q", lines[0])
		}
		if !strings.HasPrefix(lines[1], "Risk: HIGH") {
			t.Errorf("linha 2 deveria começar com 'Risk: HIGH', foi: %q", lines[1])
		}
		if !strings.HasPrefix(lines[2], "Context: ") || len(lines[2]) < len("Context: ")+10 {
			t.Errorf("linha 3 deveria ser Context não-vazio, foi: %q", lines[2])
		}
		if !strings.HasPrefix(lines[3], "Action: ") {
			t.Errorf("linha 4 deveria começar com 'Action: ', foi: %q", lines[3])
		}
		// Garante que Context é single-line (não rompe o contrato de 4 linhas).
		if strings.Count(lines[2], "\n") != 0 {
			t.Errorf("Context deve ser uma única linha: %q", lines[2])
		}
	})

	t.Run("falls back to detail when fixHint empty", func(t *testing.T) {
		res := &doctorResult{checks: []check{{
			name:     "Tracking-server /health",
			severity: sevCritical,
			detail:   "inacessível: connection refused",
		}}}
		var buf bytes.Buffer
		emitHighRisk(&buf, res)
		if !strings.Contains(buf.String(), "Action: inacessível: connection refused") {
			t.Errorf("fallback para detail falhou: %q", buf.String())
		}
	})
}

// TestAnalyzeDeprecationMsg garante que a string canônica do aviso
// permanece estável — hooks/CI podem grepar por ela.
func TestAnalyzeDeprecationMsg(t *testing.T) {
	if !strings.Contains(analyzeDeprecationMsg, "deprecated") {
		t.Errorf("mensagem de deprecation deve conter 'deprecated': %q", analyzeDeprecationMsg)
	}
	if !strings.Contains(analyzeDeprecationMsg, "doctor --alert-only") {
		t.Errorf("mensagem deve apontar para 'doctor --alert-only': %q", analyzeDeprecationMsg)
	}
}
