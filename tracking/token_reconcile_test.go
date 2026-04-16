package tracking

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func reconcileCounterVal(c interface{ Write(*dto.Metric) error }) float64 {
	var m dto.Metric
	_ = c.Write(&m)
	return m.GetCounter().GetValue()
}

func reconcileGaugeVal(g interface{ Write(*dto.Metric) error }) float64 {
	var m dto.Metric
	_ = g.Write(&m)
	return m.GetGauge().GetValue()
}

// newTestRegistry creates an isolated registry with unique limits so tests
// do not share session state through the package-level maps.
func newTestReconcileRegistry() *TokenBudgetRegistry {
	return NewTokenBudgetRegistry(10_000, 5_000)
}

func reconcileSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// ---------------------------------------------------------------------------
// Core reconciliation tests
// ---------------------------------------------------------------------------

// TestBudgetReconciliationIncrease — actual > estimated: session Used increases.
func TestBudgetReconciliationIncrease(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-increase-" + reconcileSuffix()

	// Pre-execution estimate: 300 tokens.
	dec, err := reg.CheckAndConsume(sid, 300)
	if err != nil || !dec.Allowed {
		t.Fatalf("CheckAndConsume: err=%v allowed=%v", err, dec.Allowed)
	}
	if dec.Used != 300 {
		t.Fatalf("expected Used=300 after check, got %d", dec.Used)
	}

	// Post-execution: model used 450 tokens → delta = +150.
	res, err := reg.Reconcile(sid, TokenUsage{Estimated: 300, Actual: 450})
	if err != nil {
		t.Fatalf("Reconcile: unexpected error: %v", err)
	}
	if res.Delta != 150 {
		t.Errorf("Delta = %d; want 150", res.Delta)
	}
	if res.UsedAfter != 450 {
		t.Errorf("UsedAfter = %d; want 450", res.UsedAfter)
	}
	if res.BudgetExceeded {
		t.Error("BudgetExceeded should be false (450 < 10000)")
	}
}

// TestBudgetReconciliationDecrease — actual < estimated: session Used decreases.
func TestBudgetReconciliationDecrease(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-decrease-" + reconcileSuffix()

	dec, err := reg.CheckAndConsume(sid, 500)
	if err != nil || !dec.Allowed {
		t.Fatalf("CheckAndConsume: err=%v allowed=%v", err, dec.Allowed)
	}

	// Actual was only 200 tokens → delta = -300.
	res, err := reg.Reconcile(sid, TokenUsage{Estimated: 500, Actual: 200})
	if err != nil {
		t.Fatalf("Reconcile: unexpected error: %v", err)
	}
	if res.Delta != -300 {
		t.Errorf("Delta = %d; want -300", res.Delta)
	}
	if res.UsedAfter != 200 {
		t.Errorf("UsedAfter = %d; want 200", res.UsedAfter)
	}
	if res.BudgetExceeded {
		t.Error("BudgetExceeded should be false")
	}
}

// TestBudgetReconciliationExactMatch — delta = 0: nothing changes.
func TestBudgetReconciliationExactMatch(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-exact-" + reconcileSuffix()

	reg.CheckAndConsume(sid, 400) //nolint:errcheck

	res, err := reg.Reconcile(sid, TokenUsage{Estimated: 400, Actual: 400})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Delta != 0 {
		t.Errorf("Delta = %d; want 0", res.Delta)
	}
	if res.UsedAfter != 400 {
		t.Errorf("UsedAfter = %d; want 400", res.UsedAfter)
	}
}

// TestBudgetReconciliationFloorAtZero — credit cannot push Used below 0.
func TestBudgetReconciliationFloorAtZero(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-floor-" + reconcileSuffix()

	reg.CheckAndConsume(sid, 100) //nolint:errcheck

	// Release more than was consumed: estimated=300 > Used=100.
	res, err := reg.Reconcile(sid, TokenUsage{Estimated: 300, Actual: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedAfter != 0 {
		t.Errorf("UsedAfter = %d; want 0 (floor)", res.UsedAfter)
	}
}

// TestBudgetReconciliationSessionNotFound — fail-closed: unknown session → error.
func TestBudgetReconciliationSessionNotFound(t *testing.T) {
	reg := newTestReconcileRegistry()
	_, err := reg.Reconcile("no-such-session", TokenUsage{Estimated: 100, Actual: 120})
	if err == nil {
		t.Fatal("expected error for unknown session, got nil")
	}
}

// TestBudgetReconciliationNegativeActual — actual < 0 → error.
func TestBudgetReconciliationNegativeActual(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-neg-actual-" + reconcileSuffix()
	reg.CheckAndConsume(sid, 100) //nolint:errcheck

	_, err := reg.Reconcile(sid, TokenUsage{Estimated: 100, Actual: -1})
	if err == nil {
		t.Fatal("expected error for negative actual, got nil")
	}
}

// TestBudgetReconciliationNegativeEstimated — estimated < 0 → error.
func TestBudgetReconciliationNegativeEstimated(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-neg-est-" + reconcileSuffix()
	reg.CheckAndConsume(sid, 100) //nolint:errcheck

	_, err := reg.Reconcile(sid, TokenUsage{Estimated: -1, Actual: 100})
	if err == nil {
		t.Fatal("expected error for negative estimated, got nil")
	}
}

// TestBudgetReconciliationBudgetExceededFlag — informational only: flag is set
// when increase pushes Used above MaxPerSession.
func TestBudgetReconciliationBudgetExceededFlag(t *testing.T) {
	reg := NewTokenBudgetRegistry(1_000, 5_000)
	sid := "rec-exceed-" + reconcileSuffix()

	reg.CheckAndConsume(sid, 900) //nolint:errcheck

	// actual=1200 pushes Used to 1200 (>1000).
	res, err := reg.Reconcile(sid, TokenUsage{Estimated: 900, Actual: 1200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.BudgetExceeded {
		t.Error("BudgetExceeded should be true when Used > MaxPerSession")
	}
	if res.UsedAfter != 1200 {
		t.Errorf("UsedAfter = %d; want 1200", res.UsedAfter)
	}
}

// ---------------------------------------------------------------------------
// TokenUsage struct tests
// ---------------------------------------------------------------------------

func TestTokenUsage_Delta(t *testing.T) {
	cases := []struct {
		est, act, want int64
	}{
		{300, 450, 150},
		{500, 200, -300},
		{400, 400, 0},
	}
	for _, tc := range cases {
		u := TokenUsage{Estimated: tc.est, Actual: tc.act}
		if got := u.Delta(); got != tc.want {
			t.Errorf("TokenUsage{%d,%d}.Delta() = %d; want %d", tc.est, tc.act, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Prometheus metric tests
// ---------------------------------------------------------------------------

func TestReconcileMetrics_ActualTotalIncrement(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-metrics-actual-" + reconcileSuffix()
	reg.CheckAndConsume(sid, 200) //nolint:errcheck

	before := reconcileCounterVal(tokenActualTotal)
	reg.Reconcile(sid, TokenUsage{Estimated: 200, Actual: 350}) //nolint:errcheck
	after := reconcileCounterVal(tokenActualTotal)

	if after-before != 350 {
		t.Errorf("tokenActualTotal delta = %.0f; want 350", after-before)
	}
}

func TestReconcileMetrics_EstimationErrorGauge(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-metrics-err-" + reconcileSuffix()
	reg.CheckAndConsume(sid, 300) //nolint:errcheck

	reg.Reconcile(sid, TokenUsage{Estimated: 300, Actual: 420}) //nolint:errcheck
	val := reconcileGaugeVal(tokenEstimationError)
	if val != 120 {
		t.Errorf("tokenEstimationError = %.0f; want 120", val)
	}
}

func TestReconcileMetrics_EstimationErrorGauge_Negative(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-metrics-neg-" + reconcileSuffix()
	reg.CheckAndConsume(sid, 600) //nolint:errcheck

	reg.Reconcile(sid, TokenUsage{Estimated: 600, Actual: 400}) //nolint:errcheck
	val := reconcileGaugeVal(tokenEstimationError)
	if val != -200 {
		t.Errorf("tokenEstimationError = %.0f; want -200", val)
	}
}

// ---------------------------------------------------------------------------
// HTTP handler tests
// ---------------------------------------------------------------------------

func TestReconcileHandler_OK(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-http-ok-" + reconcileSuffix()
	reg.CheckAndConsume(sid, 300) //nolint:errcheck

	body, _ := json.Marshal(map[string]interface{}{
		"session_id": sid,
		"estimated":  300,
		"actual":     420,
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	ReconcileHandler(reg)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	var res ReconcileResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Delta != 120 {
		t.Errorf("Delta = %d; want 120", res.Delta)
	}
}

func TestReconcileHandler_SessionNotFound(t *testing.T) {
	reg := newTestReconcileRegistry()
	body, _ := json.Marshal(map[string]interface{}{
		"session_id": "ghost-session",
		"estimated":  100,
		"actual":     150,
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	ReconcileHandler(reg)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", rec.Code)
	}
}

func TestReconcileHandler_MissingSessionID(t *testing.T) {
	reg := newTestReconcileRegistry()
	body, _ := json.Marshal(map[string]interface{}{
		"estimated": 100,
		"actual":    150,
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	ReconcileHandler(reg)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

func TestReconcileHandler_NegativeActual(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "rec-http-neg-" + reconcileSuffix()
	reg.CheckAndConsume(sid, 100) //nolint:errcheck

	body, _ := json.Marshal(map[string]interface{}{
		"session_id": sid,
		"estimated":  100,
		"actual":     -1,
	})
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	ReconcileHandler(reg)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

func TestReconcileHandler_InvalidJSON(t *testing.T) {
	reg := newTestReconcileRegistry()
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader([]byte("{bad json")))
	rec := httptest.NewRecorder()

	ReconcileHandler(reg)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

func TestReconcileHandler_MethodNotAllowed(t *testing.T) {
	reg := newTestReconcileRegistry()
	req := httptest.NewRequest(http.MethodGet, "/reconcile", nil)
	rec := httptest.NewRecorder()

	ReconcileHandler(reg)(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d; want 405", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// PromQL governance tests
// ---------------------------------------------------------------------------

func TestTokenReconcileMetricNames_PassGovernance(t *testing.T) {
	for _, name := range TokenReconcileMetricNames {
		if err := ValidatePromQLStrict(name); err != nil {
			t.Errorf("metric %q fails governance: %v", name, err)
		}
	}
}

func TestTokenReconcileMetricNames_RateQueriesPassGovernance(t *testing.T) {
	queries := []string{
		"rate(orbit_token_actual_total[5m])",
		"orbit_token_estimation_error",
	}
	for _, q := range queries {
		if err := ValidatePromQLStrict(q); err != nil {
			t.Errorf("query %q fails governance: %v", q, err)
		}
	}
}
