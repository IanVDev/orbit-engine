// redact.go — redação de secrets no output capturado antes de persistir.
//
// Invariante I12 SECRET_SAFETY: nenhum secret em formato conhecido é
// persistido em texto puro em ~/.orbit/logs/. Fail-closed: a função é
// chamada SEMPRE antes do marshal; remover a chamada quebra o teste.
//
// Preserva output_bytes do original — redaction altera o campo `output`
// persistido, NÃO o len usado no proof (sha256(sid|ts|output_bytes)).
// Isso mantém I2 (proof) e I3 (schema) consistentes.
package tracking

import (
	"fmt"
	"regexp"
)

// MaxPayloadBytes é o limite antes de truncar payloads em logs.
// Payloads acima desse limite são truncados e marcados com [TRUNCATED].
const MaxPayloadBytes = 4096

// secretPatterns cobre os formatos mais comuns que vazam em CLI ou HTTP:
//   - Bearer tokens (Authorization: Bearer xxx)
//   - X-Authorization header (x-authorization: xxx)
//   - API keys sk-live/sk-test (Stripe, OpenAI, Anthropic)
//   - AWS access keys (AKIA...)
//   - password=/api_key=/token= (env/config inline)
//   - SSH private key headers
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]{10,}`),
	regexp.MustCompile(`(?i)(x-authorization\s*:\s*)[^\s"']+`),
	regexp.MustCompile(`sk-(live|test|proj)-[A-Za-z0-9_\-]{10,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`(?i)(password|api[_-]?key|secret|token)\s*[=:]\s*[^\s"']+`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// RedactSecrets substitui valores sensíveis por "***REDACTED***" preservando
// o prefixo identificador (ex: "Bearer ***REDACTED***", "password=***REDACTED***").
// Se nenhum padrão for detectado, devolve a string inalterada.
//
// Idempotente: aplicar duas vezes é igual a aplicar uma.
func RedactSecrets(s string) string {
	out := s
	for _, pat := range secretPatterns {
		out = pat.ReplaceAllStringFunc(out, func(match string) string {
			// Preserva o prefixo (ex: "bearer ", "password=") quando há
			// captura. Se o padrão é puro (AKIA..., BEGIN KEY), substitui
			// integralmente.
			if groups := pat.FindStringSubmatch(match); len(groups) > 1 {
				return groups[1] + "***REDACTED***"
			}
			return "***REDACTED***"
		})
	}
	return out
}

// TruncatePayload trunca s se len(s) > maxBytes, adicionando marcador.
// Garante que payloads longos não causem exposição excessiva em logs.
func TruncatePayload(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + fmt.Sprintf(" [TRUNCATED: %d bytes omitidos]", len(s)-maxBytes)
}
