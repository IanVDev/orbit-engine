// redact.go — sanitização de secrets comuns em output capturado antes
// de persistir em disco. Regex simples, substituição por "[REDACTED]".
//
// Cobertura mínima (stdout/stderr do comando executado):
//   - Bearer <token>
//   - password = <valor> | password: <valor>
//   - token    = <valor> | token: <valor>
//   - api-key / api_key  = <valor>
//
// Fora do escopo: detecção de chaves RSA/PEM, JWT standalone, heurísticas
// de entropia. Se aparecer demanda, extende-se em redact_extra.go — aqui
// mantém-se o mínimo necessário para não vazar credenciais em logs.
package main

import "regexp"

var redactors = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)(Bearer)\s+[A-Za-z0-9._\-+/=]+`), `$1 [REDACTED]`},
	{regexp.MustCompile(`(?i)(x-authorization\s*:)\s*[^\s"']+`), `$1 [REDACTED]`},
	{regexp.MustCompile(`(?i)(password|token|api[_-]?key)(\s*[:=]\s*)([^\s&"'<>,]+)`), `$1$2[REDACTED]`},
}

// redactOutput substitui padrões conhecidos de secrets por "[REDACTED]".
// Preserva prefixo (chave e separador) para manter o output legível e
// apenas modifica o valor sensível.
func redactOutput(s string) string {
	for _, r := range redactors {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
