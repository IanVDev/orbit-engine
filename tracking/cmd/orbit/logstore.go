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
