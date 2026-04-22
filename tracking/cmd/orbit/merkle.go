// merkle.go — árvore de Merkle determinística sobre body_hashes.
//
// Fecha o gap G4: com body_hash + prev_proof + legacy_gap, a chain é
// tamper-evident localmente, mas um atacante com acesso a ~/.orbit/logs
// ainda pode apagar a sequência inteira. merkle_root extrai um compromisso
// único sobre todos os logs, que é então ancorado externamente em AURYA.
//
// Regras:
//   - folhas são os body_hashes em hex, mantidos na ordem do input (caller
//     passa em ordem estável de timestamp via ListExecutionLogs);
//   - nós internos: sha256(left || right) sobre bytes brutos;
//   - nível com número ímpar de nós duplica o ÚLTIMO (Bitcoin-style).
//     Determinístico: mesma entrada sempre produz mesmo root.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ComputeMerkleRoot devolve o root em hex sobre as folhas body_hash fornecidas.
// Retorna erro se a lista estiver vazia ou se alguma folha não for hex válido —
// ambos são bugs upstream, não condições de runtime, e ancorar lixo silenciosa-
// mente derrubaria a garantia da chain toda.
func ComputeMerkleRoot(leaves []string) (string, error) {
	if len(leaves) == 0 {
		return "", fmt.Errorf("merkle: empty leaves")
	}
	level := make([][]byte, len(leaves))
	for i, h := range leaves {
		b, err := hex.DecodeString(h)
		if err != nil {
			return "", fmt.Errorf("merkle: leaf %d invalid hex: %w", i, err)
		}
		level[i] = b
	}
	for len(level) > 1 {
		if len(level)%2 == 1 {
			level = append(level, level[len(level)-1])
		}
		next := make([][]byte, 0, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			h := sha256.New()
			h.Write(level[i])
			h.Write(level[i+1])
			next = append(next, h.Sum(nil))
		}
		level = next
	}
	return hex.EncodeToString(level[0]), nil
}
