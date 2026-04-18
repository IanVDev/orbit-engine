// snapshot.go — captura read-only do estado do repositório git.
//
// Conteúdo obrigatório (refinamento do plano):
//   - git_branch    (git rev-parse --abbrev-ref HEAD)
//   - last_commit   (git rev-parse HEAD)
//   - diff_stat     (git diff --stat)
//
// Fail-soft por campo: se um comando git falhar (repo não-git, git ausente),
// o campo correspondente vira "" e o snapshot é marcado incomplete=true.
// Nunca escreve em arquivos fora de $ORBIT_HOME/snapshots/.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

const snapshotsDirName = "snapshots"

// Snapshot é o payload persistido por ação TRIGGER_SNAPSHOT / TRIGGER_ANALYZE.
type Snapshot struct {
	SchemaVersion int    `json:"schema_version"`
	SessionID     string `json:"session_id"`
	Timestamp     string `json:"timestamp"`
	Reason        string `json:"reason"`
	GitBranch     string `json:"git_branch"`
	LastCommit    string `json:"last_commit"`
	DiffStat      string `json:"diff_stat"`
	Incomplete    bool   `json:"incomplete,omitempty"`
}

const snapshotSchemaVersion = 1

// TakeSnapshot coleta branch, último commit e diff-stat, persiste em
// $ORBIT_HOME/snapshots/<sessionID>.json e devolve o path criado.
// Retorna erro apenas em falhas de I/O; falhas de git são absorvidas e
// refletem-se em campos vazios + Incomplete=true.
func TakeSnapshot(sessionID, reason string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("snapshot: session_id obrigatório")
	}

	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return "", fmt.Errorf("snapshot: resolve home: %w", err)
	}
	dir := filepath.Join(base, snapshotsDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("snapshot: mkdir %q: %w", dir, err)
	}

	s := Snapshot{
		SchemaVersion: snapshotSchemaVersion,
		SessionID:     sessionID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		Reason:        reason,
	}

	branch, okB := runGit("rev-parse", "--abbrev-ref", "HEAD")
	commit, okC := runGit("rev-parse", "HEAD")
	diffStat, okD := runGit("diff", "--stat")
	s.GitBranch = branch
	s.LastCommit = commit
	s.DiffStat = diffStat
	if !okB || !okC || !okD {
		s.Incomplete = true
	}

	payload, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", fmt.Errorf("snapshot: marshal: %w", err)
	}
	path := filepath.Join(dir, sessionID+".json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", fmt.Errorf("snapshot: write %q: %w", path, err)
	}
	return path, nil
}

// runGit executa um subcomando git no CWD atual. Retorna (stdout trimmed, ok).
// ok==false em qualquer falha (binário ausente, exit != 0, timeout).
func runGit(args ...string) (string, bool) {
	c := exec.Command("git", args...)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}
