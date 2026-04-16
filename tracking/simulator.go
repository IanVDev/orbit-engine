// simulator.go — Simulador de produto real para orbit-engine.
//
// Representa o ciclo completo de uma sessão de usuário:
//
//	start → N interações → decisão (accepted|ignored) → end
//
// Métricas computadas por simulação:
//   - activation_rate   = activations / sessions
//   - value_rate        = value_sessions / sessions
//   - cost_per_value    = total_tokens / value_sessions  (0 se nenhum valor gerado)
//
// Integração com o layer de observabilidade:
//   - RecordEventValue       → orbit_user_perceived_value_total
//   - RecordUserReturned     → orbit_user_returned_total{fingerprint}
//
// Fail-closed:
//   - Sessions == 0 → RunSimulation retorna error imediatamente
//   - activation_rate == 0 após todas as sessões → retorna error
//
// Design: sem mocks, sem overengineering. Toda sessão é determinística dado
// uma semente (rand.Source), facilitando testes e reprodução de bugs.
package tracking

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"
)

// ---------------------------------------------------------------------------
// Tipos públicos
// ---------------------------------------------------------------------------

// SimConfig controla os parâmetros de uma simulação.
type SimConfig struct {
	// Sessions é o número de sessões a simular. Deve ser > 0.
	Sessions int

	// ActivationProbability é a chance (0.0–1.0) de um evento dentro de uma
	// sessão resultar em activação (ActionsApplied > 0).
	// Use -1 para aplicar o default (0.6).
	// Use 0 para simular zero activações (ativa fail-closed de activation_rate).
	ActivationProbability float64

	// InteractionsPerSession é o número de eventos por sessão.
	// Default 3 se zero.
	InteractionsPerSession int

	// BaseTokens é o impacto de token base por interação.
	// Default 500 se zero.
	BaseTokens int64

	// Seed permite resultados determinísticos. Use 0 para aleatoriedade real.
	Seed int64
}

// SimSession representa o resultado de uma única sessão simulada.
type SimSession struct {
	SessionID    string    `json:"session_id"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	Interactions int       `json:"interactions"`
	Activations  int       `json:"activations"` // eventos onde ActionsApplied > 0
	TotalTokens  int64     `json:"total_tokens"`
	ValueLevel   string    `json:"value_level"` // high | medium | low | none
	Returned     bool      `json:"returned"`    // usuário retornou (returned_total)
}

// SimResult agrega os resultados de todas as sessões da simulação.
type SimResult struct {
	Config          SimConfig    `json:"config"`
	Sessions        []SimSession `json:"sessions"`
	TotalSessions   int          `json:"total_sessions"`
	TotalActivation int          `json:"total_activations"`
	TotalTokens     int64        `json:"total_tokens"`

	// KPIs calculados após todas as sessões.
	ActivationRate float64 `json:"activation_rate"` // activations / sessions
	ValueRate      float64 `json:"value_rate"`      // sessions_with_value / total_sessions
	CostPerValue   float64 `json:"cost_per_value"`  // total_tokens / value_sessions (0 se nenhum valor)
}

// ---------------------------------------------------------------------------
// Log estruturado
// ---------------------------------------------------------------------------

type simLogEntry struct {
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`
	SessionID string `json:"session_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

func emitSimLog(event, sessionID, detail string) {
	entry := simLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Event:     event,
		SessionID: sessionID,
		Detail:    detail,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[SIM][WARN] marshal error: %v", err)
		return
	}
	log.Printf("[SIM] %s", line)
}

// ---------------------------------------------------------------------------
// RunSimulation
// ---------------------------------------------------------------------------

// RunSimulation executa cfg.Sessions sessões simuladas, registra métricas no
// layer de observabilidade de valor e retorna o resultado agregado.
//
// Fail-closed:
//   - cfg.Sessions == 0 → retorna error imediatamente (nada registrado).
//   - activation_rate == 0 após todas as sessões → retorna error (sinal de
//     que o simulador está mal configurado ou o ambiente está quebrado).
func RunSimulation(cfg SimConfig) (SimResult, error) {
	// Fail-closed: ao menos 1 sessão.
	if cfg.Sessions <= 0 {
		return SimResult{}, fmt.Errorf("simulator: sessions must be > 0, got %d", cfg.Sessions)
	}

	// Aplicar defaults.
	// ActivationProbability: usa -1 como sentinela de "não configurado" → default 0.6.
	// Valor 0 é explícito e válido; resulta em activation_rate=0 → fail-closed adiante.
	if cfg.ActivationProbability < 0 {
		cfg.ActivationProbability = 0.6
	}
	if cfg.InteractionsPerSession <= 0 {
		cfg.InteractionsPerSession = 3
	}
	if cfg.BaseTokens <= 0 {
		cfg.BaseTokens = 500
	}

	// RNG — determinístico se Seed != 0.
	var src rand.Source
	if cfg.Seed != 0 {
		src = rand.NewSource(cfg.Seed)
	} else {
		src = rand.NewSource(time.Now().UnixNano())
	}
	rng := rand.New(src) //nolint:gosec // não é criptográfico — simulação apenas

	result := SimResult{Config: cfg}
	valueSessions := 0

	emitSimLog("simulation_start", "", fmt.Sprintf("sessions=%d prob=%.2f", cfg.Sessions, cfg.ActivationProbability))

	for i := 0; i < cfg.Sessions; i++ {
		sess := simulateSession(i, cfg, rng)

		// Registrar métricas no layer de observabilidade existente.
		if sess.ValueLevel != "none" {
			event := SkillEvent{
				SessionID:            sess.SessionID,
				ActionsSuggested:     sess.Interactions,
				ActionsApplied:       sess.Activations,
				ImpactEstimatedToken: sess.TotalTokens,
				EstimatedWaste:       float64(sess.Activations) / float64(sess.Interactions+1),
				Mode:                 "auto",
			}
			RecordEventValue(event)
			valueSessions++
		}

		if sess.Returned {
			RecordUserReturned(sess.SessionID)
		}

		result.Sessions = append(result.Sessions, sess)
		result.TotalActivation += sess.Activations
		result.TotalTokens += sess.TotalTokens
	}

	result.TotalSessions = cfg.Sessions

	// Calcular KPIs.
	result.ActivationRate = float64(result.TotalActivation) / float64(result.TotalSessions)
	result.ValueRate = float64(valueSessions) / float64(result.TotalSessions)
	if valueSessions > 0 {
		result.CostPerValue = float64(result.TotalTokens) / float64(valueSessions)
	}

	emitSimLog("simulation_end", "", fmt.Sprintf(
		"activation_rate=%.3f value_rate=%.3f cost_per_value=%.1f",
		result.ActivationRate, result.ValueRate, result.CostPerValue,
	))

	// Fail-closed: activation_rate == 0 é sinal de problema.
	if result.ActivationRate == 0 {
		return result, fmt.Errorf(
			"simulator: activation_rate is 0 after %d sessions — check ActivationProbability (%.2f)",
			cfg.Sessions, cfg.ActivationProbability,
		)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// simulateSession — núcleo de uma única sessão
// ---------------------------------------------------------------------------

// simulateSession gera uma sessão simulada com N interações.
// Cada interação tem cfg.ActivationProbability de resultar em activação.
// O ValueLevel é determinado pelo ratio activations/interactions.
func simulateSession(index int, cfg SimConfig, rng *rand.Rand) SimSession {
	sessionID := fmt.Sprintf("sim-sess-%04d-%d", index, time.Now().UnixNano())
	start := time.Now().UTC()

	var activations int
	var totalTokens int64

	emitSimLog("session_start", sessionID, fmt.Sprintf("interactions=%d", cfg.InteractionsPerSession))

	for j := 0; j < cfg.InteractionsPerSession; j++ {
		tokens := cfg.BaseTokens + rng.Int63n(cfg.BaseTokens/2)
		totalTokens += tokens

		if rng.Float64() < cfg.ActivationProbability {
			activations++
			emitSimLog("interaction_activated", sessionID, fmt.Sprintf("tokens=%d", tokens))
		} else {
			emitSimLog("interaction_ignored", sessionID, fmt.Sprintf("tokens=%d", tokens))
		}
	}

	// Classificar valor da sessão.
	valueLevel := classifySessionValue(activations, cfg.InteractionsPerSession)

	// Usuário "retornou" se teve ao menos 1 activação (proxy de engajamento real).
	returned := activations > 0

	end := time.Now().UTC()

	emitSimLog("session_end", sessionID, fmt.Sprintf(
		"activations=%d/%d value=%s returned=%v tokens=%d",
		activations, cfg.InteractionsPerSession, valueLevel, returned, totalTokens,
	))

	return SimSession{
		SessionID:    sessionID,
		StartedAt:    start,
		EndedAt:      end,
		Interactions: cfg.InteractionsPerSession,
		Activations:  activations,
		TotalTokens:  totalTokens,
		ValueLevel:   valueLevel,
		Returned:     returned,
	}
}

// classifySessionValue mapeia o ratio activations/interactions para um nível
// de valor, espelhando a lógica de ClassifyEventValue.
//
//	high:   activations >= interactions (todas activadas)
//	medium: activations > 0             (parcial)
//	low:    activations == 0            (nenhuma) → "none" para sinalizar skip
func classifySessionValue(activations, interactions int) string {
	if interactions <= 0 {
		return "none"
	}
	switch {
	case activations >= interactions:
		return string(ValueHigh)
	case activations > 0:
		return string(ValueMedium)
	default:
		return "none" // nenhuma activação → sem valor registrável
	}
}
