package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRedactOutput_RedactsCommonSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		bad  []string
		good []string
	}{
		{
			name: "Bearer token",
			in:   "Authorization: Bearer eyJhbGciOi.payload.sig",
			bad:  []string{"eyJhbGciOi.payload.sig"},
			good: []string{"Bearer [REDACTED]"},
		},
		{
			name: "password equals",
			in:   "config: password=SuperSecret42",
			bad:  []string{"SuperSecret42"},
			good: []string{"password=[REDACTED]"},
		},
		{
			name: "token colon",
			in:   "token: abc123xyz",
			bad:  []string{"abc123xyz"},
			good: []string{"token: [REDACTED]"},
		},
		{
			name: "api-key dash",
			in:   "api-key: LIVE_abcdef",
			bad:  []string{"LIVE_abcdef"},
			good: []string{"api-key: [REDACTED]"},
		},
		{
			name: "api_key underscore",
			in:   "api_key=xyz.123",
			bad:  []string{"xyz.123"},
			good: []string{"api_key=[REDACTED]"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactOutput(c.in)
			for _, b := range c.bad {
				if strings.Contains(got, b) {
					t.Errorf("secret %q vazou em %q", b, got)
				}
			}
			for _, g := range c.good {
				if !strings.Contains(got, g) {
					t.Errorf("marcador esperado %q ausente em %q", g, got)
				}
			}
		})
	}
}

// TestRedact_MultilineSecrets documenta o comportamento do redactor em
// inputs multi-linha. O regex é line-bounded (o charset da value exclui
// whitespace), então cada secret é tratado independentemente na sua
// linha — mesmo que o redactor não cubra todo padrão possível, o efeito
// é previsível e nunca pior que "não redigir".
func TestRedact_MultilineSecrets(t *testing.T) {
	// Caso 1: secrets em linhas distintas — cada ocorrência é redigida.
	in := "password=line1_secret\nsomething_normal=visible\ntoken: line3_secret"
	got := redactOutput(in)
	for _, leak := range []string{"line1_secret", "line3_secret"} {
		if strings.Contains(got, leak) {
			t.Errorf("secret %q vazou em multiline: %q", leak, got)
		}
	}
	if !strings.Contains(got, "something_normal=visible") {
		t.Errorf("linha não-sensível foi alterada: %q", got)
	}

	// Caso 2: múltiplos Bearer em linhas consecutivas — todos redigidos.
	in2 := "Bearer tokenA\nBearer tokenB\nBearer tokenC"
	got2 := redactOutput(in2)
	for _, leak := range []string{"tokenA", "tokenB", "tokenC"} {
		if strings.Contains(got2, leak) {
			t.Errorf("Bearer %q não foi redigido em multiline: %q", leak, got2)
		}
	}

	// Caso 3: valor que termina ao encontrar \n — comportamento previsível.
	// O regex para ao encontrar whitespace; o conteúdo da linha seguinte
	// NÃO é parte do secret e NÃO é modificado. Se o usuário tivesse um
	// secret que legitimamente atravessa \n, este redactor não o cobre —
	// esse limite é aceito e documentado aqui.
	in3 := "password=single_line_secret\nunrelated next line content"
	got3 := redactOutput(in3)
	if strings.Contains(got3, "single_line_secret") {
		t.Errorf("secret da primeira linha deveria ter sido redigido: %q", got3)
	}
	if !strings.Contains(got3, "unrelated next line content") {
		t.Errorf("conteúdo da linha seguinte não deveria ser tocado: %q", got3)
	}
}

func TestRedactOutput_PreservesNonSecrets(t *testing.T) {
	in := "hello world\nexit code 0\ncompiling package foo...\n"
	if got := redactOutput(in); got != in {
		t.Errorf("output alterado indevidamente:\n  in : %q\n  got: %q", in, got)
	}
}

// TestRun_RedactsSecrets (obrigatório) — garante que secrets no output
// capturado NÃO chegam ao log persistido em $ORBIT_HOME/logs/. Reproduz
// o fluxo crítico de run.go: redact → WriteExecutionLog.
func TestRun_RedactsSecrets(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	raw := "calling API with Authorization: Bearer eyJxyz.signed.payload\npassword=SuperSecret42\napi-key: LIVE_zzz\n"
	rawArg := "token=arg_leak_123"
	redactedArgs := []string{redactOutput(rawArg)}
	result := RunResult{
		Version:   LogSchemaVersion,
		Command:   "echo",
		Args:      redactedArgs,
		ExitCode:  0,
		Output:    redactOutput(raw),
		Proof:     "deadbeef",
		SessionID: "run-secret-test",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	path, err := WriteExecutionLog(result)
	if err != nil {
		t.Fatalf("WriteExecutionLog: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	for _, leak := range []string{"eyJxyz.signed.payload", "SuperSecret42", "LIVE_zzz", "arg_leak_123"} {
		if bytes.Contains(data, []byte(leak)) {
			t.Errorf("secret %q vazou no log %s", leak, path)
		}
	}
	if !bytes.Contains(data, []byte("[REDACTED]")) {
		t.Error("marcador [REDACTED] ausente no log persistido")
	}
}
