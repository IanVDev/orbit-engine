// anchor.go — I15 HISTORY_ANCHOR: snapshot persistido fora de ~/.orbit.
//
// Detecta wipe de ~/.orbit/. Anchor fica em <ORBIT_HOME>.anchor (path irmão,
// não colide entre testes paralelos). Override via ORBIT_ANCHOR_PATH.
// TotalRuns é monotônico (resiliente ao prune de I13).
package tracking

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Anchor é o snapshot mínimo de continuidade.
type Anchor struct {
	SchemaVersion int    `json:"schema_version"`
	TotalRuns     int64  `json:"total_runs"`
	LastProof     string `json:"last_proof,omitempty"`
	LastTs        string `json:"last_ts,omitempty"`
}

const AnchorSchemaV1 = 1

func resolveAnchorPath() (string, error) {
	if v := os.Getenv("ORBIT_ANCHOR_PATH"); v != "" {
		return v, nil
	}
	storeHome, err := ResolveStoreHome()
	if err != nil {
		return "", err
	}
	return filepath.Clean(storeHome) + ".anchor", nil
}

// loadAnchor lê o anchor. (Anchor{}, false, nil) se não existe.
func loadAnchor() (Anchor, bool, error) {
	path, err := resolveAnchorPath()
	if err != nil {
		return Anchor{}, false, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Anchor{}, false, nil
	}
	if err != nil {
		return Anchor{}, false, err
	}
	var a Anchor
	if err := json.Unmarshal(b, &a); err != nil || a.SchemaVersion != AnchorSchemaV1 {
		return Anchor{}, false, fmt.Errorf("anchor: parse/schema: %v", err)
	}
	return a, true, nil
}

// SaveAnchor grava o anchor atomicamente (0600). Incrementa TotalRuns.
// Fail-closed em erro de escrita.
func SaveAnchor(proof, ts string) error {
	path, err := resolveAnchorPath()
	if err != nil {
		return err
	}
	prev, _, _ := loadAnchor() // erro de load tolerado: fresh start
	next := Anchor{AnchorSchemaV1, prev.TotalRuns + 1, proof, ts}
	data, _ := json.Marshal(next)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("anchor: write: %w", err)
	}
	return os.Rename(tmp, path)
}

// VerifyAnchor detecta wipe de $ORBIT_HOME/logs: se anchor registra runs
// mas o diretório não existe, retorna CRITICAL. Primeiro uso (anchor
// ausente ou TotalRuns=0) retorna nil.
func VerifyAnchor() error {
	a, exists, err := loadAnchor()
	if err != nil {
		return fmt.Errorf("anchor: %w", err)
	}
	if !exists || a.TotalRuns == 0 {
		return nil
	}
	home, err := ResolveStoreHome()
	if err != nil {
		return err
	}
	logsDir := filepath.Join(home, "logs")
	if _, err := os.Stat(logsDir); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("CRITICAL: history wipe detected — anchor reporta %d runs mas %s não existe",
			a.TotalRuns, logsDir)
	}
	return nil
}
