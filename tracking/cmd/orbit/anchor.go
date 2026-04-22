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
// + monotonic timestamp.
//
// I21 TRUSTED_ANCHOR_SIGNER: receipt SÓ é aceito se AppPub == trustedAuryaPubKey.
// Signer key é resolvida em resolveSignerKey() a partir de ORBIT_SIGNER_PRIVKEY
// (hex, 128 chars) com fallback para devSignerPrivKeyHex (dev default, bootstrap).
// Produção: exportar ORBIT_SIGNER_PRIVKEY com chave controlada; rotação via
// regeneração + atualização de trustedAuryaPubKey (I22 futuro).
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// trustedAuryaPubKey é a única public key aceita como signer autorizado do
// receipt (I21). Qualquer receipt com AppPub diferente → verify FAIL CLOSED.
// Hardcoded no bootstrap; pareada com devSignerPrivKeyHex. Em prod séria, o
// valor é rotacionado via novo release do binário (cadeia de trust do release
// já coberta por I2 + sha256 do binário no install_remote).
const trustedAuryaPubKey = "c2860595b5d5b89e376b6af4023af82042b6bc018b5c997346bdd3c01c1cdfca"

// devSignerPrivKeyHex é o keypair dev-default pareado com trustedAuryaPubKey.
// Usado quando ORBIT_SIGNER_PRIVKEY não está definida — permite bootstrap
// sem configuração. Em prod, exportar ORBIT_SIGNER_PRIVKEY=<hex> com chave
// controlada. Aviso: dev-default é público (visível no código), não oferece
// autenticação em ambiente hostil — é tamper-resistance inicial.
const devSignerPrivKeyHex = "52301ccea79f2f3ea798210589eeb7d8697daa0c89f598e66f286a0549ae322ac2860595b5d5b89e376b6af4023af82042b6bc018b5c997346bdd3c01c1cdfca"

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

// resolveSignerKey devolve priv + pub do signer. Ordem:
//  1. ORBIT_SIGNER_PRIVKEY (hex, 128 chars) — prod/customizado.
//  2. devSignerPrivKeyHex — dev default (bootstrap).
// Fail-closed: env inválido → erro (NÃO faz fallback silencioso pro default).
func resolveSignerKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	src := os.Getenv("ORBIT_SIGNER_PRIVKEY")
	explicit := src != ""
	if !explicit {
		src = devSignerPrivKeyHex
	}
	raw, err := hex.DecodeString(src)
	if err != nil {
		return nil, nil, fmt.Errorf("anchor sign: ORBIT_SIGNER_PRIVKEY hex inválido: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("anchor sign: signer privkey tamanho %d, esperado %d",
			len(raw), ed25519.PrivateKeySize)
	}
	priv := ed25519.PrivateKey(raw)
	pub := priv.Public().(ed25519.PublicKey)
	return priv, pub, nil
}

// signAnchorReceipt assina o corpo canônico (receipt com AppSignature zerado)
// com a chave do signer trusted (I21) e preenche AppPub + AppSignature.
// Fail-closed em keygen/priv inválida.
func signAnchorReceipt(r *AnchorReceipt) error {
	priv, pub, err := resolveSignerKey()
	if err != nil {
		return err
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
