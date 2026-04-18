// criticality.go — derivação determinística de severidade para o log.
//
// Regras (refinamento obrigatório do plano):
//
//	CODE_CHANGE          → "low"
//	TEST_RUN + exit != 0 → "medium"  (TEST_FAIL)
//	CODE_MERGE           → "high"
//
// Demais casos retornam string vazia (ausente), coerente com fail-closed.
package main

// Criticality é a classificação de severidade derivada de (event, exitCode).
type Criticality string

const (
	CriticalityNone   Criticality = ""
	CriticalityLow    Criticality = "low"
	CriticalityMedium Criticality = "medium"
	CriticalityHigh   Criticality = "high"
)

// ComputeCriticality aplica as regras fixas do refinamento.
// Determinística e sem side-effects.
func ComputeCriticality(event EventType, exitCode int) Criticality {
	switch event {
	case EventCodeChange:
		return CriticalityLow
	case EventTestRun:
		if exitCode != 0 {
			return CriticalityMedium
		}
	case EventCodeMerge:
		return CriticalityHigh
	}
	return CriticalityNone
}
