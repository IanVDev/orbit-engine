// event_classifier.go — classifica comandos executados via `orbit run`
// em eventos conhecidos do Orbit Decision Engine.
//
// Escopo:
//   - git commit         → CODE_CHANGE
//   - git merge / rebase → CODE_MERGE
//   - go test / pytest / npm test / cargo test / jest → TEST_RUN
//
// Comandos não reconhecidos retornam EventUnknown e não disparam decisões.
// Fail-closed: cmdName vazio também retorna EventUnknown.
package main

// EventType representa a categoria semântica de um comando executado.
type EventType string

const (
	EventUnknown    EventType = "UNKNOWN"
	EventCodeChange EventType = "CODE_CHANGE"
	EventCodeMerge  EventType = "CODE_MERGE"
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
		if len(args) > 0 {
			switch args[0] {
			case "commit":
				return EventCodeChange
			case "merge", "rebase":
				return EventCodeMerge
			}
		}
	case "go":
		if len(args) > 0 && args[0] == "test" {
			return EventTestRun
		}
	case "pytest":
		return EventTestRun
	case "cargo":
		if len(args) > 0 && args[0] == "test" {
			return EventTestRun
		}
	case "npm", "yarn", "pnpm":
		if len(args) > 0 {
			a0 := args[0]
			if a0 == "test" || a0 == "t" {
				return EventTestRun
			}
			if a0 == "run" && len(args) > 1 && args[1] == "test" {
				return EventTestRun
			}
		}
	case "jest", "vitest":
		return EventTestRun
	}
	return EventUnknown
}
