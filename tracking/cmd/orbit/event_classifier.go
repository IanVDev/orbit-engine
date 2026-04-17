// event_classifier.go — classifica comandos executados via `orbit run`
// em eventos conhecidos do Orbit Decision Engine (MVP).
//
// Escopo atual (mínimo):
//   - git commit     → CODE_CHANGE
//   - go test        → TEST_RUN
//
// Comandos não reconhecidos retornam EventUnknown e não disparam decisões.
// Fail-closed: cmdName vazio também retorna EventUnknown.
package main

// EventType representa a categoria semântica de um comando executado.
type EventType string

const (
	EventUnknown    EventType = "UNKNOWN"
	EventCodeChange EventType = "CODE_CHANGE"
	EventTestRun    EventType = "TEST_RUN"
)

// ClassifyCommand inspeciona (cmdName, args) e retorna o EventType
// correspondente. Determinístico e sem side-effects — pode ser chamado
// antes, durante ou depois da execução do comando.
func ClassifyCommand(cmdName string, args []string) EventType {
	if cmdName == "" {
		return EventUnknown
	}

	switch cmdName {
	case "git":
		if len(args) > 0 && args[0] == "commit" {
			return EventCodeChange
		}
	case "go":
		if len(args) > 0 && args[0] == "test" {
			return EventTestRun
		}
	}
	return EventUnknown
}
