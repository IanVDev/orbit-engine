// integrity.go — body_hash canônico do RunResult.
//
// Fecha o gap G3: o proof legado cobre apenas session_id|timestamp|output_bytes,
// então editar output/decision/diagnosis no arquivo NÃO quebrava orbit verify.
// body_hash cobre o JSON inteiro e detecta adulteração de qualquer campo.
//
// Fórmula pareável entre Go e Python:
//   body_hash = sha256( canonical_json(log, excluindo "body_hash") )
// canonical_json = keys ordenadas, compact, sem HTML-escape, UTF-8 nativo.
// Python replica com json.dumps(sort_keys=True, separators=(",",":"),
// ensure_ascii=False). Divergência aqui quebra todo o scan do observatório.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// CanonicalHash computa body_hash a partir do RunResult. O próprio campo
// BodyHash é zerado antes do hash — um campo nunca entra na sua assinatura.
// Round-trip via map garante ordenação alfabética determinística; UseNumber
// preserva inteiros sem passar por float64 (evita perda de precisão).
func CanonicalHash(r RunResult) (string, error) {
	r.BodyHash = ""
	raw, err := marshalNoEscapeHTML(r)
	if err != nil {
		return "", fmt.Errorf("integrity: marshal struct: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]interface{}
	if err := dec.Decode(&m); err != nil {
		return "", fmt.Errorf("integrity: decode map: %w", err)
	}
	delete(m, "body_hash")
	canon, err := marshalNoEscapeHTML(m)
	if err != nil {
		return "", fmt.Errorf("integrity: marshal map: %w", err)
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// marshalNoEscapeHTML emite JSON compacto sem escapar <, >, & — o default
// do encoding/json escaparia para < etc. e o Python não reproduz isso
// sem configuração extra. Desligar aqui é mais simples que forçar escape lá.
func marshalNoEscapeHTML(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
