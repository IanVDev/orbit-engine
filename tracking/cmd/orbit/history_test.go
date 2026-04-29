// history_test.go — anti-regressão para orbit history.
//
// Garante que:
//   - Segredos nos campos sensíveis não aparecem na saída (tabela, detail, JSON).
//   - Registros inválidos (sem session_id) não são exibidos como confiáveis.
//   - Registros são ordenados do mais recente para o mais antigo.
//   - --failed filtra apenas exit_code != 0.
//   - --json retorna envelope estruturado com integrity_status DEGRADED quando
//     há inválidos.
//   - --detail mostra view completa apenas para o registro correspondente.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeHistoryTestLog escreve um historyRecord como JSON em dir/name.
func writeHistoryTestLog(t *testing.T, dir, name string, r historyRecord) {
	t.Helper()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// setupHistoryTestEnv cria ORBIT_HOME temporário com 4 logs:
//
//	run-1000  2026-04-28T10:00:00Z  OK    go      (oldest)
//	run-2000  2026-04-28T11:00:00Z  OK    make    (contém segredo)
//	run-3000  2026-04-28T12:00:00Z  FAIL  flutter (newest)
//	invalid   sem session_id        —     bad     (registro inválido)
func setupHistoryTestEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)
	logsDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	writeHistoryTestLog(t, logsDir,
		"2026-04-28T10-00-00.000000000Z_aaa_exit0.json",
		historyRecord{
			Version:   1,
			SessionID: "run-1000",
			Timestamp: "2026-04-28T10:00:00Z",
			Command:   "go",
			ExitCode:  0,
			DurationMs: 1000,
		},
	)

	writeHistoryTestLog(t, logsDir,
		"2026-04-28T11-00-00.000000000Z_bbb_exit0.json",
		historyRecord{
			Version:        1,
			SessionID:      "run-2000",
			Timestamp:      "2026-04-28T11:00:00Z",
			Command:        "make",
			ExitCode:       0,
			DurationMs:     2000,
			Output:         "token=abc123secret results here",
			DecisionReason: "token=abc123secret was detected",
			Args:           []string{"--api-key=abc123secret", "build"},
		},
	)

	writeHistoryTestLog(t, logsDir,
		"2026-04-28T12-00-00.000000000Z_ccc_exit1.json",
		historyRecord{
			Version:   1,
			SessionID: "run-3000",
			Timestamp: "2026-04-28T12:00:00Z",
			Command:   "flutter",
			ExitCode:  1,
			DurationMs: 3000,
		},
	)

	// Registro inválido: sem session_id.
	invalidJSON := `{"version":1,"timestamp":"2026-04-28T09:00:00Z","command":"bad"}`
	if err := os.WriteFile(
		filepath.Join(logsDir, "2026-04-28T09-00-00.000000000Z_inv_exit0.json"),
		[]byte(invalidJSON), 0o600,
	); err != nil {
		t.Fatal(err)
	}

	return logsDir
}

func TestHistoryCommandRedactsSensitiveData(t *testing.T) {
	setupHistoryTestEnv(t)

	var out, errOut strings.Builder
	err := runHistoryTo(&out, &errOut, []string{})

	// Deve retornar exitCodeError com código 2 (registro inválido).
	var exitErr *exitCodeError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Errorf("want exitCodeError{code:2}, got %v", err)
	}

	// Segredo NÃO deve aparecer na saída da tabela.
	if strings.Contains(out.String(), "abc123secret") {
		t.Errorf("segredo vazou na saída da tabela:\n%s", out.String())
	}

	// Registro inválido não deve aparecer como session_id confiável.
	// (O JSON sem session_id não tem valor para aparecer na tabela.)
	if strings.Contains(out.String(), `"bad"`) {
		t.Errorf("registro inválido apareceu como confiável:\n%s", out.String())
	}
}

func TestHistoryCommandOrdering(t *testing.T) {
	setupHistoryTestEnv(t)

	var out, errOut strings.Builder
	_ = runHistoryTo(&out, &errOut, []string{})

	output := out.String()
	pos3000 := strings.Index(output, "run-3000")
	pos1000 := strings.Index(output, "run-1000")

	if pos3000 == -1 {
		t.Fatal("run-3000 não encontrado na saída")
	}
	if pos1000 == -1 {
		t.Fatal("run-1000 não encontrado na saída")
	}
	// Mais recente (run-3000, 12h) deve aparecer antes (posição menor) que run-1000 (10h).
	if pos3000 >= pos1000 {
		t.Errorf("ordering errada: run-3000 (pos=%d) deve vir antes de run-1000 (pos=%d)\n%s",
			pos3000, pos1000, output)
	}
}

func TestHistoryCommandJSONRedacts(t *testing.T) {
	setupHistoryTestEnv(t)

	var out, errOut strings.Builder
	_ = runHistoryTo(&out, &errOut, []string{"--json"})

	raw := out.String()

	// Segredo NÃO deve aparecer no JSON.
	if strings.Contains(raw, "abc123secret") {
		t.Errorf("segredo vazou no JSON:\n%s", raw)
	}

	// Deve ser JSON válido.
	var payload historyJSONOutput
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("--json não é JSON válido: %v\n%s", err, raw)
	}

	// integrity_status deve ser DEGRADED porque há um registro inválido.
	if payload.IntegrityStatus != "DEGRADED" {
		t.Errorf("want integrity_status=DEGRADED, got %s", payload.IntegrityStatus)
	}

	// Deve ter exatamente 3 registros válidos.
	if len(payload.Records) != 3 {
		t.Errorf("want 3 records, got %d", len(payload.Records))
	}

	// Ordering no JSON: mais recente primeiro.
	if len(payload.Records) >= 2 {
		if payload.Records[0].SessionID != "run-3000" {
			t.Errorf("primeiro registro JSON deve ser run-3000, got %s", payload.Records[0].SessionID)
		}
	}
}

func TestHistoryCommandDetailRedacts(t *testing.T) {
	setupHistoryTestEnv(t)

	var out, errOut strings.Builder
	err := runHistoryTo(&out, &errOut, []string{"--detail", "run-2000"})

	// run-2000 é válido, mas há registro inválido → exit 2.
	var exitErr *exitCodeError
	if !errors.As(err, &exitErr) || exitErr.code != 2 {
		t.Errorf("want exitCodeError{code:2}, got %v", err)
	}

	// Segredo NÃO deve aparecer na saída de detail.
	if strings.Contains(out.String(), "abc123secret") {
		t.Errorf("segredo vazou no detail:\n%s", out.String())
	}

	// [REDACTED] deve aparecer (token foi mascarado).
	if !strings.Contains(out.String(), "[REDACTED]") {
		t.Errorf("quero [REDACTED] no detail, got:\n%s", out.String())
	}

	// Deve mostrar session_id correto.
	if !strings.Contains(out.String(), "run-2000") {
		t.Errorf("session_id run-2000 ausente no detail:\n%s", out.String())
	}
}

func TestHistoryCommandFailedFilter(t *testing.T) {
	setupHistoryTestEnv(t)

	var out, errOut strings.Builder
	_ = runHistoryTo(&out, &errOut, []string{"--failed"})

	output := out.String()

	// run-3000 (exit 1) deve aparecer.
	if !strings.Contains(output, "run-3000") {
		t.Errorf("--failed deve exibir run-3000 (exit 1):\n%s", output)
	}

	// run-1000 e run-2000 (exit 0) NÃO devem aparecer.
	if strings.Contains(output, "run-1000") {
		t.Errorf("--failed não deve exibir run-1000 (exit 0):\n%s", output)
	}
	if strings.Contains(output, "run-2000") {
		t.Errorf("--failed não deve exibir run-2000 (exit 0):\n%s", output)
	}
}

func TestHistoryCommandEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}

	var out, errOut strings.Builder
	err := runHistoryTo(&out, &errOut, []string{})
	if err != nil {
		t.Errorf("diretório vazio deve retornar nil, got %v", err)
	}
	if !strings.Contains(out.String(), "Nenhum registro") {
		t.Errorf("want 'Nenhum registro' na saída, got:\n%s", out.String())
	}
}

func TestHistoryCommandLimit(t *testing.T) {
	setupHistoryTestEnv(t)

	var out, errOut strings.Builder
	_ = runHistoryTo(&out, &errOut, []string{"--limit", "1"})

	output := out.String()
	// Com limit=1, só o mais recente (run-3000) deve aparecer.
	if !strings.Contains(output, "run-3000") {
		t.Errorf("--limit 1 deve exibir run-3000:\n%s", output)
	}
	if strings.Contains(output, "run-1000") || strings.Contains(output, "run-2000") {
		t.Errorf("--limit 1 não deve exibir run-1000 nem run-2000:\n%s", output)
	}
}
