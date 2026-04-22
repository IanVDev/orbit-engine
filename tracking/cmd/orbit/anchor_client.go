// anchor_client.go — cliente Ed25519 para AURYA /proofstream/submit.
//
// AURYA exige que o payload venha assinado com Ed25519 sobre a mensagem
// canônica `timestamp|nonce|payload`. Keypair efêmero por submissão é
// suficiente para o contrato atual: o nó só precisa de uma prova de que
// *alguém* com a pub_key enviou aquele payload naquele instante — quem é
// esse "alguém" é irrelevante para a semântica de anchor (rastreabilidade
// vem do body_hash do orbit, não da identidade do submissor).
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AuryaResponse espelha a resposta mínima consumida pelo orbit.
// Campos extras do servidor são ignorados via json.Unmarshal default.
type AuryaResponse struct {
	OK            bool   `json:"ok"`
	Hash          string `json:"hash"`
	NodeTimestamp string `json:"node_timestamp"`
	NodeSignature string `json:"node_signature"`
}

// submitToAurya envia {merkle_root, leaf_count} assinado para host/proofstream/submit.
// Fail-closed: HTTP != 200, ok=false, ou parse inválido retornam erro.
func submitToAurya(host, merkleRoot string, leafCount int) (*AuryaResponse, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("keygen: %w", err)
	}
	payload := map[string]interface{}{
		"merkle_root": merkleRoot,
		"leaf_count":  leafCount,
		"app":         "orbit-engine",
	}
	pb, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	nonce := fmt.Sprintf("orbit-%d", time.Now().UnixNano())
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	msg := append([]byte(ts+"|"+nonce+"|"), pb...)
	sig := ed25519.Sign(priv, msg)
	body, err := json.Marshal(map[string]interface{}{
		"app_id":    "orbit-engine",
		"app_pub":   hex.EncodeToString(pub),
		"signature": hex.EncodeToString(sig),
		"nonce":     nonce,
		"timestamp": ts,
		"payload":   payload,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	resp, err := http.Post(host+"/proofstream/submit", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(rb))
	}
	var out AuryaResponse
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if !out.OK {
		return nil, fmt.Errorf("AURYA rejected: %s", string(rb))
	}
	return &out, nil
}
