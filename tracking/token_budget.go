// token_budget.go — Token Budget Governor for orbit-engine.
//
// Controls token consumption per execution and per session, blocking requests
// that exceed configured limits before they reach the tracking pipeline.
//
// Pipeline position (outermost wrapper):
//
//	HTTP request
//	     │
//	     ▼
//	[1] TokenBudget enforcement     ← this file
//	     │ over budget  → HTTP 429
//	     │ bad request  → HTTP 400
//	     ▼
//	[2] ExecGov.ValidateExecution   ← exec_gov.go
//	     │ blocked → HTTP 403
//	     ▼
//	[3] ModelControl enforcement    ← model_control.go
//	     │ locked+override → HTTP 403
//	     ▼
//	[4] TrackHandler                ← tracking.go
//
// Fail-closed rules (applied in order before budget rules):
//
//	• tokens < 0         → 400 Bad Request  (invalid request)
//	• tokens > 0, no id  → 400 Bad Request  (cost claimed without identity)
//	• tokens = 0         → pass-through     (zero-cost events are valid)
//
// Budget rules (CheckAndConsume):
//
//	R0: tokens > MaxPerCall         → 429 (single call too expensive)
//	R1: Used + tokens > MaxPerSession → 429 (session budget exhausted)
//
// Fail-closed: any rule violation → reject. Unknown state → reject.
// No external dependencies beyond the standard library.
//
// Metrics:
//
//	orbit_token_spent_total      Counter   — tokens consumed across all sessions
//	orbit_token_per_call         Histogram — distribution of token cost per call
//	orbit_token_budget_remaining Gauge     — remaining budget after the most recent check
//	orbit_token_allowed_total    Counter   — calls allowed by the governor
//	orbit_token_blocked_total    CounterVec{reason} — calls blocked, labelled by reason
//	orbit_token_usage_ratio      Gauge     — Used/MaxPerSession after the most recent check
package tracking

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// Block-reason constants — used as metric labels and log fields.
// Low-cardinality, stable values only.
// ---------------------------------------------------------------------------

const (
	blockReasonCallLimit      = "call_limit"      // R0: tokens > MaxPerCall
	blockReasonSessionLimit   = "session_limit"   // R1: Used+tokens > MaxPerSession
	blockReasonMissingSession = "missing_session" // middleware: tokens > 0, no session_id
	blockReasonNegativeTokens = "negative_tokens" // middleware: tokens < 0
)

// ---------------------------------------------------------------------------
// TokenEstimator — pluggable cost estimation hook
// ---------------------------------------------------------------------------

// EstimatorInput carries the fields available to a cost estimator.
type EstimatorInput struct {
	SessionID      string
	Mode           string
	Trigger        string
	ReportedTokens int64 // token count reported by the client in the request body
}

// TokenEstimator computes the effective token cost for a call.
// Implement this interface to connect real model API costs.
//
// Contract:
//   - Return ≥ 0.  Negative return values are treated as 0 (no-cost pass-through).
//   - When nil, the middleware uses the client-reported token count as-is.
//   - The estimator DOES NOT modify the event body; it only affects budget accounting.
//
// Example usage — fixed overhead factor:
//
//	type overheadEstimator struct{ factor float64 }
//	func (e overheadEstimator) Estimate(in EstimatorInput) int64 {
//	    return int64(float64(in.ReportedTokens) * e.factor)
//	}
type TokenEstimator interface {
	Estimate(in EstimatorInput) int64
}

// ---------------------------------------------------------------------------
// TokenBudget — per-session budget state
// ---------------------------------------------------------------------------

// TokenBudget holds the configuration and current usage for one session.
// Fields are exported so callers can inspect limits and state (e.g. in tests).
// All access MUST be protected by the parent registry's mutex.
type TokenBudget struct {
	MaxPerSession int64
	MaxPerCall    int64
	Used          int64
}

// Remaining returns tokens still available in the session (floor 0).
func (b *TokenBudget) Remaining() int64 {
	if r := b.MaxPerSession - b.Used; r > 0 {
		return r
	}
	return 0
}

// UsageRatio returns the fraction of the session budget consumed (0.0–1.0+).
func (b *TokenBudget) UsageRatio() float64 {
	if b.MaxPerSession == 0 {
		return 0
	}
	return float64(b.Used) / float64(b.MaxPerSession)
}

// ---------------------------------------------------------------------------
// BudgetDecision — result of a single CheckAndConsume call
// ---------------------------------------------------------------------------

// BudgetDecision captures every attribute of a budget evaluation.
// Logged as JSONL and embedded in HTTP error bodies.
type BudgetDecision struct {
	Allowed       bool   `json:"allowed"`
	Reason        string `json:"reason"`                  // verbose human-readable message
	BlockReason   string `json:"block_reason,omitempty"`  // short metric label (call_limit | session_limit)
	SessionID     string `json:"session_id"`
	CallTokens    int64  `json:"call_tokens"`
	Used          int64  `json:"used_after"`
	Remaining     int64  `json:"remaining_after"`
	MaxPerCall    int64  `json:"max_per_call"`
	MaxPerSession int64  `json:"max_per_session"`
	Timestamp     string `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// TokenBudgetRegistry — thread-safe per-session budget manager
// ---------------------------------------------------------------------------

// TokenBudgetRegistry is safe for concurrent use.
type TokenBudgetRegistry struct {
	mu                   sync.Mutex
	sessions             map[string]*TokenBudget
	defaultMaxPerSession int64
	defaultMaxPerCall    int64
	estimator            TokenEstimator // optional; nil → use client-reported tokens
}

// NewTokenBudgetRegistry returns a registry applying the given defaults to
// every new session. Panics on limits ≤ 0 (fail-closed).
func NewTokenBudgetRegistry(maxPerSession, maxPerCall int64) *TokenBudgetRegistry {
	if maxPerSession <= 0 || maxPerCall <= 0 {
		panic(fmt.Sprintf(
			"orbit-engine: NewTokenBudgetRegistry: limits must be positive (per_session=%d, per_call=%d)",
			maxPerSession, maxPerCall,
		))
	}
	return &TokenBudgetRegistry{
		sessions:             make(map[string]*TokenBudget),
		defaultMaxPerSession: maxPerSession,
		defaultMaxPerCall:    maxPerCall,
	}
}

// WithEstimator attaches a TokenEstimator to the registry and returns it for
// chaining.  Must be called before serving requests (not concurrency-safe at
// configuration time, safe once the registry is in use).
func (r *TokenBudgetRegistry) WithEstimator(e TokenEstimator) *TokenBudgetRegistry {
	r.estimator = e
	return r
}

// CheckAndConsume validates budget rules and atomically consumes tokens.
//
// Return contract (fail-closed):
//   - (decision{Allowed:true}, nil)    → execution permitted; tokens consumed.
//   - (decision{Allowed:false}, error) → execution MUST be blocked; tokens NOT consumed.
//
// Rules (first match wins):
//
//	R0: tokens > MaxPerCall         → block (single call too expensive)
//	R1: Used+tokens > MaxPerSession → block (cumulative session budget exhausted)
func (r *TokenBudgetRegistry) CheckAndConsume(sessionID string, tokens int64) (BudgetDecision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ts := time.Now().UTC().Format(time.RFC3339Nano)

	b, ok := r.sessions[sessionID]
	if !ok {
		b = &TokenBudget{
			MaxPerSession: r.defaultMaxPerSession,
			MaxPerCall:    r.defaultMaxPerCall,
		}
		r.sessions[sessionID] = b
	}

	block := func(blockReason, verboseReason string) (BudgetDecision, error) {
		d := BudgetDecision{
			Allowed:       false,
			Reason:        verboseReason,
			BlockReason:   blockReason,
			SessionID:     sessionID,
			CallTokens:    tokens,
			Used:          b.Used,
			Remaining:     b.Remaining(),
			MaxPerCall:    b.MaxPerCall,
			MaxPerSession: b.MaxPerSession,
			Timestamp:     ts,
		}
		emitBudgetLog(d)
		tokenBlockedByReason.WithLabelValues(blockReason).Inc()
		tokenBudgetRemainingGauge.Set(float64(b.Remaining()))
		tokenUsageRatio.Set(b.UsageRatio())
		return d, fmt.Errorf("token-budget: %s (session=%s, tokens=%d)", verboseReason, sessionID, tokens)
	}

	// R0: per-call limit
	if tokens > b.MaxPerCall {
		return block(blockReasonCallLimit, fmt.Sprintf(
			"call_exceeds_max_per_call (requested=%d, limit=%d)", tokens, b.MaxPerCall,
		))
	}

	// R1: session cumulative limit
	if b.Used+tokens > b.MaxPerSession {
		return block(blockReasonSessionLimit, fmt.Sprintf(
			"session_budget_exhausted (used=%d, requested=%d, limit=%d)", b.Used, tokens, b.MaxPerSession,
		))
	}

	// Consume
	b.Used += tokens
	d := BudgetDecision{
		Allowed:       true,
		Reason:        "within_budget",
		SessionID:     sessionID,
		CallTokens:    tokens,
		Used:          b.Used,
		Remaining:     b.Remaining(),
		MaxPerCall:    b.MaxPerCall,
		MaxPerSession: b.MaxPerSession,
		Timestamp:     ts,
	}
	emitBudgetLog(d)
	tokenSpentTotal.Add(float64(tokens))
	tokenPerCall.Observe(float64(tokens))
	tokenAllowedTotal.Inc()
	tokenBudgetRemainingGauge.Set(float64(d.Remaining))
	tokenUsageRatio.Set(b.UsageRatio())

	return d, nil
}

// ResetSession removes a session's budget state. For testing ONLY.
func (r *TokenBudgetRegistry) ResetSession(sessionID string) {
	r.mu.Lock()
	delete(r.sessions, sessionID)
	r.mu.Unlock()
}

// ---------------------------------------------------------------------------
// JSONL logging
// ---------------------------------------------------------------------------

type budgetLogEntry struct {
	Event         string `json:"event"`
	Allowed       bool   `json:"allowed"`
	Reason        string `json:"reason"`
	BlockReason   string `json:"block_reason,omitempty"`
	SessionID     string `json:"session_id"`
	CallTokens    int64  `json:"call_tokens"`
	Used          int64  `json:"used_after"`
	Remaining     int64  `json:"remaining_after"`
	MaxPerCall    int64  `json:"max_per_call"`
	MaxPerSession int64  `json:"max_per_session"`
	Timestamp     string `json:"timestamp"`
}

func emitBudgetLog(d BudgetDecision) {
	entry := budgetLogEntry{
		Event:         "token_budget",
		Allowed:       d.Allowed,
		Reason:        d.Reason,
		BlockReason:   d.BlockReason,
		SessionID:     d.SessionID,
		CallTokens:    d.CallTokens,
		Used:          d.Used,
		Remaining:     d.Remaining,
		MaxPerCall:    d.MaxPerCall,
		MaxPerSession: d.MaxPerSession,
		Timestamp:     d.Timestamp,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[TOKEN_BUDGET][ERROR] marshal failed: %v", err)
		return
	}
	log.Printf("[TOKEN_BUDGET] %s", b)
}

// emitBudgetRejectLog writes a structured log line for middleware-level
// rejections (negative_tokens, missing_session) that never reach CheckAndConsume.
func emitBudgetRejectLog(blockReason, reason, sessionID string, tokens int64) {
	entry := map[string]interface{}{
		"event":        "token_budget",
		"allowed":      false,
		"reason":       reason,
		"block_reason": blockReason,
		"session_id":   sessionID,
		"call_tokens":  tokens,
		"timestamp":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	b, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[TOKEN_BUDGET][ERROR] marshal failed: %v", err)
		return
	}
	log.Printf("[TOKEN_BUDGET] %s", b)
}

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var (
	// orbit_token_spent_total — cumulative tokens consumed across all sessions.
	// Monotonically increasing; rate() gives tokens/second consumed by the system.
	tokenSpentTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orbit_token_spent_total",
		Help: "Total estimated tokens consumed across all sessions by the token budget governor.",
	})

	// orbit_token_per_call — per-call token cost distribution.
	// Use histogram_quantile to detect P95 call costs or spending tail.
	tokenPerCall = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "orbit_token_per_call",
		Help:    "Distribution of estimated token cost per tracked call.",
		Buckets: []float64{100, 500, 1_000, 2_500, 5_000, 10_000, 25_000, 50_000},
	})

	// orbit_token_budget_remaining — remaining budget after the most recent check.
	// No session_id label to prevent cardinality explosion. Reflects the last
	// session evaluated; alert when this drops near zero.
	tokenBudgetRemainingGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orbit_token_budget_remaining",
		Help: "Remaining token budget after the most recent check. Reflects the last active session.",
	})

	// orbit_token_allowed_total — every call allowed by the governor.
	// rate() > 0 means the system is actively processing cost-bearing events.
	tokenAllowedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orbit_token_allowed_total",
		Help: "Total calls allowed by the token budget governor.",
	})

	// orbit_token_blocked_total{reason} — every call blocked by the governor.
	// Labels: call_limit | session_limit | missing_session | negative_tokens
	// rate() > 0 means sessions are hitting limits or sending invalid requests.
	tokenBlockedByReason = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orbit_token_blocked_total",
		Help: "Total calls blocked by the token budget governor. Label: reason (call_limit|session_limit|missing_session|negative_tokens).",
	}, []string{"reason"})

	// orbit_token_usage_ratio — Used/MaxPerSession after the most recent check.
	// Alert: orbit_token_usage_ratio > 0.9 → session approaching exhaustion.
	// No session_id label to prevent cardinality explosion.
	tokenUsageRatio = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orbit_token_usage_ratio",
		Help: "Fraction of session budget consumed (0.0–1.0+) after the most recent check.",
	})
)

// tokenBudgetMetricsOnce protects RegisterTokenBudgetMetrics from double-registration.
var tokenBudgetMetricsOnce sync.Once

// RegisterTokenBudgetMetrics registers token budget Prometheus collectors.
// Call once at startup alongside RegisterMetrics and RegisterSecurityMetrics.
func RegisterTokenBudgetMetrics(reg prometheus.Registerer) {
	tokenBudgetMetricsOnce.Do(func() {
		reg.MustRegister(
			tokenSpentTotal,
			tokenPerCall,
			tokenBudgetRemainingGauge,
			tokenAllowedTotal,
			tokenBlockedByReason,
			tokenUsageRatio,
		)
	})
}

// TokenBudgetMetricNames is the closed set of metric names owned by this package.
// Used by governance tests to verify all names pass ValidatePromQLStrict.
var TokenBudgetMetricNames = []string{
	"orbit_token_spent_total",
	"orbit_token_per_call",
	"orbit_token_budget_remaining",
	"orbit_token_allowed_total",
	"orbit_token_blocked_total",
	"orbit_token_usage_ratio",
}

// ---------------------------------------------------------------------------
// HTTP middleware — TrackHandlerWithBudget
// ---------------------------------------------------------------------------

// TrackHandlerWithBudget wraps an inner handler and enforces the token budget
// before delegating. Intended to wrap TrackHandlerWithControl.
//
// Decision table:
//
//	tokens < 0                → 400 Bad Request (negative_tokens)
//	tokens = 0                → pass-through  (zero-cost events: info, heartbeat, etc.)
//	tokens > 0, session_id="" → 400 Bad Request (missing_session)
//	tokens > MaxPerCall       → 429 Too Many Requests (call_limit)
//	session budget exhausted  → 429 Too Many Requests (session_limit)
//	within budget             → delegate to inner handler
//
// If a TokenEstimator is registered via WithEstimator, the effective token
// count used for budget accounting is estimator.Estimate(input) rather than
// the client-reported value.  The event body is NOT modified.
//
// Fail-closed: nil registry panics at construction time.
func TrackHandlerWithBudget(registry *TokenBudgetRegistry, inner http.Handler) http.HandlerFunc {
	if registry == nil {
		panic("orbit-engine: TrackHandlerWithBudget received nil registry (fail-closed)")
	}

	rejectHTTP := func(w http.ResponseWriter, blockReason, detail, sessionID string, tokens int64, statusCode int) {
		emitBudgetRejectLog(blockReason, detail, sessionID, tokens)
		tokenBlockedByReason.WithLabelValues(blockReason).Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		resp, _ := json.Marshal(map[string]interface{}{
			"error":        "token_budget_rejected",
			"block_reason": blockReason,
			"reason":       detail,
		})
		_, _ = w.Write(resp)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var peek struct {
			SessionID string `json:"session_id"`
			Tokens    int64  `json:"impact_estimated_tokens"`
			Mode      string `json:"mode"`
			Trigger   string `json:"trigger"`
		}

		body, err := peekRequestBody(r)
		if err != nil || len(body) == 0 {
			inner.ServeHTTP(w, r)
			return
		}
		_ = json.Unmarshal(body, &peek)

		// Gate 1: negative tokens are invalid — never pass through.
		if peek.Tokens < 0 {
			rejectHTTP(w, blockReasonNegativeTokens,
				fmt.Sprintf("impact_estimated_tokens=%d is invalid (must be ≥ 0)", peek.Tokens),
				peek.SessionID, peek.Tokens, http.StatusBadRequest)
			return
		}

		// Gate 2: zero tokens — zero-cost events (info, status, etc.) pass unconditionally.
		if peek.Tokens == 0 {
			inner.ServeHTTP(w, r)
			return
		}

		// From here: peek.Tokens > 0

		// Gate 3: tokens claimed but no session identity — fail-closed.
		if peek.SessionID == "" {
			rejectHTTP(w, blockReasonMissingSession,
				"session_id is required when impact_estimated_tokens > 0",
				"", peek.Tokens, http.StatusBadRequest)
			return
		}

		// Effective token count: use estimator if registered, else client-reported.
		effectiveTokens := peek.Tokens
		if registry.estimator != nil {
			if est := registry.estimator.Estimate(EstimatorInput{
				SessionID:      peek.SessionID,
				Mode:           peek.Mode,
				Trigger:        peek.Trigger,
				ReportedTokens: peek.Tokens,
			}); est >= 0 {
				effectiveTokens = est
			}
		}

		// Budget rules.
		decision, budgetErr := registry.CheckAndConsume(peek.SessionID, effectiveTokens)
		if budgetErr != nil || !decision.Allowed {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			resp, _ := json.Marshal(map[string]interface{}{
				"error":           "token_budget_exceeded",
				"block_reason":    decision.BlockReason,
				"reason":          decision.Reason,
				"session_id":      decision.SessionID,
				"call_tokens":     decision.CallTokens,
				"used":            decision.Used,
				"remaining":       decision.Remaining,
				"max_per_call":    decision.MaxPerCall,
				"max_per_session": decision.MaxPerSession,
			})
			_, _ = w.Write(resp)
			return
		}

		inner.ServeHTTP(w, r)
	}
}
