// anchor_check.go — comparação fail-closed entre logs atuais e receipt.
//
// Semântica: o receipt congelou N folhas no momento da ancoragem. Agora
// as N primeiras folhas atuais devem bater exatamente — elemento por
// elemento — e o merkle_root recomputado deve ser idêntico. Logs novos
// APÓS a ancoragem são permitidos (sufixo); qualquer alteração no PREFIXO
// ancorado é deleção, reorder ou adulteração.
package main

import (
	"fmt"
	"io"
	"path/filepath"
)

// verifyAgainstLatestAnchor é chamado por runVerifyChain após a chain OK.
// Sem receipt → no-op. Com receipt → recompute + compare, fail-closed.
func verifyAgainstLatestAnchor(w io.Writer) error {
	rec, path, err := loadLatestAnchor()
	if err != nil {
		return fmt.Errorf("anchor verify: load: %w", err)
	}
	if rec == nil {
		return nil
	}
	leaves, err := collectLeafHashes()
	if err != nil {
		return fmt.Errorf("anchor verify: %w", err)
	}
	if len(leaves) < rec.LeafCount {
		return fmt.Errorf(
			"anchor mismatch — %d folhas atuais < %d ancoradas (log apagado após anchor)",
			len(leaves), rec.LeafCount)
	}
	for i, h := range rec.LeafHashes {
		if leaves[i] != h {
			return fmt.Errorf(
				"anchor mismatch — folha %d divergente (log adulterado ou removido)", i)
		}
	}
	root, err := ComputeMerkleRoot(leaves[:rec.LeafCount])
	if err != nil {
		return fmt.Errorf("anchor verify: merkle: %w", err)
	}
	if root != rec.MerkleRoot {
		return fmt.Errorf(
			"anchor mismatch — merkle_root recomputado %s... != receipt %s...",
			safePrefix(root, 16), safePrefix(rec.MerkleRoot, 16))
	}
	fmt.Fprintf(w, "✅  anchor ok — merkle_root %s... confere (receipt %s, AURYA %s)\n",
		safePrefix(root, 16), filepath.Base(path), rec.NodeTimestamp)
	return nil
}
