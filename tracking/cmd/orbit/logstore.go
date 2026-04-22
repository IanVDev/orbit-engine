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
	"strconv"
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

	// I13 LOG_RETENTION: prune síncrono após cada write (best-effort).
	// Fail-soft: erro no prune é ignorado para não derrubar o run bem-sucedido,
	// mas o cap é aplicado — remover esta chamada quebra TestLogRotationEnforced.
	_ = pruneOldLogs(dir)
	return path, nil
}

// pruneOldLogs mantém no máximo ORBIT_MAX_LOGS arquivos em `dir`, removendo
// os mais antigos (por mtime). Default: 10000 (generoso — cobre ~1 ano de
// uso típico de 30 runs/dia). Defina ORBIT_MAX_LOGS=N para ajustar.
//
// Retorna erro apenas em I/O irrecuperável; best-effort por design.
func pruneOldLogs(dir string) error {
	max := 10000
	if v := os.Getenv("ORBIT_MAX_LOGS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			max = n
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) <= max {
		return nil
	}
	// Ordena por mtime ascendente (mais antigo primeiro).
	type fe struct {
		name  string
		mtime time.Time
	}
	items := make([]fe, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, fe{e.Name(), info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mtime.Before(items[j].mtime) })
	// Remove o excesso.
	excess := len(items) - max
	for i := 0; i < excess; i++ {
		_ = os.Remove(filepath.Join(dir, items[i].name))
	}
	return nil
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
