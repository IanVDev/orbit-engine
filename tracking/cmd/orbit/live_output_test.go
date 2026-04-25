package main

import (
	"strings"
	"testing"
	"time"
)

// TestRun_LiveOutputRedactsSecretsBeforePrinting é o teste crítico de segurança.
// Garante que nenhum secret aparece no display (terminal) — apenas [REDACTED].
func TestRun_LiveOutputRedactsSecretsBeforePrinting(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, true)
	lo.Start()

	secrets := []byte(
		"Authorization: Bearer abc123\n" +
			"password=supersecret\n" +
			"x-authorization: token-real\n",
	)
	lo.StdoutWriter().Write(secrets)
	lo.Stop()

	out := display.String()

	for _, forbidden := range []string{"abc123", "supersecret", "token-real"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("display vazou segredo %q (não redatado)", forbidden)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Error("display deve conter [REDACTED] para os segredos acima")
	}
}

// TestRun_LiveOutputDoesNotChangeProofBytes garante que RawBytes() preserva
// o conteúdo original intacto — I-PROOF.
func TestRun_LiveOutputDoesNotChangeProofBytes(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, true)
	lo.Start()

	original := "Authorization: Bearer abc123\npassword=supersecret\n"
	lo.StdoutWriter().Write([]byte(original))
	lo.Stop()

	raw := string(lo.RawBytes())
	if raw != original {
		t.Errorf("RawBytes() alterou os bytes originais:\ngot:  %q\nwant: %q", raw, original)
	}

	if strings.Contains(display.String(), "abc123") {
		t.Error("display vaza 'abc123' — redaction falhou no terminal")
	}
}

func TestRun_LiveOutputShowsStdoutInInteractiveMode(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, true)
	lo.Start()

	lo.StdoutWriter().Write([]byte("hello world\n"))
	lo.Stop()

	if !strings.Contains(display.String(), "hello world") {
		t.Errorf("display deve conter 'hello world', got: %q", display.String())
	}
	if lo.Lines == 0 {
		t.Error("Lines deve ser > 0 após output")
	}
}

func TestRun_LiveOutputShowsStderrWithPrefix(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, true)
	lo.Start()

	lo.StderrWriter().Write([]byte("error message\n"))
	lo.Stop()

	out := display.String()
	if !strings.Contains(out, "[stderr]") {
		t.Errorf("stderr deve ser prefixado com [stderr], got: %q", out)
	}
	if !strings.Contains(out, "error message") {
		t.Errorf("mensagem de stderr deve aparecer no display, got: %q", out)
	}
}

// TestRun_LiveOutputDisabledInJSONMode garante que enabled=false não imprime
// nada no display, mas ainda captura para proof.
func TestRun_LiveOutputDisabledInJSONMode(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, false)
	lo.Start()

	lo.StdoutWriter().Write([]byte("should not display\n"))
	lo.Stop()

	if display.Len() > 0 {
		t.Errorf("display deve estar vazio quando disabled, got: %q", display.String())
	}
	if len(lo.RawBytes()) == 0 {
		t.Error("RawBytes deve capturar mesmo quando display está desabilitado")
	}
}

// TestRun_LiveOutputHeartbeatWhenCommandIsSilent verifica que o heartbeat
// é emitido quando o comando fica silencioso por mais de silenceGrace.
func TestRun_LiveOutputHeartbeatWhenCommandIsSilent(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, true)
	lo.silenceGrace = 50 * time.Millisecond

	lo.Start()
	time.Sleep(200 * time.Millisecond)
	lo.Stop()

	if !strings.Contains(display.String(), "still running") {
		t.Errorf("heartbeat deve aparecer após silêncio, got: %q", display.String())
	}
}

func TestRun_LiveOutputCountsRedactions(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, true)
	lo.Start()

	lo.StdoutWriter().Write([]byte("Bearer tokenXYZ\n"))
	lo.StdoutWriter().Write([]byte("normal line\n"))
	lo.Stop()

	if lo.Redactions != 1 {
		t.Errorf("Redactions = %d, want 1", lo.Redactions)
	}
	if lo.Lines != 2 {
		t.Errorf("Lines = %d, want 2", lo.Lines)
	}
}

func TestRun_LiveOutputTruncatesLongLines(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, true)
	lo.maxLineLen = 10
	lo.Start()

	lo.StdoutWriter().Write([]byte("abcdefghijklmnopqrstuvwxyz\n"))
	lo.Stop()

	if lo.Truncated != 1 {
		t.Errorf("Truncated = %d, want 1", lo.Truncated)
	}
	out := display.String()
	if strings.Contains(out, "lmnopqrstuvwxyz") {
		t.Error("conteúdo além do limite não deve aparecer no display")
	}
	if !strings.Contains(out, "truncated") {
		t.Error("display deve indicar truncamento")
	}
}

// TestRun_LiveOutputRawBytesIsOrderedCapture confirma que RawBytes acumula
// stdout e stderr na ordem de chegada, sem perder bytes.
func TestRun_LiveOutputRawBytesIsOrderedCapture(t *testing.T) {
	var display strings.Builder
	lo := NewLiveOutput(&display, false) // display desabilitado

	lo.StdoutWriter().Write([]byte("A"))
	lo.StderrWriter().Write([]byte("B"))
	lo.StdoutWriter().Write([]byte("C"))

	raw := lo.RawBytes()
	if string(raw) != "ABC" {
		t.Errorf("RawBytes = %q, want %q", raw, "ABC")
	}
}
