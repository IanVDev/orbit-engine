// threat_model_doc_test.go — anti-drift gate para THREAT_MODEL.md.
//
// Objetivo (fail-closed): manter o THREAT_MODEL.md alinhado com o código
// real. Hoje, a autenticação HMAC em /track existe (security.go:ValidateHMAC,
// realusage.go:151, security_init_test.go). Se alguém reintroduzir texto
// descrevendo o gap como aberto — frases como "Sem autenticação no /track"
// ou "P3 backlog" associadas a bearer/mTLS — a doc volta a divergir do
// código e o teste reprova o CI.
//
// Este teste não valida comportamento de runtime: apenas evita que a
// documentação regrida para um estado que já não corresponde à realidade.
package tracking

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenPhrases lista trechos que indicariam drift para o estado
// pré-mitigação. Cada entrada carrega o motivo pelo qual é proibido.
var forbiddenPhrases = []struct {
	phrase string
	reason string
}{
	{
		phrase: "Sem autenticação no /track",
		reason: "HMAC auth está implementada (security.go:ValidateHMAC + realusage.go:151)",
	},
	{
		phrase: "P3 backlog: bearer token ou mTLS",
		reason: "remediação P3 obsoleta — HMAC já cobre o vetor",
	},
	{
		phrase: "Sem auth no /track",
		reason: "linha da matriz de gaps sobre /track auth deve permanecer removida",
	},
}

// requiredPhrases garante que as referências ao código de mitigação
// continuam na doc — se alguém as remover, o teste falha e força revisão.
var requiredPhrases = []string{
	"security.go:ValidateHMAC",
	"realusage.go:151",
	"security_init_test.go",
}

func TestThreatModelDoc_NoDriftOnTrackAuth(t *testing.T) {
	path := filepath.Join("..", "THREAT_MODEL.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("não foi possível ler %s: %v", path, err)
	}
	doc := string(raw)

	for _, f := range forbiddenPhrases {
		if strings.Contains(doc, f.phrase) {
			t.Errorf("THREAT_MODEL.md contém frase proibida %q — motivo: %s",
				f.phrase, f.reason)
		}
	}

	for _, r := range requiredPhrases {
		if !strings.Contains(doc, r) {
			t.Errorf("THREAT_MODEL.md deveria referenciar %q para evidenciar a mitigação HMAC",
				r)
		}
	}
}
