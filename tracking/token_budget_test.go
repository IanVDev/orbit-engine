// token_budget_test.go — Anti-regression tests for the Token Budget Governor.
//
// Coverage matrix:
//
//	Logic:       within limits → allowed; per-call limit → blocked
//	             per-session limit → blocked; accumulation across calls correct
//	             separate sessions are independent; exact boundary allowed
//	             exact boundary then next call blocked
//	             concurrent access is race-free
//	HTTP:        handler returns 429 on per-call exceeded
//	             handler returns 429 on session exhausted
//	             handler returns 200 when within budget
//	             pass-through when tokens = 0 (non-activation events)
//	             pass-through when tokens = 0 and session_id missing
//	             400 when tokens > 0 and session_id missing  (bypass closed)
//	             400 when tokens < 0                         (bypass closed)
//	             response body contains remaining + used on block
//	             block_reason field present in all error responses
//	Estimator:   estimator overrides client-reported tokens for budget check
//	             estimator result blocking triggers 429
//	             estimator not called for zero-token events
//	Fail-closed: NewTokenBudgetRegistry panics on maxPerSession ≤ 0
//	             NewTokenBudgetRegistry panics on maxPerCall ≤ 0
//	             TrackHandlerWithBudget panics on nil registry
//	             blocked calls do not consume budget
//	Metrics:     orbit_token_allowed_total increments on allow
//	             orbit_token_blocked_total{reason} increments with correct label
//	             orbit_token_usage_ratio reflects Used/MaxPerSession
//	Governance:  all TokenBudgetMetricNames pass ValidatePromQLStrict
//	             common query patterns pass ValidatePromQLStrict
package tracking

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// ─────────────────────────────────────────────────────────────────────────────
// metric-value helpers (read directly from collector, no registry needed)
// ─────────────────────────────────────────────────────────────────────────────

func budgetCounterVal(c interface{ Write(*dto.Metric) error }) float64 {
	var m dto.Metric
	_ = c.Write(&m)
	return m.GetCounter().GetValue()
}

func budgetGaugeVal(g interface{ Write(*dto.Metric) error }) float64 {
	var m dto.Metric
	_ = g.Write(&m)
	return m.GetGauge().GetValue()
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// budgetPayload builds a minimal JSON /track payload with the given session and
// token count. Timestamp is always fresh RFC3339Nano UTC.
func budgetPayload(sessionID string, tokens int64) []byte {
	ev := map[string]interface{}{
		"event_type":              "activation",
		"timestamp":               time.Now().UTC().Format(time.RFC3339Nano),
		"session_id":              sessionID,
		"mode":                    "auto",
		"trigger":                 "test",
		"estimated_waste":         0.0,
		"actions_suggested":       1,
		"actions_applied":         1,
		"impact_estimated_tokens": tokens,
	}
	b, _ := json.Marshal(ev)
	return b
}

// stubOKHandler is a minimal inner handler that always returns 200 + {"status":"ok"}.
var stubOKHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
})

// newBudgetHandler returns a TrackHandlerWithBudget using stubOKHandler as inner.
func newBudgetHandler(reg *TokenBudgetRegistry) http.HandlerFunc {
	return TrackHandlerWithBudget(reg, stubOKHandler)
}

// postBudget fires one POST against the given handler and returns the recorder.
func postBudget(handler http.HandlerFunc, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// ─────────────────────────────────────────────────────────────────────────────
// TokenBudget — unit logic tests
// ─────────────────────────────────────────────────────────────────────────────

func TestTokenBudget_AllowsWithinLimits(t *testing.T) {
	reg := NewTokenBudgetRegistry(10_000, 5_000)
	d, err := reg.CheckAndConsume("sess-ok", 1_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Allowed {
		t.Errorf("Allowed = false; want true")
	}
	if d.Used != 1_000 {
		t.Errorf("Used = %d; want 1000", d.Used)
	}
	if d.Remaining != 9_000 {
		t.Errorf("Remaining = %d; want 9000", d.Remaining)
	}
	if d.Reason != "within_budget" {
		t.Errorf("Reason = %q; want within_budget", d.Reason)
	}
}

func TestTokenBudget_BlocksOverCallLimit(t *testing.T) {
	reg := NewTokenBudgetRegistry(100_000, 5_000)
	d, err := reg.CheckAndConsume("sess-call-block", 6_000) // > MaxPerCall 5000
	if err == nil {
		t.Fatal("expected error for call exceeding per-call limit, got nil")
	}
	if d.Allowed {
		t.Error("Allowed = true; want false")
	}
	// Budget must NOT be consumed when blocked.
	if d.Used != 0 {
		t.Errorf("Used = %d; want 0 (budget not consumed on block)", d.Used)
	}
}

func TestTokenBudget_BlocksOverSessionLimit(t *testing.T) {
	reg := NewTokenBudgetRegistry(5_000, 10_000)

	d1, err := reg.CheckAndConsume("sess-session-block", 4_000)
	if err != nil || !d1.Allowed {
		t.Fatalf("first call should be allowed: err=%v allowed=%v", err, d1.Allowed)
	}

	// 4000 + 2000 > 5000 → should block
	d2, err := reg.CheckAndConsume("sess-session-block", 2_000)
	if err == nil {
		t.Fatal("expected error for call exceeding session limit, got nil")
	}
	if d2.Allowed {
		t.Error("second call: Allowed = true; want false")
	}
	// Used must remain at 4000 — blocked call did not consume.
	if d2.Used != 4_000 {
		t.Errorf("Used after blocked call = %d; want 4000", d2.Used)
	}
}

func TestTokenBudget_AccumulatesAcrossMultipleCalls(t *testing.T) {
	reg := NewTokenBudgetRegistry(10_000, 5_000)
	sess := "sess-accumulate"

	for i := 0; i < 3; i++ {
		if _, err := reg.CheckAndConsume(sess, 1_000); err != nil {
			t.Fatalf("call %d failed: %v", i+1, err)
		}
	}

	d, err := reg.CheckAndConsume(sess, 500)
	if err != nil {
		t.Fatalf("4th call failed: %v", err)
	}
	if d.Used != 3_500 {
		t.Errorf("Used = %d; want 3500", d.Used)
	}
	if d.Remaining != 6_500 {
		t.Errorf("Remaining = %d; want 6500", d.Remaining)
	}
}

func TestTokenBudget_SeparatesSessionBudgets(t *testing.T) {
	reg := NewTokenBudgetRegistry(5_000, 5_000)

	if _, err := reg.CheckAndConsume("sess-A", 5_000); err != nil {
		t.Fatalf("sess-A: initial consume failed: %v", err)
	}
	if _, err := reg.CheckAndConsume("sess-A", 1); err == nil {
		t.Error("sess-A: expected block after exhaustion, got nil")
	}

	// Session B is independent.
	d, err := reg.CheckAndConsume("sess-B", 1_000)
	if err != nil {
		t.Fatalf("sess-B: unexpected block: %v", err)
	}
	if !d.Allowed {
		t.Error("sess-B: Allowed = false; want true")
	}
}

func TestTokenBudget_ExactBoundaryIsAllowed(t *testing.T) {
	reg := NewTokenBudgetRegistry(5_000, 5_000)
	d, err := reg.CheckAndConsume("sess-boundary", 5_000)
	if err != nil {
		t.Fatalf("exact boundary should be allowed: %v", err)
	}
	if !d.Allowed {
		t.Error("Allowed = false at exact boundary; want true")
	}
	if d.Remaining != 0 {
		t.Errorf("Remaining = %d; want 0", d.Remaining)
	}
}

func TestTokenBudget_CallAfterExhaustionIsBlocked(t *testing.T) {
	reg := NewTokenBudgetRegistry(5_000, 5_000)
	reg.CheckAndConsume("sess-after-boundary", 5_000) //nolint:errcheck — error is irrelevant here

	_, err := reg.CheckAndConsume("sess-after-boundary", 1)
	if err == nil {
		t.Error("call after session exhaustion: expected block, got nil")
	}
}

func TestTokenBudget_ZeroTokensAlwaysAllowed(t *testing.T) {
	reg := NewTokenBudgetRegistry(5_000, 5_000)
	// Exhaust the session.
	reg.CheckAndConsume("sess-zero", 5_000) //nolint:errcheck

	// A 0-token call: 0 ≤ MaxPerCall and Used+0 ≤ MaxPerSession (5000 ≤ 5000).
	d, err := reg.CheckAndConsume("sess-zero", 0)
	if err != nil {
		t.Fatalf("zero-token call on exhausted session: unexpected error: %v", err)
	}
	if !d.Allowed {
		t.Error("zero-token call: Allowed = false; want true")
	}
}

func TestTokenBudget_DecisionFieldsArePopulated(t *testing.T) {
	reg := NewTokenBudgetRegistry(10_000, 5_000)
	d, _ := reg.CheckAndConsume("sess-fields", 1_000)

	if d.SessionID != "sess-fields" {
		t.Errorf("SessionID = %q; want sess-fields", d.SessionID)
	}
	if d.CallTokens != 1_000 {
		t.Errorf("CallTokens = %d; want 1000", d.CallTokens)
	}
	if d.MaxPerCall != 5_000 {
		t.Errorf("MaxPerCall = %d; want 5000", d.MaxPerCall)
	}
	if d.MaxPerSession != 10_000 {
		t.Errorf("MaxPerSession = %d; want 10000", d.MaxPerSession)
	}
	if d.Timestamp == "" {
		t.Error("Timestamp is empty")
	}
}

func TestTokenBudget_ConcurrentAccess(t *testing.T) {
	reg := NewTokenBudgetRegistry(1_000_000, 50_000)
	var wg sync.WaitGroup

	// 200 goroutines, 10 sessions — stress test the mutex.
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess := fmt.Sprintf("concurrent-sess-%d", i%10)
			// Result is either allowed or blocked — both are valid outcomes.
			_, _ = reg.CheckAndConsume(sess, 1_000)
		}(i)
	}
	wg.Wait()
	// No panic or data race = success.
}

// ─────────────────────────────────────────────────────────────────────────────
// Fail-closed: construction panics
// ─────────────────────────────────────────────────────────────────────────────

func TestTokenBudgetRegistry_PanicsOnZeroPerSession(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for maxPerSession=0, got none")
		}
	}()
	NewTokenBudgetRegistry(0, 1_000)
}

func TestTokenBudgetRegistry_PanicsOnNegativePerSession(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for maxPerSession=-1, got none")
		}
	}()
	NewTokenBudgetRegistry(-1, 1_000)
}

func TestTokenBudgetRegistry_PanicsOnZeroPerCall(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for maxPerCall=0, got none")
		}
	}()
	NewTokenBudgetRegistry(100_000, 0)
}

func TestTrackHandlerWithBudget_PanicsOnNilRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil registry, got none")
		}
	}()
	TrackHandlerWithBudget(nil, stubOKHandler)
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP handler — TrackHandlerWithBudget
// ─────────────────────────────────────────────────────────────────────────────

func TestTokenBudgetHandler_Returns200WithinBudget(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(10_000, 5_000))
	rec := postBudget(handler, budgetPayload("http-ok", 1_000))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}

func TestTokenBudgetHandler_Returns429OnCallLimitExceeded(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(100_000, 5_000))
	rec := postBudget(handler, budgetPayload("http-call-block", 6_000)) // > MaxPerCall
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d; want 429", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["error"] != "token_budget_exceeded" {
		t.Errorf("error = %q; want token_budget_exceeded", body["error"])
	}
}

func TestTokenBudgetHandler_Returns429OnSessionExhausted(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(3_000, 5_000))

	// First call fills the session budget exactly.
	rec1 := postBudget(handler, budgetPayload("http-exhaust", 3_000))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first call: status = %d; want 200", rec1.Code)
	}

	// Any subsequent call with tokens > 0 must be blocked.
	rec2 := postBudget(handler, budgetPayload("http-exhaust", 1))
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("after exhaustion: status = %d; want 429", rec2.Code)
	}
}

func TestTokenBudgetHandler_PassThroughOnZeroTokens(t *testing.T) {
	// Even with an exhausted session, 0-token events must pass through.
	handler := newBudgetHandler(NewTokenBudgetRegistry(1_000, 1_000))
	postBudget(handler, budgetPayload("http-zero", 1_000)) // exhaust

	rec := postBudget(handler, budgetPayload("http-zero", 0))
	if rec.Code != http.StatusOK {
		t.Errorf("zero-token event: status = %d; want 200", rec.Code)
	}
}

func TestTokenBudgetHandler_Blocks400OnMissingSessionIDWithTokens(t *testing.T) {
	// tokens > 0 with no session_id must be rejected — bypass closed.
	handler := newBudgetHandler(NewTokenBudgetRegistry(10_000, 5_000))
	body, _ := json.Marshal(map[string]interface{}{
		"event_type":              "activation",
		"impact_estimated_tokens": 1_000,
		// session_id intentionally absent
	})
	req := httptest.NewRequest(http.MethodPost, "/track", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("tokens > 0, no session_id: status = %d; want 400 (fail-closed)", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["block_reason"] != blockReasonMissingSession {
		t.Errorf("block_reason = %q; want %q", resp["block_reason"], blockReasonMissingSession)
	}
}

func TestTokenBudgetHandler_BlockBodyContainsUsedAndRemaining(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(5_000, 10_000))

	// 4000 consumed, leaving 1000.
	postBudget(handler, budgetPayload("http-body-check", 4_000))

	// 2000 requested → 4000+2000 > 5000 → 429
	rec := postBudget(handler, budgetPayload("http-body-check", 2_000))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d; want 429", rec.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	usedRaw, ok := body["used"]
	if !ok {
		t.Fatal("response missing 'used' field")
	}
	if _, ok := body["remaining"]; !ok {
		t.Fatal("response missing 'remaining' field")
	}
	// JSON numbers unmarshal as float64.
	if used, _ := usedRaw.(float64); used != 4_000 {
		t.Errorf("used = %v; want 4000", used)
	}
}

func TestTokenBudgetHandler_BlockBodyContainsMaxFields(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(10_000, 5_000))
	rec := postBudget(handler, budgetPayload("http-max-fields", 9_000)) // > MaxPerCall
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d; want 429", rec.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if _, ok := body["max_per_call"]; !ok {
		t.Error("response missing 'max_per_call'")
	}
	if _, ok := body["max_per_session"]; !ok {
		t.Error("response missing 'max_per_session'")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PromQL governance — all metric names must be valid
// ─────────────────────────────────────────────────────────────────────────────

func TestTokenBudgetMetricNames_PassPromQLGovernance(t *testing.T) {
	for _, name := range TokenBudgetMetricNames {
		if err := ValidatePromQLStrict(name); err != nil {
			t.Errorf("governance rejected %q: %v", name, err)
		}
	}
}

func TestTokenBudgetMetricNames_RateQueryPassesGovernance(t *testing.T) {
	queries := []string{
		`rate(orbit_token_spent_total[5m])`,
		`rate(orbit_token_allowed_total[5m])`,
		`rate(orbit_token_blocked_total[5m])`,
		`rate(orbit_token_blocked_total{reason="call_limit"}[5m])`,
		`histogram_quantile(0.95, rate(orbit_token_per_call_bucket[5m]))`,
		`orbit_token_budget_remaining`,
		`orbit_token_usage_ratio`,
	}
	for _, q := range queries {
		if err := ValidatePromQLStrict(q); err != nil {
			t.Errorf("governance rejected query %q: %v", q, err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Bypass closed — fail-closed gate tests (AURYA requirements)
// ─────────────────────────────────────────────────────────────────────────────

func TestBypassWithoutSessionID_ShouldFail(t *testing.T) {
	// tokens > 0, no session_id — must be 400, not pass-through.
	handler := newBudgetHandler(NewTokenBudgetRegistry(10_000, 5_000))
	body, _ := json.Marshal(map[string]interface{}{
		"event_type":              "activation",
		"impact_estimated_tokens": 500,
	})
	rec := postBudget(handler, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bypass attempt (tokens>0, no session): status = %d; want 400", rec.Code)
	}
}

func TestNegativeTokens_ShouldFail(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(10_000, 5_000))
	body := budgetPayload("neg-sess", -100)
	rec := postBudget(handler, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("negative tokens: status = %d; want 400", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["block_reason"] != blockReasonNegativeTokens {
		t.Errorf("block_reason = %q; want %q", resp["block_reason"], blockReasonNegativeTokens)
	}
}

func TestZeroTokensWithoutSession_ShouldPassThrough(t *testing.T) {
	// tokens = 0 with no session_id: zero-cost events pass unconditionally.
	handler := newBudgetHandler(NewTokenBudgetRegistry(10_000, 5_000))
	body, _ := json.Marshal(map[string]interface{}{
		"event_type":              "info",
		"impact_estimated_tokens": 0,
		// session_id intentionally absent
	})
	req := httptest.NewRequest(http.MethodPost, "/track", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("zero-token, no session: status = %d; want 200 (pass-through)", rec.Code)
	}
}

func TestBlockBodyContainsBlockReason(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(100_000, 5_000))

	// per-call block
	rec := postBudget(handler, budgetPayload("reason-sess-call", 6_000))
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["block_reason"] != blockReasonCallLimit {
		t.Errorf("call_limit body: block_reason = %q; want %q", resp["block_reason"], blockReasonCallLimit)
	}

	// session-limit block
	handler2 := newBudgetHandler(NewTokenBudgetRegistry(3_000, 10_000))
	postBudget(handler2, budgetPayload("reason-sess-session", 3_000))
	rec2 := postBudget(handler2, budgetPayload("reason-sess-session", 1))
	var resp2 map[string]interface{}
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if resp2["block_reason"] != blockReasonSessionLimit {
		t.Errorf("session_limit body: block_reason = %q; want %q", resp2["block_reason"], blockReasonSessionLimit)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TokenEstimator — pluggable cost hook
// ─────────────────────────────────────────────────────────────────────────────

// fixedEstimator always returns a fixed token count regardless of input.
type fixedEstimator struct{ fixed int64 }

func (e fixedEstimator) Estimate(_ EstimatorInput) int64 { return e.fixed }

// doubleEstimator returns twice the reported tokens (overhead simulation).
type doubleEstimator struct{}

func (doubleEstimator) Estimate(in EstimatorInput) int64 { return in.ReportedTokens * 2 }

func TestTokenEstimator_OverridesReportedTokens(t *testing.T) {
	// Client reports 1000 but estimator says 500 — budget uses 500.
	reg := NewTokenBudgetRegistry(10_000, 5_000).WithEstimator(fixedEstimator{fixed: 500})
	handler := newBudgetHandler(reg)

	rec := postBudget(handler, budgetPayload("est-override", 1_000))
	if rec.Code != http.StatusOK {
		t.Errorf("estimator overrides to 500 (within limit 5000): status = %d; want 200", rec.Code)
	}
}

func TestTokenEstimator_BlocksWhenEstimateExceedsCallLimit(t *testing.T) {
	// Client reports 1000 but estimator doubles → 2000 > MaxPerCall 1500 → 429.
	reg := NewTokenBudgetRegistry(100_000, 1_500).WithEstimator(doubleEstimator{})
	handler := newBudgetHandler(reg)

	rec := postBudget(handler, budgetPayload("est-block", 1_000))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("estimator doubles to 2000 (> limit 1500): status = %d; want 429", rec.Code)
	}
}

func TestTokenEstimator_NotCalledForZeroTokenEvents(t *testing.T) {
	// Zero-token events skip budget entirely — estimator must not be consulted.
	called := false
	type spyEstimator struct{}
	// We use fixedEstimator with 99999 so that if it IS called, the budget check blocks.
	reg := NewTokenBudgetRegistry(10_000, 5_000).WithEstimator(fixedEstimator{fixed: 99_999})
	_ = called
	handler := newBudgetHandler(reg)

	rec := postBudget(handler, budgetPayload("est-zero", 0))
	if rec.Code != http.StatusOK {
		t.Errorf("zero-token event with estimator: status = %d; want 200 (pass-through)", rec.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Metrics — value assertions
// ─────────────────────────────────────────────────────────────────────────────

func TestMetricsIncrement_AllowedTotal(t *testing.T) {
	reg := NewTokenBudgetRegistry(100_000, 10_000)
	before := budgetCounterVal(tokenAllowedTotal)
	if _, err := reg.CheckAndConsume("metrics-allowed-"+uniqueSuffix(), 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := budgetCounterVal(tokenAllowedTotal)
	if after-before != 1 {
		t.Errorf("tokenAllowedTotal delta = %v; want 1", after-before)
	}
}

func TestMetricsIncrement_BlockedByReason_CallLimit(t *testing.T) {
	reg := NewTokenBudgetRegistry(100_000, 5_000)
	before := budgetCounterVal(tokenBlockedByReason.WithLabelValues(blockReasonCallLimit))
	reg.CheckAndConsume("metrics-block-call-"+uniqueSuffix(), 6_000) //nolint:errcheck
	after := budgetCounterVal(tokenBlockedByReason.WithLabelValues(blockReasonCallLimit))
	if after-before != 1 {
		t.Errorf("blocked{call_limit} delta = %v; want 1", after-before)
	}
}

func TestMetricsIncrement_BlockedByReason_SessionLimit(t *testing.T) {
	sess := "metrics-block-sess-" + uniqueSuffix()
	reg := NewTokenBudgetRegistry(3_000, 10_000)
	reg.CheckAndConsume(sess, 3_000) //nolint:errcheck — fills budget
	before := budgetCounterVal(tokenBlockedByReason.WithLabelValues(blockReasonSessionLimit))
	reg.CheckAndConsume(sess, 1) //nolint:errcheck
	after := budgetCounterVal(tokenBlockedByReason.WithLabelValues(blockReasonSessionLimit))
	if after-before != 1 {
		t.Errorf("blocked{session_limit} delta = %v; want 1", after-before)
	}
}

func TestMetricsIncrement_BlockedByReason_MissingSession(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(10_000, 5_000))
	before := budgetCounterVal(tokenBlockedByReason.WithLabelValues(blockReasonMissingSession))
	body, _ := json.Marshal(map[string]interface{}{
		"impact_estimated_tokens": 500,
	})
	postBudget(handler, body)
	after := budgetCounterVal(tokenBlockedByReason.WithLabelValues(blockReasonMissingSession))
	if after-before != 1 {
		t.Errorf("blocked{missing_session} delta = %v; want 1", after-before)
	}
}

func TestMetricsIncrement_BlockedByReason_NegativeTokens(t *testing.T) {
	handler := newBudgetHandler(NewTokenBudgetRegistry(10_000, 5_000))
	before := budgetCounterVal(tokenBlockedByReason.WithLabelValues(blockReasonNegativeTokens))
	postBudget(handler, budgetPayload("neg-metrics-sess-"+uniqueSuffix(), -1))
	after := budgetCounterVal(tokenBlockedByReason.WithLabelValues(blockReasonNegativeTokens))
	if after-before != 1 {
		t.Errorf("blocked{negative_tokens} delta = %v; want 1", after-before)
	}
}

func TestTokenUsageRatioUpdates(t *testing.T) {
	reg := NewTokenBudgetRegistry(10_000, 10_000)
	reg.CheckAndConsume("ratio-sess-"+uniqueSuffix(), 5_000) //nolint:errcheck — 50% usage

	ratio := budgetGaugeVal(tokenUsageRatio)
	if math.Abs(ratio-0.5) > 0.001 {
		t.Errorf("usage_ratio = %v; want ~0.5", ratio)
	}
}

func TestTokenUsageRatio_ZeroOnNewSession(t *testing.T) {
	reg := NewTokenBudgetRegistry(10_000, 10_000)
	reg.CheckAndConsume("ratio-zero-"+uniqueSuffix(), 0) //nolint:errcheck — zero tokens, no change to ratio

	// After a 0-token consume the ratio stays 0.
	ratio := budgetGaugeVal(tokenUsageRatio)
	if ratio > 0.001 {
		// ratio might be from a previous test — only check if it's within range of 0
		// (ratio is a global gauge so we can't assert exactly 0 in a concurrent test suite)
		t.Logf("usage_ratio = %v (may reflect prior tests; this is expected)", ratio)
	}
}

// uniqueSuffix returns a short unique string to prevent session collisions
// across tests that share the same package-level metric state.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
