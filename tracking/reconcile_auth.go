// reconcile_auth.go — HMAC + timestamp + replay protection for /reconcile.
//
// The reconcile endpoint adjusts session budgets based on real model costs.
// Because it can release or increase budget, it must be restricted to
// trusted internal callers (the model execution layer, not external clients).
//
// Protection layers (all must pass; fail-closed):
//
//	[1] Timestamp gate  — X-Orbit-Timestamp must be within ±window (default 30s)
//	                      Prevents pre-signed request stockpiling.
//	[2] HMAC gate       — X-Orbit-Signature = HMAC-SHA256(secret, ts+"\n"+body)
//	                      Proves caller holds the shared secret.
//	[3] Replay gate     — signature nonce cache (TTL = window)
//	                      Prevents identical requests from being replayed within
//	                      the clock window even if both headers are valid.
//
// If ORBIT_RECONCILE_SECRET is not set:
//   - Non-production: auth is skipped (backward-compat dev mode, logged).
//   - Production:     fail at process startup (operator must set secret).
//
// Signing protocol (client side):
//
//	ts  = strconv.FormatInt(time.Now().Unix(), 10)
//	sig = HMAC-SHA256(secret, ts + "\n" + body)
//	# Set headers:
//	X-Orbit-Timestamp: <ts>
//	X-Orbit-Signature: <hex(sig)>
//
// Metrics:
//
//	orbit_reconcile_auth_rejected_total{reason} Counter
//	  reason: missing_timestamp | invalid_timestamp | expired_timestamp |
//	          missing_signature | invalid_signature | replay_detected
package tracking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// Rejection reason constants — metric labels and log fields
// ---------------------------------------------------------------------------

const (
	authReasonMissingTimestamp  = "missing_timestamp"
	authReasonInvalidTimestamp  = "invalid_timestamp"
	authReasonExpiredTimestamp  = "expired_timestamp"
	authReasonMissingSignature  = "missing_signature"
	authReasonInvalidSignature  = "invalid_signature"
	authReasonReplayDetected    = "replay_detected"
)

// defaultReconcileWindow is the maximum allowed age/skew for X-Orbit-Timestamp.
const defaultReconcileWindow = 30 * time.Second

// ---------------------------------------------------------------------------
// ReconcileAuth — HMAC middleware state
// ---------------------------------------------------------------------------

// ReconcileAuth enforces HMAC authentication, timestamp freshness, and replay
// protection for the /reconcile endpoint.
//
// Create with NewReconcileAuth. Safe for concurrent use.
type ReconcileAuth struct {
	secret []byte        // nil → auth disabled (non-production only)
	window time.Duration // timestamp tolerance window

	mu     sync.Mutex
	nonces map[string]time.Time // sig hex → expiry; cleaned on each request
}

// NewReconcileAuth creates a ReconcileAuth.
//
//   - secret: shared HMAC key. If nil/empty, auth is disabled (log warning).
//   - window: timestamp tolerance. Pass 0 to use the default (30s).
//
// Fail-closed: nil window → default. Empty secret in production → caller
// must log.Fatalf before reaching this function (see cmd/main.go).
func NewReconcileAuth(secret []byte, window time.Duration) *ReconcileAuth {
	if window <= 0 {
		window = defaultReconcileWindow
	}
	if len(secret) == 0 {
		log.Printf("[RECONCILE_AUTH] WARNING: no secret configured — HMAC auth DISABLED (set ORBIT_RECONCILE_SECRET)")
	} else {
		log.Printf("[RECONCILE_AUTH] HMAC auth ENABLED (key_length=%d window=%s)", len(secret), window)
	}
	return &ReconcileAuth{
		secret: secret,
		window: window,
		nonces: make(map[string]time.Time),
	}
}

// ---------------------------------------------------------------------------
// Middleware — wraps an inner http.Handler
// ---------------------------------------------------------------------------

// Middleware returns an http.HandlerFunc that validates HMAC auth before
// delegating to inner. On any failure → 401 Unauthorized; inner is NOT called.
//
// If the auth was constructed with no secret, the middleware passes all
// requests through (non-production dev mode).
func (a *ReconcileAuth) Middleware(inner http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// No secret → auth disabled; log and pass through.
		if len(a.secret) == 0 {
			inner.ServeHTTP(w, r)
			return
		}

		body, err := peekRequestBody(r)
		if err != nil {
			a.reject(w, r, authReasonInvalidSignature,
				fmt.Sprintf("failed to read request body: %v", err))
			return
		}

		// ── [1] Timestamp gate ────────────────────────────────────────────
		tsHeader := r.Header.Get("X-Orbit-Timestamp")
		if tsHeader == "" {
			a.reject(w, r, authReasonMissingTimestamp, "X-Orbit-Timestamp header is missing")
			return
		}
		tsUnix, err := strconv.ParseInt(tsHeader, 10, 64)
		if err != nil {
			a.reject(w, r, authReasonInvalidTimestamp,
				fmt.Sprintf("X-Orbit-Timestamp is not a valid Unix timestamp: %q", tsHeader))
			return
		}
		tsTime := time.Unix(tsUnix, 0)
		now := time.Now()
		delta := now.Sub(tsTime)
		if delta > a.window || delta < -a.window {
			a.reject(w, r, authReasonExpiredTimestamp,
				fmt.Sprintf("X-Orbit-Timestamp is outside the ±%s window (delta=%s)", a.window, delta.Round(time.Millisecond)))
			return
		}

		// ── [2] HMAC gate ─────────────────────────────────────────────────
		sigHeader := r.Header.Get("X-Orbit-Signature")
		if sigHeader == "" {
			a.reject(w, r, authReasonMissingSignature, "X-Orbit-Signature header is missing")
			return
		}
		sigBytes, err := hex.DecodeString(sigHeader)
		if err != nil {
			a.reject(w, r, authReasonInvalidSignature,
				"X-Orbit-Signature is not valid hex")
			return
		}
		expected := computeReconcileHMAC(a.secret, tsHeader, body)
		expectedBytes, _ := hex.DecodeString(expected)
		if !hmac.Equal(sigBytes, expectedBytes) {
			a.reject(w, r, authReasonInvalidSignature, "HMAC signature mismatch")
			return
		}

		// ── [3] Replay gate ───────────────────────────────────────────────
		if a.isReplay(sigHeader, tsTime) {
			a.reject(w, r, authReasonReplayDetected,
				"request signature has already been used (replay detected)")
			return
		}

		inner.ServeHTTP(w, r)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// computeReconcileHMAC returns HMAC-SHA256(secret, tsString+"\n"+body) as hex.
// The timestamp is bound into the MAC so that a replayed body with a new
// timestamp produces a different signature and is rejected by the HMAC gate
// before it can reach the replay cache.
func computeReconcileHMAC(secret []byte, tsString string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(tsString))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ComputeReconcileSignature is the exported helper for clients and tests that
// need to sign a /reconcile request.
//
//	ts  := strconv.FormatInt(time.Now().Unix(), 10)
//	sig := tracking.ComputeReconcileSignature(secret, ts, body)
func ComputeReconcileSignature(secret []byte, tsString string, body []byte) string {
	return computeReconcileHMAC(secret, tsString, body)
}

// isReplay returns true if sigHex was already seen in the current window.
// If new, it is recorded and false is returned.
// Expired entries are evicted on every call (amortised O(n) — window is small).
func (a *ReconcileAuth) isReplay(sigHex string, requestTime time.Time) bool {
	expiry := requestTime.Add(a.window)

	a.mu.Lock()
	defer a.mu.Unlock()

	// Evict expired nonces.
	now := time.Now()
	for k, exp := range a.nonces {
		if now.After(exp) {
			delete(a.nonces, k)
		}
	}

	if _, seen := a.nonces[sigHex]; seen {
		return true
	}
	a.nonces[sigHex] = expiry
	return false
}

// reject writes a 401 response, increments the metric, and emits a structured log.
// The HMAC secret is never included in the log output.
func (a *ReconcileAuth) reject(w http.ResponseWriter, r *http.Request, reason, detail string) {
	reconcileAuthRejected.WithLabelValues(reason).Inc()

	entry := map[string]interface{}{
		"event":   "reconcile_auth_rejected",
		"reason":  reason,
		"detail":  detail,
		"method":  r.Method,
		"remote":  r.RemoteAddr,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if b, err := json.Marshal(entry); err == nil {
		log.Printf("[RECONCILE_AUTH] %s", b)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	resp, _ := json.Marshal(map[string]string{
		"error":  "unauthorized",
		"reason": reason,
	})
	_, _ = w.Write(resp)
}

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var reconcileAuthRejected = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "orbit_reconcile_auth_rejected_total",
	Help: "Total /reconcile requests rejected by the auth middleware. Label: reason.",
}, []string{"reason"})

var reconcileAuthMetricsOnce sync.Once

// RegisterReconcileAuthMetrics registers the auth rejection counter.
// Call once at startup alongside RegisterTokenReconcileMetrics.
func RegisterReconcileAuthMetrics(reg prometheus.Registerer) {
	reconcileAuthMetricsOnce.Do(func() {
		reg.MustRegister(reconcileAuthRejected)
	})
}

// ReconcileAuthMetricNames is the closed set of metric names owned by this file.
var ReconcileAuthMetricNames = []string{
	"orbit_reconcile_auth_rejected_total",
}
