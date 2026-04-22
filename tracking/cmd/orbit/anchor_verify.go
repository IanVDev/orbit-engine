// anchor_verify.go — valida logs locais contra o receipt AURYA mais recente.
//
// Gap fechado: apagar ou truncar ~/.orbit/logs antes de `verify --chain` era
// imperceptível (sem logs → nada a verificar). Com anchor, o receipt exige
// que as N primeiras folhas atuais sejam exatamente as N folhas ancoradas —
// qualquer deleção/reorder/adulteração rompe a comparação.
//
// Integração: runVerifyChain chama verifyAgainstLatestAnchor após a chain OK.
// Se não há receipt, é no-op (back-compat com instalações pré-anchor).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/IanVDev/orbit-engine/tracking"
)

// collectLeafHashes devolve os body_hashes de todos os logs atuais na ordem
// estável de ListExecutionLogs. Compartilhado por `anchor` e por `verify`
// para garantir que o root é computado sobre a MESMA sequência em ambos.
func collectLeafHashes() ([]string, error) {
	paths, err := ListExecutionLogs()
	if err != nil {
		return nil, fmt.Errorf("anchor: list: %w", err)
	}
	var leaves []string
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("anchor: read %q: %w", p, err)
		}
		var r RunResult
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("anchor: unmarshal %q: %w", p, err)
		}
		if r.BodyHash == "" {
			continue
		}
		leaves = append(leaves, r.BodyHash)
	}
	return leaves, nil
}

// loadLatestAnchor devolve o receipt mais recente em $ORBIT_HOME/anchors/,
// ou (nil, "", nil) se o diretório/arquivos não existirem. Ausência ≠ erro
// para permitir verify em instalações que ainda não ancoraram.
func loadLatestAnchor() (*AnchorReceipt, string, error) {
	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(base, "anchors")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return nil, "", nil
	}
	sort.Strings(files)
	path := filepath.Join(dir, files[len(files)-1])
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var r AnchorReceipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, "", err
	}
	return &r, path, nil
}
