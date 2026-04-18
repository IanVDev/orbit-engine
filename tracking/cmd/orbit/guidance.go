// guidance.go — mensagens acionáveis derivadas do output de execução.
//
// Para TEST_FAIL (event=TEST_RUN com exit != 0): extrai a primeira
// ocorrência de <file>:<line> do output e devolve exatamente isso.
// Sem mensagens genéricas. Sem sugestões inventadas.
//
// Fail-closed:
//   - Se nenhum match é encontrado → guidance vazio.
//   - Qualquer erro de regex/parsing → guidance vazio.
package main

import "regexp"

// fileLineRe casa tokens do tipo "path/file.ext:123" (extensão obrigatória
// para evitar falsos positivos em URLs ou timestamps com ':'). O path pode
// conter '/', '\', '-', '_', '.', dígitos, letras.
var fileLineRe = regexp.MustCompile(`([A-Za-z0-9_./\\\-]+\.[A-Za-z0-9]+):(\d+)`)

// BuildGuidance devolve o texto de guidance a ser exibido ao usuário e
// gravado no log. Ver regras no topo do arquivo.
func BuildGuidance(event EventType, exitCode int, output string) string {
	// TEST_FAIL é a única categoria com guidance "apontar local".
	if event == EventTestRun && exitCode != 0 {
		return extractFirstFileLine(output)
	}
	return ""
}

// extractFirstFileLine retorna a primeira correspondência de file:line
// encontrada no texto, no formato exato "<file>:<line>". Sem match → "".
func extractFirstFileLine(text string) string {
	if text == "" {
		return ""
	}
	m := fileLineRe.FindStringSubmatch(text)
	if len(m) < 3 {
		return ""
	}
	return m[1] + ":" + m[2]
}
