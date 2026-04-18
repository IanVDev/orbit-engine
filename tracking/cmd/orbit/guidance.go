// guidance.go — texto curto apontando o local provável da falha.
//
// Para TEST_FAIL (event=TEST_RUN com exit != 0): extrai a PRIMEIRA
// ocorrência de <file>:<line> do output e devolve exatamente isso.
// Sem mensagens genéricas. Sem sugestões inventadas.
//
// A regex compartilhada (fileLineLooseRe) mora em diagnose.go — este
// arquivo é apenas a superfície pública usada pelo `run.go`. Qualquer
// evolução de detecção (novos parsers, novos eventos) vai para diagnose.go
// para manter um único domicílio de contrato.
//
// Fail-closed:
//   - evento != TEST_RUN ou exit == 0     → ""
//   - nenhum match                        → ""
//   - linha não-numérica                  → ""
package main

import "strconv"

// BuildGuidance devolve o texto de guidance a ser exibido ao usuário e
// gravado no log (campo RunResult.Guidance).
func BuildGuidance(event EventType, exitCode int, output string) string {
	if event != EventTestRun || exitCode == 0 {
		return ""
	}
	file, line, ok := firstFileLine(output)
	if !ok {
		return ""
	}
	return file + ":" + strconv.Itoa(line)
}
