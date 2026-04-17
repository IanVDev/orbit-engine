// decision_engine.go — mapeia (EventType, exit_code) em uma Decision
// que o orbit run pode executar como próximo passo.
//
// Regras do MVP:
//
//	TEST_RUN    com exit != 0  → trigger_analyze
//	CODE_CHANGE qualquer exit  → trigger_snapshot
//
// Eventos desconhecidos ou casos não mapeados retornam ActionNone,
// de modo que o comportamento original do `orbit run` seja preservado.
//
// Fail-closed: contexto inválido (event vazio) produz ActionNone com
// Reason explícito. O caller pode logar sem disparar efeitos colaterais.
package main

import "fmt"

// ActionType é o próximo passo sugerido pelo decision engine.
type ActionType string

const (
	ActionNone            ActionType = "NONE"
	ActionTriggerAnalyze  ActionType = "TRIGGER_ANALYZE"
	ActionTriggerSnapshot ActionType = "TRIGGER_SNAPSHOT"
)

// Decision é o resultado da avaliação do decision engine.
// Reason é sempre preenchido para permitir logs auditáveis.
type Decision struct {
	Event  EventType
	Action ActionType
	Reason string
}

// Decide aplica as regras do MVP sobre (event, exitCode) e retorna
// uma Decision. Nunca retorna erro — um contexto inválido vira
// ActionNone com Reason explicativo (fail-closed).
func Decide(event EventType, exitCode int) Decision {
	if event == "" {
		return Decision{
			Event:  EventUnknown,
			Action: ActionNone,
			Reason: "evento vazio — contexto inválido, nenhuma ação",
		}
	}

	switch event {
	case EventTestRun:
		if exitCode != 0 {
			return Decision{
				Event:  event,
				Action: ActionTriggerAnalyze,
				Reason: fmt.Sprintf("TEST_RUN falhou (exit=%d) — analisar falhas", exitCode),
			}
		}
		return Decision{
			Event:  event,
			Action: ActionNone,
			Reason: "TEST_RUN passou — nenhuma ação",
		}

	case EventCodeChange:
		return Decision{
			Event:  event,
			Action: ActionTriggerSnapshot,
			Reason: "CODE_CHANGE detectado — snapshot do estado",
		}
	}

	return Decision{
		Event:  event,
		Action: ActionNone,
		Reason: fmt.Sprintf("evento %q não mapeado", event),
	}
}
