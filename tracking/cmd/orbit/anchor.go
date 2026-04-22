// anchor.go — comando `orbit anchor`: publica merkle_root em AURYA.
//
// Extrai body_hash de todos os logs com body_hash preenchido (ordem estável
// por timestamp), computa merkle_root, submete à AURYA via ProofStream, e
// persiste o receipt (root + assinatura do nó + leaf_hashes) em
// $ORBIT_HOME/anchors/. O verify posterior recomputa o root a partir dos
// logs locais e exige match — se um log foi apagado, a recomputação diverge.
//
// I20 ANCHOR_VERIFICATION: receipt carrega self-signature Ed25519 (AppPub +
// AppSignature) sobre o corpo canônico. Verify valida assinatura + full match
// + monotonic timestamp. Keypair efêmero é descartado após assinar — nenhuma
// chave privada persistida, tamper-evident do receipt inteiro.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// AnchorReceipt é o registro persistido de uma ancoragem bem-sucedida.
// LeafHashes é incluído para que verify possa recomputar exatamente o mesmo
// root sem ambiguidade sobre "quais logs estavam presentes na ancoragem".
type AnchorReceipt struct {
	MerkleRoot    string   `json:"merkle_root"`
	LeafCount     int      `json:"leaf_count"`
	LeafHashes    []string `json:"leaf_hashes"`
	Host          string   `json:"host"`
	NodeTimestamp string   `json:"node_timestamp"`
	NodeSignature string   `json:"node_signature"`
	NodeHash      string   `json:"node_hash"`
	CreatedAt     string   `json:"created_at"`
	// I20 self-signature: Ed25519 sobre canonical(receipt sem app_signature).
	// Keypair efêmero gerado em runAnchor; privKey descartado após assinar.
	AppPub       string `json:"app_pub"`
	AppSignature string `json:"app_signature"`
}

// signAnchorReceipt gera keypair Ed25519 efêmero, assina o corpo canônico
// (receipt com AppSignature zerado) e preenche AppPub + AppSignature.
// Privada é descartada ao retornar.
func signAnchorReceipt(r *AnchorReceipt) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("anchor sign: keygen: %w", err)
	}
	r.AppPub = hex.EncodeToString(pub)
	r.AppSignature = ""
	msg, err := canonicalAnchorMsg(*r)
	if err != nil {
		return fmt.Errorf("anchor sign: canonical: %w", err)
	}
	r.AppSignature = hex.EncodeToString(ed25519.Sign(priv, msg))
	return nil
}

// canonicalAnchorMsg produz a mensagem canônica para assinar/verificar.
// Corpo = receipt com AppSignature vazio, serializado via JSON estável.
func canonicalAnchorMsg(r AnchorReceipt) ([]byte, error) {
	r.AppSignature = ""
	return json.Marshal(r)
}

// runAnchor é o entrypoint do subcomando. Falha fail-closed se não há logs
// com body_hash, se merkle falhar, ou se AURYA recusar.
func runAnchor(w io.Writer, host string) error {
	leaves, err := collectLeafHashes()
	if err != nil {
		return err
	}
	if len(leaves) == 0 {
		return fmt.Errorf("anchor: nenhum log com body_hash para ancorar")
	}
	root, err := ComputeMerkleRoot(leaves)
	if err != nil {
		return fmt.Errorf("anchor: merkle: %w", err)
	}
	resp, err := submitToAurya(host, root, len(leaves))
	if err != nil {
		return fmt.Errorf("anchor: AURYA submit: %w", err)
	}
	receipt := AnchorReceipt{
		MerkleRoot: root, LeafCount: len(leaves), LeafHashes: leaves,
		Host:          host,
		NodeTimestamp: resp.NodeTimestamp,
		NodeSignature: resp.NodeSignature,
		NodeHash:      resp.Hash,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := signAnchorReceipt(&receipt); err != nil {
		return fmt.Errorf("anchor: sign: %w", err)
	}
	path, err := writeAnchorReceipt(receipt)
	if err != nil {
		return fmt.Errorf("anchor: persistência: %w", err)
	}
	fmt.Fprintf(w, "✅  anchor criado — %d folhas → root %s...\n", len(leaves), safePrefix(root, 16))
	fmt.Fprintf(w, "    AURYA node_ts: %s\n    receipt:       %s\n", resp.NodeTimestamp, path)
	return nil
}

// writeAnchorReceipt grava o receipt em $ORBIT_HOME/anchors/anchor_<ts>.json.
// Nome ordenável lexicograficamente para loadLatestAnchor pegar o mais recente.
func writeAnchorReceipt(r AnchorReceipt) (string, error) {
	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "anchors")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "anchor_"+r.CreatedAt+".json")
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}
