// anchor_check.go — comparação fail-closed entre logs atuais e receipt.
//
// I20 ANCHOR_VERIFICATION: valida em ordem fail-closed:
//   1. Assinatura Ed25519 do receipt (app_signature sobre canonical).
//   2. Monotonic: NodeTimestamp > último visto em .anchor-last-ts (anti-replay).
//   3. leaf_count consistente (LeafCount == len(LeafHashes) == len(leaves)).
//   4. Full match: leaves_atuais == LeafHashes elemento-por-elemento.
//   5. merkle_root recomputado == rec.MerkleRoot.
//
// Semântica "full match" (não prefixo): após `orbit anchor`, qualquer log
// adicional exige re-anchor antes do próximo `verify --chain`. Operação
// explícita para preservar imutabilidade do corpo ancorado.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/IanVDev/orbit-engine/tracking"
)

// verifyAgainstLatestAnchor é chamado por runVerifyChain após a chain OK.
// Sem receipt → no-op. Com receipt → 5 checks fail-closed (I20).
func verifyAgainstLatestAnchor(w io.Writer) error {
	rec, path, err := loadLatestAnchor()
	if err != nil {
		return fmt.Errorf("anchor verify: load: %w", err)
	}
	if rec == nil {
		return nil
	}

	// [1] Assinatura Ed25519 do receipt.
	if err := verifyAnchorSignature(rec); err != nil {
		return fmt.Errorf("anchor mismatch — assinatura inválida: %w", err)
	}

	// [2] Monotonic timestamp (anti-replay).
	if err := verifyAnchorMonotonic(rec); err != nil {
		return fmt.Errorf("anchor mismatch — replay detectado: %w", err)
	}

	// [3] leaf_count consistente (struct-interno).
	if rec.LeafCount != len(rec.LeafHashes) {
		return fmt.Errorf("anchor mismatch — leaf_count=%d != len(leaf_hashes)=%d",
			rec.LeafCount, len(rec.LeafHashes))
	}

	leaves, err := collectLeafHashes()
	if err != nil {
		return fmt.Errorf("anchor verify: %w", err)
	}

	// [4] Full match (não prefixo): len E cada leaf.
	if len(leaves) != rec.LeafCount {
		return fmt.Errorf(
			"anchor mismatch — %d folhas atuais != %d ancoradas (full match exige igualdade)",
			len(leaves), rec.LeafCount)
	}
	for i, h := range rec.LeafHashes {
		if leaves[i] != h {
			return fmt.Errorf(
				"anchor mismatch — folha %d divergente (log adulterado ou reordenado)", i)
		}
	}

	// [5] Merkle root recomputado.
	root, err := ComputeMerkleRoot(leaves)
	if err != nil {
		return fmt.Errorf("anchor verify: merkle: %w", err)
	}
	if root != rec.MerkleRoot {
		return fmt.Errorf(
			"anchor mismatch — merkle_root recomputado %s... != receipt %s...",
			safePrefix(root, 16), safePrefix(rec.MerkleRoot, 16))
	}

	// Atualiza .anchor-last-ts após aceitação (commit do monotonic).
	if err := persistAnchorTimestamp(rec.NodeTimestamp); err != nil {
		return fmt.Errorf("anchor verify: persist ts: %w", err)
	}

	// I21 observabilidade: signer fingerprint + trusted flag no output.
	// trusted==true é garantido aqui (verifyAnchorSignature já validou); o
	// log torna a decisão audit-friendly.
	trusted := rec.AppPub == trustedAuryaPubKey
	fmt.Fprintf(w, "✅  anchor ok — merkle_root %s... confere (receipt %s, AURYA %s)\n",
		safePrefix(root, 16), filepath.Base(path), rec.NodeTimestamp)
	fmt.Fprintf(w, "    signer=%s... trusted=%v\n", safePrefix(rec.AppPub, 16), trusted)
	return nil
}

// verifyAnchorSignature valida app_signature contra canonical(receipt sem sig)
// usando app_pub. Falha se: hex inválido, pub/sig com tamanho errado, ou
// verify ed25519 retornar false.
//
// I21 TRUSTED_ANCHOR_SIGNER: antes do crypto verify, exige que rec.AppPub
// bata EXATAMENTE com trustedAuryaPubKey (hardcoded). Receipt assinado com
// qualquer outra key → FAIL CLOSED com erro CRITICAL.
func verifyAnchorSignature(rec *AnchorReceipt) error {
	if rec.AppPub == "" || rec.AppSignature == "" {
		return fmt.Errorf("receipt sem app_pub/app_signature (legado ou adulterado)")
	}
	// I21 — trusted signer check ANTES de qualquer crypto.
	if rec.AppPub != trustedAuryaPubKey {
		return fmt.Errorf(
			"CRITICAL: receipt não assinado por trusted signer (signer=%s... trusted=%s...)",
			safePrefix(rec.AppPub, 16), safePrefix(trustedAuryaPubKey, 16))
	}
	pub, err := hex.DecodeString(rec.AppPub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("app_pub inválido")
	}
	sig, err := hex.DecodeString(rec.AppSignature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("app_signature inválido")
	}
	msg, err := canonicalAnchorMsg(*rec)
	if err != nil {
		return fmt.Errorf("canonical: %w", err)
	}
	if !ed25519.Verify(pub, msg, sig) {
		return fmt.Errorf("assinatura não confere sobre corpo canônico")
	}
	return nil
}

// anchorLastTsPath devolve $ORBIT_HOME/.anchor-last-ts.
// Arquivo de 1 linha: RFC3339Nano do NodeTimestamp do último receipt aceito.
func anchorLastTsPath() (string, error) {
	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ".anchor-last-ts"), nil
}

// verifyAnchorMonotonic exige rec.NodeTimestamp > último aceito. Primeiro
// uso (arquivo ausente) aceita. Empate ou retrocesso → replay.
func verifyAnchorMonotonic(rec *AnchorReceipt) error {
	p, err := anchorLastTsPath()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil // primeiro uso
	}
	if err != nil {
		return fmt.Errorf("read last-ts: %w", err)
	}
	prev := string(b)
	if rec.NodeTimestamp <= prev {
		return fmt.Errorf("NodeTimestamp=%s <= último aceito=%s", rec.NodeTimestamp, prev)
	}
	return nil
}

// persistAnchorTimestamp grava ts em .anchor-last-ts (0600).
func persistAnchorTimestamp(ts string) error {
	p, err := anchorLastTsPath()
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(ts), 0o600)
}
