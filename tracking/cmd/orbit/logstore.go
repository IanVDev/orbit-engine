// logstore.go — persistência append-only por-execução em ~/.orbit/logs/.
//
// Um arquivo JSON por `orbit run`, com nome no padrão esperado pelo parser
// (scripts/parse_orbit_events.py): {ts}_{sid8}_exit{code}.json
//
// Fail-closed: erro de escrita é retornado ao caller, que deve exibir via
// stderr. O exit code do comando executado NÃO é alterado por falha no log.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

const logsDirName = "logs"

// LogSchemaVersion é a versão do schema gravado em cada log por-execução.
// Incrementar só em mudanças incompatíveis do shape consumido pelo parser.
const LogSchemaVersion = 1

// WriteExecutionLog serializa o RunResult como JSON em
// $ORBIT_HOME/logs/{ts}_{sid8}_exit{code}.json.
// Retorna o path absoluto do arquivo criado.
func WriteExecutionLog(result RunResult) (string, error) {
	if result.SessionID == "" {
		return "", errors.New("logstore: session_id obrigatório")
	}
	if result.Timestamp == "" {
		return "", errors.New("logstore: timestamp obrigatório")
	}

	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return "", fmt.Errorf("logstore: resolve home: %w", err)
	}
	dir := filepath.Join(base, logsDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("logstore: mkdir %q: %w", dir, err)
	}

	fname := logFilename(result)
	path := filepath.Join(dir, fname)

	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("logstore: marshal: %w", err)
	}

	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", fmt.Errorf("logstore: write %q: %w", path, err)
	}
	return path, nil
}

// ListExecutionLogs devolve os paths de logs em $ORBIT_HOME/logs/, ordenados
// pelo nome do arquivo. O nome começa com RFC3339Nano com ':' → '-', então
// ordem lexicográfica == ordem cronológica (dentro de mesma zona UTC).
func ListExecutionLogs() ([]string, error) {
	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, logsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// findPreviousBodyHash devolve o body_hash do último log por timestamp.
// "" = sem predecessor (genesis) OU predecessor legado (sem body_hash).
// Em ambos os casos o novo log vira um novo ponto de ancoragem na chain.
func findPreviousBodyHash() (string, error) {
	paths, err := ListExecutionLogs()
	if err != nil || len(paths) == 0 {
		return "", err
	}
	data, err := os.ReadFile(paths[len(paths)-1])
	if err != nil {
		return "", err
	}
	var r struct {
		BodyHash string `json:"body_hash"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", err
	}
	return r.BodyHash, nil
}

// logFilename gera o nome do arquivo conforme o padrão do parser.
// Regex parser: _([0-9a-f]{8})_exit\d+\.json$
// Formato timestamp: RFC3339Nano com ':' substituído por '-' (safe em FS).
func logFilename(r RunResult) string {
	ts := strings.ReplaceAll(r.Timestamp, ":", "-")
	// session_id formato "run-<nanos>"; derivamos 8 hex chars estáveis.
	sid8 := shortSessionHex(r.SessionID, r.Timestamp)
	return fmt.Sprintf("%s_%s_exit%d.json", ts, sid8, r.ExitCode)
}

// shortSessionHex devolve 8 chars hex determinísticos a partir do
// session_id + timestamp. Usa proof do próprio RunResult quando disponível
// (via ComputeHash faz isso naturalmente). Fallback: hash do session_id.
func shortSessionHex(sessionID, timestamp string) string {
	h := tracking.ComputeHash(sessionID, parseRFC3339OrNow(timestamp), 0)
	if len(h) >= 8 {
		return h[:8]
	}
	return "00000000"
}

func parseRFC3339OrNow(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now().UTC()
}

// VerifyExecutionLog relê o arquivo recém-escrito e valida integridade mínima
// contra o RunResult esperado. Defesa em profundidade contra filesystem
// silencioso (ENOSPC mascarado, FS corrompido, race de chmod).
//
// Fail-closed: qualquer divergência retorna erro. Caller (run.go) escala
// para CRITICAL e incrementa execution_without_log_total.
func VerifyExecutionLog(path string, expected RunResult) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %q: %w", path, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("arquivo vazio: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}

	// Struct minimal: só os campos que nos importam para integridade.
	var got struct {
		Version   int    `json:"version"`
		SessionID string `json:"session_id"`
		Timestamp string `json:"timestamp"`
		ExitCode  int    `json:"exit_code"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		return fmt.Errorf("unmarshal %q: %w", path, err)
	}

	if got.Version < 1 {
		return fmt.Errorf("version inválida (%d) em %s", got.Version, path)
	}
	if got.SessionID != expected.SessionID {
		return fmt.Errorf("session_id divergente: got %q, want %q",
			got.SessionID, expected.SessionID)
	}
	if got.Timestamp != expected.Timestamp {
		return fmt.Errorf("timestamp divergente: got %q, want %q",
			got.Timestamp, expected.Timestamp)
	}
	if got.ExitCode != expected.ExitCode {
		return fmt.Errorf("exit_code divergente: got %d, want %d",
			got.ExitCode, expected.ExitCode)
	}

	// Integridade do corpo: se o log carrega body_hash, recompute e compare.
	// Back-compat: logs antigos sem body_hash passam silenciosamente.
	var full RunResult
	if err := json.Unmarshal(data, &full); err != nil {
		return fmt.Errorf("unmarshal full %q: %w", path, err)
	}
	if full.BodyHash != "" {
		recomputed, err := CanonicalHash(full)
		if err != nil {
			return fmt.Errorf("body_hash recompute %q: %w", path, err)
		}
		if recomputed != full.BodyHash {
			return fmt.Errorf("body_hash mismatch em %s (log adulterado)",
				filepath.Base(path))
		}
	}
	return nil
}
