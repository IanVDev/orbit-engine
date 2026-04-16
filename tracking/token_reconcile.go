// token_reconcile.go — Post-execution token reconciliation for orbit-engine.
//
// After a call completes, the actual token cost reported by the model API
// often differs from the pre-execution estimate. Reconcile adjusts the
// session budget to reflect the real spend:
//
//   - delta > 0: actual > estimated → consume additional tokens (may exceed limit; informational).
//   - delta < 0: actual < estimated → release credit back to the session.
//   - delta = 0: estimate was exact; no adjustment needed.
//
// Budget floor: session Used is never set below 0 (credit cannot go negative).
//
// Fail-closed rules:
//   - unknown session_id → 404 (cannot reconcile without prior CheckAndConsume).
//   - actual < 0         → 400 (invalid request).
//   - estimated < 0      → 400 (invalid request).
//
// HTTP: POST /reconcile
//
//	Request:  {"session_id":"…","estimated":500,"actual":620}
//	Response: {"session_id":"…","estimated":500,"actual":620,"delta":120,"used_after":…,"remaining_after":…,"budget_exceeded":false}
//
// Metrics:
//
//	orbit_token_actual_total      Counter — cumulative actual tokens reported
//	orbit_token_estimation_error  Gauge   — most recent delta (actual − estimated); can be negative
package tracking

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// TokenUsage — estimated vs actual cost for one execution
// ---------------------------------------------------------------------------

// TokenUsage holds the pre-execution estimate and the real post-execution cost.
type TokenUsage struct {
	Estimated int64 `json:"estimated"`
	Actual    int64 `json:"actual"`
}

// Delta returns the difference between actual and estimated cost.
// Positive: execution was more expensive than estimated.
// Negative: execution was cheaper; credit will be returned to the session.
func (u TokenUsage) Delta() int64 {
	return u.Actual - u.Estimated
}

// ---------------------------------------------------------------------------
// ReconcileResult — outcome of a single Reconcile call
// ---------------------------------------------------------------------------

// ReconcileResult captures the full state after reconciliation.
// Returned from Reconcile and logged as JSONL.
type ReconcileResult struct {
	SessionID     string `json:"session_id"`
	Estimated     int64  `json:"estimated"`
	Actual        int64  `json:"actual"`
	Delta         int64  `json:"delta"`
	UsedAfter     int64  `json:"used_after"`
	RemainingAfter int64 `json:"remaining_after"`
	MaxPerSession int64  `json:"max_per_session"`
	// BudgetExceeded is informational only. Execution already happened;
	// the excess is recorded for observability, not for blocking.
	BudgetExceeded bool   `json:"budget_exceeded"`
	Timestamp      string `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Reconcile — post-execution budget adjustment
// ---------------------------------------------------------------------------

// Reconcile adjusts the session budget based on the difference between the
// estimated token cost (used during CheckAndConsume) and the actual cost
// reported by the model API after execution.
//
// Fail-closed:
//   - Session not found → error (caller must have called CheckAndConsume first).
//   - usage.Actual < 0  → error (invalid).
//   - usage.Estimated < 0 → error (invalid).
//
// Budget adjustment (atomic, under registry mutex):
//   - delta > 0: b.Used += delta (may push Used above MaxPerSession; informational).
//   - delta < 0: b.Used += delta, floor at 0.
func (r *TokenBudgetRegistry) Reconcile(sessionID string, usage TokenUsage) (ReconcileResult, error) {
	if usage.Estimated < 0 {
		return ReconcileResult{}, fmt.Errorf("token-reconcile: estimated=%d is invalid (must be ≥ 0)", usage.Estimated)
	}
	if usage.Actual < 0 {
		return ReconcileResult{}, fmt.Errorf("token-reconcile: actual=%d is invalid (must be ≥ 0)", usage.Actual)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	b, ok := r.sessions[sessionID]
	if !ok {
		return ReconcileResult{}, fmt.Errorf("token-reconcile: session %q not found (CheckAndConsume must precede Reconcile)", sessionID)
	}

	delta := usage.Delta()

	// Apply delta; floor at 0 (cannot release more than was consumed).
	b.Used += delta
	if b.Used < 0 {
		b.Used = 0
	}

	budgetExceeded := b.Used > b.MaxPerSession

	result := ReconcileResult{
		SessionID:      sessionID,
		Estimated:      usage.Estimated,
		Actual:         usage.Actual,
		Delta:          delta,
		UsedAfter:      b.Used,
		RemainingAfter: b.Remaining(),
		MaxPerSession:  b.MaxPerSession,
		BudgetExceeded: budgetExceeded,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
	}

	emitReconcileLog(result)
	tokenActualTotal.Add(float64(usage.Actual))
	tokenEstimationError.Set(float64(delta))
	tokenBudgetRemainingGauge.Set(float64(result.RemainingAfter))
	tokenUsageRatio.Set(b.UsageRatio())

	return result, nil
}

// ---------------------------------------------------------------------------
// JSONL logging
// ---------------------------------------------------------------------------

type reconcileLogEntry struct {
	Event          string `json:"event"`
	SessionID      string `json:"session_id"`
	Estimated      int64  `json:"estimated"`
	Actual         int64  `json:"actual"`
	Delta          int64  `json:"delta"`
	UsedAfter      int64  `json:"used_after"`
	RemainingAfter int64  `json:"remaining_after"`
	MaxPerSession  int64  `json:"max_per_session"`
	BudgetExceeded bool   `json:"budget_exceeded"`
	Timestamp      string `json:"timestamp"`
}

func emitReconcileLog(res ReconcileResult) {
	entry := reconcileLogEntry{
		Event:          "token_reconcile",
		SessionID:      res.SessionID,
		Estimated:      res.Estimated,
		Actual:         res.Actual,
		Delta:          res.Delta,
		UsedAfter:      res.UsedAfter,
		RemainingAfter: res.RemainingAfter,
		MaxPerSession:  res.MaxPerSession,
		BudgetExceeded: res.BudgetExceeded,
		Timestamp:      res.Timestamp,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[TOKEN_RECONCILE][ERROR] marshal failed: %v", err)
		return
	}
	log.Printf("[TOKEN_RECONCILE] %s", b)
}

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var (
	// orbit_token_actual_total — cumulative actual tokens reported post-execution.
	// rate() gives real token consumption velocity; compare with tokenSpentTotal for
	// estimation accuracy.
	tokenActualTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orbit_token_actual_total",
		Help: "Cumulative actual tokens reported post-execution across all sessions.",
	})

	// orbit_token_estimation_error — most recent delta (actual − estimated).
	// Can be negative (over-estimate) or positive (under-estimate).
	// Alert: abs(orbit_token_estimation_error) > threshold → estimator needs recalibration.
	tokenEstimationError = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orbit_token_estimation_error",
		Help: "Most recent reconciliation delta (actual − estimated tokens). Negative = over-estimate.",
	})
)

var tokenReconcileMetricsOnce sync.Once

// RegisterTokenReconcileMetrics registers reconciliation Prometheus collectors.
// Call once at startup alongside RegisterTokenBudgetMetrics.
func RegisterTokenReconcileMetrics(reg prometheus.Registerer) {
	tokenReconcileMetricsOnce.Do(func() {
		reg.MustRegister(
			tokenActualTotal,
			tokenEstimationError,
		)
	})
}

// TokenReconcileMetricNames is the closed set of metric names owned by this file.
// Used by governance tests to verify all names pass ValidatePromQLStrict.
var TokenReconcileMetricNames = []string{
	"orbit_token_actual_total",
	"orbit_token_estimation_error",
}

// ---------------------------------------------------------------------------
// HTTP handler — ReconcileHandler
// ---------------------------------------------------------------------------

// reconcileRequest is the JSON body expected by POST /reconcile.
type reconcileRequest struct {
	SessionID string `json:"session_id"`
	Estimated int64  `json:"estimated"`
	Actual    int64  `json:"actual"`
}

// ReconcileHandler returns an http.HandlerFunc that processes POST /reconcile.
//
// Request:  {"session_id":"…","estimated":500,"actual":620}
// Response 200: ReconcileResult JSON
// Response 400: invalid request (missing session_id, negative values, bad JSON)
// Response 404: session not found (CheckAndConsume must precede Reconcile)
func ReconcileHandler(registry *TokenBudgetRegistry) http.HandlerFunc {
	if registry == nil {
		panic("orbit-engine: ReconcileHandler received nil registry (fail-closed)")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req reconcileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			resp, _ := json.Marshal(map[string]string{
				"error":  "invalid_json",
				"detail": err.Error(),
			})
			_, _ = w.Write(resp)
			return
		}

		if req.SessionID == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			resp, _ := json.Marshal(map[string]string{
				"error":  "missing_session_id",
				"detail": "session_id is required",
			})
			_, _ = w.Write(resp)
			return
		}

		result, err := registry.Reconcile(req.SessionID, TokenUsage{
			Estimated: req.Estimated,
			Actual:    req.Actual,
		})
		if err != nil {
			// Distinguish session-not-found (404) from validation errors (400).
			status := http.StatusBadRequest
			errMsg := err.Error()
			if containsSessionNotFound(errMsg) {
				status = http.StatusNotFound
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			resp, _ := json.Marshal(map[string]string{
				"error":  "reconcile_failed",
				"detail": errMsg,
			})
			_, _ = w.Write(resp)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp, _ := json.Marshal(result)
		_, _ = w.Write(resp)
	}
}

// containsSessionNotFound identifies session-not-found errors without adding
// package-level sentinel error variables.
func containsSessionNotFound(msg string) bool {
	return strings.Contains(msg, "not found") || strings.Contains(msg, "CheckAndConsume must precede")
}
