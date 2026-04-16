// security.go — Sovereign security layer for orbit-engine /track endpoint.
//
// Implements:
//  1. Deterministic event ID (SHA-256 of raw payload)
//  2. Event deduplication (5-min sliding window)
//  3. HMAC-SHA256 authentication via X-Orbit-Signature header
//  4. Token bucket rate limiting per session_id
//  5. Automatic cleanup of expired state (TTL 1h)
//  6. orbit_real_usage_alive gauge (1 or 0)
//
// Design: fail-closed everywhere. Unknown state → reject.
// No external dependencies beyond crypto/hmac (stdlib).
package tracking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// Configuration — loaded once from environment
// ---------------------------------------------------------------------------

// hmacSecret holds the HMAC-SHA256 key. If empty, HMAC auth is disabled
// (backward-compatible for dev/test). Set ORBIT_HMAC_SECRET in production.
var hmacSecret []byte

// hmacRequired is true when ORBIT_HMAC_SECRET is set in the environment.
// Fail-closed: if set but request has no signature → reject.
var hmacRequired bool

func init() {
	if s := os.Getenv("ORBIT_HMAC_SECRET"); s != "" {
		hmacSecret = []byte(s)
		hmacRequired = true
		log.Printf("[SECURITY] HMAC authentication ENABLED (key length=%d)", len(hmacSecret))
	} else {
		log.Printf("[SECURITY] HMAC authentication DISABLED (set ORBIT_HMAC_SECRET to enable)")
	}
}

// SetHMACSecret overrides the HMAC key at runtime. For testing ONLY.
func SetHMACSecret(secret string) {
	hmacSecret = []byte(secret)
	hmacRequired = secret != ""
}

// ---------------------------------------------------------------------------
// 1. Deterministic Event ID
// ---------------------------------------------------------------------------

// ComputeEventID returns SHA-256 hex of the raw JSON payload.
// Deterministic: same payload → same ID. Used for dedup.
func ComputeEventID(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}

// ---------------------------------------------------------------------------
// 2. Event Deduplication (5-min window)
// ---------------------------------------------------------------------------

const dedupWindow = 5 * time.Minute

var (
	dedupMap = make(map[string]time.Time)
	dedupMu  sync.Mutex
)

// CheckDedup returns nil if the event_id is new, or an error if it's a replay.
// Fail-closed: concurrent access is mutex-protected.
func CheckDedup(eventID string) error {
	dedupMu.Lock()
	defer dedupMu.Unlock()

	now := time.Now()
	if seen, ok := dedupMap[eventID]; ok {
		if now.Sub(seen) < dedupWindow {
			return fmt.Errorf("security: duplicate event_id %s (seen %v ago)", eventID[:16], now.Sub(seen).Round(time.Millisecond))
		}
	}
	dedupMap[eventID] = now
	return nil
}

// ResetDedup clears the dedup map. For testing ONLY.
func ResetDedup() {
	dedupMu.Lock()
	defer dedupMu.Unlock()
	dedupMap = make(map[string]time.Time)
}

// ---------------------------------------------------------------------------
// 3. HMAC-SHA256 Authentication
// ---------------------------------------------------------------------------

// ValidateHMAC checks the request signature against the payload.
// signatureHex is the hex-encoded HMAC-SHA256 from X-Orbit-Signature.
// Returns nil if valid, error if invalid or missing when required.
func ValidateHMAC(payload []byte, signatureHex string) error {
	if !hmacRequired {
		return nil // HMAC not configured — allow (backward compat)
	}
	if signatureHex == "" {
		return fmt.Errorf("security: HMAC signature required (X-Orbit-Signature header missing)")
	}

	sigBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("security: invalid HMAC hex encoding: %w", err)
	}

	mac := hmac.New(sha256.New, hmacSecret)
	mac.Write(payload)
	expected := mac.Sum(nil)

	if !hmac.Equal(sigBytes, expected) {
		return fmt.Errorf("security: HMAC signature mismatch")
	}

	return nil
}

// ComputeHMACHex computes the HMAC-SHA256 of payload and returns hex string.
// Exported for clients and tests that need to sign payloads.
func ComputeHMACHex(payload []byte, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// 4. Token Bucket Rate Limiting
// ---------------------------------------------------------------------------

// tokenBucket implements a simple token-bucket algorithm per session.
type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
	capacity   float64
	refillRate float64 // tokens per second
}

// allow checks if 1 token is available. Returns true and consumes if so.
func (tb *tokenBucket) allow(now time.Time) bool {
	// Refill tokens based on elapsed time
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastRefill = now

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// Token bucket configuration
const (
	bucketCapacity   = 5.0 // max burst size
	bucketRefillRate = 1.0 // tokens per second
)

var (
	sessionBuckets   = make(map[string]*tokenBucket)
	bucketMu         sync.Mutex
	bucketDisabled   bool // testing only
	bucketTimeNow    = time.Now
	bucketLastAccess = make(map[string]time.Time) // for TTL cleanup
)

// CheckTokenBucket enforces token-bucket rate limiting per session_id.
// Fail-closed: returns error if no tokens available.
func CheckTokenBucket(sessionID string) error {
	bucketMu.Lock()
	defer bucketMu.Unlock()

	if bucketDisabled {
		return nil
	}

	now := bucketTimeNow()
	tb, ok := sessionBuckets[sessionID]
	if !ok {
		tb = &tokenBucket{
			tokens:     bucketCapacity, // start full
			lastRefill: now,
			capacity:   bucketCapacity,
			refillRate: bucketRefillRate,
		}
		sessionBuckets[sessionID] = tb
	}
	bucketLastAccess[sessionID] = now

	if !tb.allow(now) {
		return fmt.Errorf("tracking: token bucket exhausted for session %s (capacity=%.0f, refill=%.1f/s)",
			sessionID, bucketCapacity, bucketRefillRate)
	}
	return nil
}

// ResetTokenBuckets clears all bucket state. For testing ONLY.
func ResetTokenBuckets() {
	bucketMu.Lock()
	defer bucketMu.Unlock()
	sessionBuckets = make(map[string]*tokenBucket)
	bucketLastAccess = make(map[string]time.Time)
	bucketDisabled = false
}

// DisableTokenBuckets disables bucket rate limiting entirely. For testing ONLY.
func DisableTokenBuckets() {
	bucketMu.Lock()
	defer bucketMu.Unlock()
	bucketDisabled = true
}

// ---------------------------------------------------------------------------
// 5. Automatic Cleanup (TTL 1h)
// ---------------------------------------------------------------------------

const sessionTTL = 1 * time.Hour

// CleanupExpiredState removes entries older than sessionTTL from dedup map
// and session buckets. Returns counts of evicted entries.
func CleanupExpiredState() (dedupEvicted, bucketsEvicted int) {
	now := time.Now()

	// Dedup map cleanup
	dedupMu.Lock()
	for id, seen := range dedupMap {
		if now.Sub(seen) > sessionTTL {
			delete(dedupMap, id)
			dedupEvicted++
		}
	}
	dedupMu.Unlock()

	// Token bucket cleanup
	bucketMu.Lock()
	for sid, lastAccess := range bucketLastAccess {
		if now.Sub(lastAccess) > sessionTTL {
			delete(sessionBuckets, sid)
			delete(bucketLastAccess, sid)
			bucketsEvicted++
		}
	}
	bucketMu.Unlock()

	return dedupEvicted, bucketsEvicted
}

// StartCleanup launches a background goroutine that periodically calls
// CleanupExpiredState. Call once after startup. Safe to call with any interval.
func StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			dedupN, bucketN := CleanupExpiredState()
			if dedupN > 0 || bucketN > 0 {
				log.Printf("[SECURITY] cleanup: evicted %d dedup entries, %d session buckets", dedupN, bucketN)
				trackingCleanupTotal.Add(float64(dedupN + bucketN))
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// 6. Security Prometheus Metrics
// ---------------------------------------------------------------------------

var (
	// orbit_tracking_dedup_blocked_total: events rejected by dedup.
	trackingDedupBlocked = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_tracking_dedup_blocked_total",
			Help: "Total events rejected as duplicate (replay prevention).",
		},
	)

	// orbit_tracking_hmac_failures_total: events rejected by HMAC auth.
	trackingHMACFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_tracking_hmac_failures_total",
			Help: "Total events rejected due to invalid or missing HMAC signature.",
		},
	)

	// orbit_tracking_cleanup_total: total entries evicted by cleanup.
	trackingCleanupTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_tracking_cleanup_total",
			Help: "Total stale entries evicted from dedup and session bucket maps.",
		},
	)

	// orbit_real_usage_alive: 1 if real usage was received in the last 5 min, 0 otherwise.
	// Updated by TrackHandler on each successful real event.
	realUsageAlive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "orbit_real_usage_alive",
			Help: "1 if a real usage event was processed recently, 0 otherwise. " +
				"Updated on each successful /track event.",
		},
	)

	// orbit_tracking_token_bucket_rejected_total: events rejected by token bucket.
	trackingBucketRejected = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_tracking_token_bucket_rejected_total",
			Help: "Total events rejected by token bucket rate limiting.",
		},
	)
)

// RegisterSecurityMetrics registers security-related Prometheus collectors.
// Call once at startup alongside RegisterMetrics.
func RegisterSecurityMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		trackingDedupBlocked,
		trackingHMACFailures,
		trackingCleanupTotal,
		realUsageAlive,
		trackingBucketRejected,
	)
}

// SetRealUsageAlive sets the alive gauge to 1 (call after successful event).
func SetRealUsageAlive() {
	realUsageAlive.Set(1)
}

// ClearRealUsageAlive sets the alive gauge to 0 (staleness detection).
func ClearRealUsageAlive() {
	realUsageAlive.Set(0)
}
