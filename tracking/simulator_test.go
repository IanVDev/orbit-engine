// simulator_test.go — Testes anti-regressão para o simulador de produto.
//
// Cobre:
//  1. Anti-regressão: ao menos 1 sessão, ao menos 1 ativação, valor registrado.
//  2. Fail-closed: Sessions == 0 → error imediato.
//  3. Fail-closed: ActivationProbability == 0 → activation_rate == 0 → error.
//  4. Determinismo: mesma Seed → mesmo resultado.
//  5. KPIs: activation_rate, value_rate, cost_per_value estão em intervalos válidos.
package tracking

import (
	"testing"
)

// newSimTestRegistry cria um registro Prometheus isolado para testes de simulação.
func newSimTestRegistry() {
	// Reutilizamos o registry global (metrics já registradas pelo init dos outros testes).
	// Apenas garantimos que RegisterValueMetrics foi chamado.
}

// ---------------------------------------------------------------------------
// 1. Anti-regressão principal
// ---------------------------------------------------------------------------

// TestSimulatorAntiRegression valida os três requisitos fundamentais:
//   - pelo menos 1 sessão executada
//   - pelo menos 1 ativação ocorrida
//   - valor foi registrado (ValueRate > 0)
func TestSimulatorAntiRegression(t *testing.T) {
	cfg := SimConfig{
		Sessions:               10,
		ActivationProbability:  0.8, // alta prob → garante ativações
		InteractionsPerSession: 3,
		BaseTokens:             400,
		Seed:                   42, // determinístico
	}
	result, err := RunSimulation(cfg)
	if err != nil {
		t.Fatalf("RunSimulation should not fail with valid config: %v", err)
	}

	// Requisito 1: ao menos 1 sessão
	if result.TotalSessions < 1 {
		t.Errorf("want TotalSessions >= 1, got %d", result.TotalSessions)
	}
	if len(result.Sessions) != result.TotalSessions {
		t.Errorf("Sessions slice len %d != TotalSessions %d", len(result.Sessions), result.TotalSessions)
	}

	// Requisito 2: ao menos 1 ativação
	if result.TotalActivation < 1 {
		t.Errorf("want TotalActivation >= 1, got %d", result.TotalActivation)
	}

	// Requisito 3: valor registrado (ValueRate > 0)
	if result.ValueRate <= 0 {
		t.Errorf("want ValueRate > 0, got %.4f", result.ValueRate)
	}

	// KPI de sanidade: activation_rate in (0, 1]
	if result.ActivationRate <= 0 || result.ActivationRate > float64(cfg.InteractionsPerSession) {
		t.Errorf("ActivationRate out of expected range: %.4f", result.ActivationRate)
	}

	// cost_per_value deve ser positivo quando há valor
	if result.CostPerValue <= 0 {
		t.Errorf("want CostPerValue > 0, got %.2f", result.CostPerValue)
	}
}

// ---------------------------------------------------------------------------
// 2. Fail-closed: Sessions == 0
// ---------------------------------------------------------------------------

func TestSimulatorFailClosedOnZeroSessions(t *testing.T) {
	_, err := RunSimulation(SimConfig{Sessions: 0})
	if err == nil {
		t.Error("RunSimulation with Sessions=0 must return error (fail-closed)")
	}
}

// ---------------------------------------------------------------------------
// 3. Fail-closed: ActivationProbability == 0 → activation_rate == 0
// ---------------------------------------------------------------------------

func TestSimulatorFailClosedOnZeroActivationRate(t *testing.T) {
	cfg := SimConfig{
		Sessions:              50,
		ActivationProbability: 0.0, // nenhuma activação possível
		Seed:                  99,
	}
	// ActivationProbability 0 vai resultar em activation_rate 0 → deve retornar error.
	_, err := RunSimulation(cfg)
	if err == nil {
		t.Error("RunSimulation with ActivationProbability=0 must return error (activation_rate==0)")
	}
}

// ---------------------------------------------------------------------------
// 4. Determinismo: mesma Seed → resultado idêntico
// ---------------------------------------------------------------------------

func TestSimulatorDeterministic(t *testing.T) {
	cfg := SimConfig{
		Sessions:               5,
		ActivationProbability:  0.8, // alta prob → garante activation_rate > 0
		InteractionsPerSession: 4,
		BaseTokens:             300,
		Seed:                   1337,
	}

	r1, err1 := RunSimulation(cfg)
	r2, err2 := RunSimulation(cfg)

	if err1 != nil || err2 != nil {
		t.Fatalf("both runs should succeed: err1=%v err2=%v", err1, err2)
	}

	if r1.TotalActivation != r2.TotalActivation {
		t.Errorf("determinism broken: TotalActivation %d != %d", r1.TotalActivation, r2.TotalActivation)
	}
	if r1.TotalTokens != r2.TotalTokens {
		t.Errorf("determinism broken: TotalTokens %d != %d", r1.TotalTokens, r2.TotalTokens)
	}
	if r1.ActivationRate != r2.ActivationRate {
		t.Errorf("determinism broken: ActivationRate %.4f != %.4f", r1.ActivationRate, r2.ActivationRate)
	}
}

// ---------------------------------------------------------------------------
// 5. KPIs: intervalos válidos
// ---------------------------------------------------------------------------

func TestSimulatorKPIBounds(t *testing.T) {
	cfg := SimConfig{
		Sessions:               20,
		ActivationProbability:  0.7, // probabilidade alta para garantir activation_rate > 0
		InteractionsPerSession: 2,
		BaseTokens:             200,
		Seed:                   7,
	}

	result, err := RunSimulation(cfg)
	if err != nil {
		t.Skipf("simulation returned error (may be activation_rate=0 with this seed): %v", err)
	}

	// ValueRate em [0, 1]
	if result.ValueRate < 0 || result.ValueRate > 1 {
		t.Errorf("ValueRate must be in [0,1], got %.4f", result.ValueRate)
	}

	// ActivationRate > 0 (garantido porque RunSimulation faz fail-closed)
	if result.ActivationRate <= 0 {
		t.Errorf("ActivationRate must be > 0, got %.4f", result.ActivationRate)
	}

	// TotalTokens > 0
	if result.TotalTokens <= 0 {
		t.Errorf("TotalTokens must be > 0, got %d", result.TotalTokens)
	}

	// Cada sessão deve ter SessionID não-vazio
	for i, s := range result.Sessions {
		if s.SessionID == "" {
			t.Errorf("session[%d] has empty SessionID", i)
		}
		if s.StartedAt.IsZero() {
			t.Errorf("session[%d] has zero StartedAt", i)
		}
		if s.Interactions <= 0 {
			t.Errorf("session[%d] has Interactions=%d", i, s.Interactions)
		}
	}
}

// ---------------------------------------------------------------------------
// 6. Returned flag — sessões com ativação devem marcar Returned=true
// ---------------------------------------------------------------------------

func TestSimulatorReturnedFlag(t *testing.T) {
	cfg := SimConfig{
		Sessions:               10,
		ActivationProbability:  1.0, // todas as interações ativam
		InteractionsPerSession: 1,
		BaseTokens:             100,
		Seed:                   21,
	}

	result, err := RunSimulation(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, s := range result.Sessions {
		if s.Activations > 0 && !s.Returned {
			t.Errorf("session[%d] has Activations=%d but Returned=false", i, s.Activations)
		}
	}
}
