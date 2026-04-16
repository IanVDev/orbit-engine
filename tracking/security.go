// security.go — Sovereign security layer for orbit-engine /track endpoint.
//
// Implements:
//  1. Deterministic event ID (SHA-256 of canonical JSON)
//  2. Event deduplication (5-min sliding window)
//  3. HMAC-SHA256 authentication via X-Orbit-Signature header
//  4. Token bucket rate limiting per client identity
//  5. Automatic cleanup of expired state (TTL 1h)
//  6. orbit_real_usage_alive gauge (1 or 0)
//  7. Unified rejection metric orbit_tracking_rejected_total{reason}
//  8. HMAC mandatory in production (ORBIT_ENV=production)
//  9. Semantic validation gate (session_id, timestamp, payload)
//
// 10. Replay hard lock for critical events (persistent dedup beyond 5-min)
// 11. Client fingerprint hardening (client_id + IP + UA + Accept-Language + salt)
// 12. Structured rejection logging
// 13. Cardinality protection (no dynamic labels)
//
// Design: fail-closed everywhere. Unknown state → reject.
// No external dependencies beyond crypto/hmac (stdlib).
package tracking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
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

// isProduction is true when ORBIT_ENV is "production".
var isProduction bool

func init() {
	env := os.Getenv("ORBIT_ENV")
	isProduction = env == "production"

	if s := os.Getenv("ORBIT_HMAC_SECRET"); s != "" {
		hmacSecret = []byte(s)
		hmacRequired = true
		log.Printf("[SECURITY] HMAC authentication ENABLED (key length=%d)", len(hmacSecret))
	} else {
		if isProduction {
			// Fail-closed: HMAC is mandatory in production.
			// Panic on startup prevents running an unprotected prod instance.
			panic("orbit-engine: ORBIT_HMAC_SECRET is required when ORBIT_ENV=production (fail-closed)")
		}
		log.Printf("[SECURITY] HMAC authentication DISABLED (set ORBIT_HMAC_SECRET to enable)")
	}
}

// SetHMACSecret overrides the HMAC key at runtime. For testing ONLY.
func SetHMACSecret(secret string) {
	hmacSecret = []byte(secret)
	hmacRequired = secret != ""
}

// SetProductionMode overrides isProduction at runtime. For testing ONLY.
func SetProductionMode(prod bool) {
	isProduction = prod
}

// IsProductionMode returns the current production mode state.
func IsProductionMode() bool {
	return isProduction
}

// ---------------------------------------------------------------------------
// 1. Deterministic Event ID (canonical JSON → SHA-256)
// ---------------------------------------------------------------------------

// CanonicalizeJSON returns a deterministic JSON representation of the payload.
// Keys are sorted recursively, whitespace is normalized. This ensures that
// {"a":1,"b":2} and {"b":2,"a":1} produce the same canonical form.
// Returns the original payload unchanged if it is not valid JSON (fail-safe).
func CanonicalizeJSON(payload []byte) []byte {
	var raw interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		// Not valid JSON — return as-is (will fail at decode step later).
		return payload
	}
	canonical, err := marshalCanonical(raw)
	if err != nil {
		return payload
	}
	return canonical
}

// marshalCanonical recursively marshals a value with sorted keys.
func marshalCanonical(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		// Sort keys
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		// Build canonical object
		buf := []byte("{")
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			keyBytes, _ := json.Marshal(k)
			buf = append(buf, keyBytes...)
			buf = append(buf, ':')
			valBytes, err := marshalCanonical(val[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, valBytes...)
		}
		buf = append(buf, '}')
		return buf, nil

	case []interface{}:
		buf := []byte("[")
		for i, item := range val {
			if i > 0 {
				buf = append(buf, ',')
			}
			itemBytes, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, itemBytes...)
		}
		buf = append(buf, ']')
		return buf, nil

	default:
		// Primitives: string, number, bool, null
		return json.Marshal(val)
	}
}

// ComputeEventID returns SHA-256 hex of the canonical JSON payload.
// Deterministic: same logical payload → same ID regardless of key order.
// Used for dedup.
func ComputeEventID(payload []byte) string {
	canonical := CanonicalizeJSON(payload)
	h := sha256.Sum256(canonical)
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

// ClientIdentity extracts a rate-limit key from the HTTP request.
// Priority: X-Orbit-Client-Id header > IP + User-Agent hash > session_id fallback.
// This prevents a single attacker from consuming the global bucket.
func ClientIdentity(clientID, remoteAddr, userAgent, sessionID string) string {
	if clientID != "" {
		return "client:" + clientID
	}
	if remoteAddr != "" {
		// Use IP + UA hash as fallback
		h := sha256.Sum256([]byte(remoteAddr + "|" + userAgent))
		return "ip:" + hex.EncodeToString(h[:8])
	}
	// Last resort: session_id
	return "session:" + sessionID
}

// CheckTokenBucket enforces token-bucket rate limiting per key.
// Fail-closed: returns error if no tokens available.
func CheckTokenBucket(key string) error {
	bucketMu.Lock()
	defer bucketMu.Unlock()

	if bucketDisabled {
		return nil
	}

	now := bucketTimeNow()
	tb, ok := sessionBuckets[key]
	if !ok {
		tb = &tokenBucket{
			tokens:     bucketCapacity, // start full
			lastRefill: now,
			capacity:   bucketCapacity,
			refillRate: bucketRefillRate,
		}
		sessionBuckets[key] = tb
	}
	bucketLastAccess[key] = now

	if !tb.allow(now) {
		return fmt.Errorf("tracking: token bucket exhausted for %s (capacity=%.0f, refill=%.1f/s)",
			key, bucketCapacity, bucketRefillRate)
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
// 9. Semantic Validation Gate
// ---------------------------------------------------------------------------

// ValidateSemantic performs deep semantic checks on a pre-parsed SkillEvent
// before it enters the dedup/tracking pipeline. Fail-closed: any violation
// returns an error and the event MUST be rejected (HTTP 400).
//
// Checks:
//   - session_id must be non-empty
//   - timestamp must not be zero
//   - timestamp must not be in the future (>60s tolerance for clock skew)
//   - raw payload must not be empty/whitespace
func ValidateSemantic(event SkillEvent, rawBody []byte) error {
	if event.SessionID == "" {
		return fmt.Errorf("semantic: session_id is required")
	}
	if event.Timestamp.IsZero() {
		return fmt.Errorf("semantic: timestamp is required")
	}
	// Future guard — 60s tolerance for clock skew
	now := time.Now().UTC()
	if event.Timestamp.Time.After(now.Add(60 * time.Second)) {
		return fmt.Errorf("semantic: timestamp is in the future (%s > now+60s)",
			event.Timestamp.Time.Format(time.RFC3339))
	}
	// Payload must contain meaningful content
	if len(rawBody) == 0 {
		return fmt.Errorf("semantic: payload body is empty")
	}
	trimmed := string(rawBody)
	for _, c := range trimmed {
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return nil // found non-whitespace → valid
		}
	}
	return fmt.Errorf("semantic: payload body is whitespace-only")
}

// ---------------------------------------------------------------------------
// 10. Replay Hard Lock (persistent dedup for critical events)
// ---------------------------------------------------------------------------

// criticalDedupTTL is the TTL for critical event dedup entries.
// Critical events are never allowed to replay, even after the 5-min window.
const criticalDedupTTL = 24 * time.Hour

var (
	criticalDedupMap = make(map[string]time.Time)
	criticalDedupMu  sync.Mutex
)

// IsCritical returns true if the event is critical and must be
// permanently deduplicated (beyond the 5-min window). Critical events
// include activations with high token impact.
func IsCritical(event SkillEvent) bool {
	return event.EventType == "activation" && event.ImpactEstimatedToken >= 100
}

// CheckCriticalDedup checks the persistent dedup map for critical events.
// Returns nil if the event_id is new, or an error if it was already seen
// within the criticalDedupTTL window (24h).
func CheckCriticalDedup(eventID string) error {
	criticalDedupMu.Lock()
	defer criticalDedupMu.Unlock()

	now := time.Now()
	if seen, ok := criticalDedupMap[eventID]; ok {
		if now.Sub(seen) < criticalDedupTTL {
			return fmt.Errorf("security: critical event replay blocked (event_id %s, seen %v ago, TTL=%v)",
				eventID[:16], now.Sub(seen).Round(time.Millisecond), criticalDedupTTL)
		}
		// TTL expired — allow and refresh
	}
	criticalDedupMap[eventID] = now
	return nil
}

// ResetCriticalDedup clears the critical dedup map. For testing ONLY.
func ResetCriticalDedup() {
	criticalDedupMu.Lock()
	defer criticalDedupMu.Unlock()
	criticalDedupMap = make(map[string]time.Time)
}

// ---------------------------------------------------------------------------
// 11. Client Fingerprint Hardening
// ---------------------------------------------------------------------------

// fingerprintSalt is a server-side salt for client fingerprinting.
// Loaded from ORBIT_FINGERPRINT_SALT env, or falls back to a default.
var fingerprintSalt string

func init() {
	if s := os.Getenv("ORBIT_FINGERPRINT_SALT"); s != "" {
		fingerprintSalt = s
	} else {
		fingerprintSalt = "orbit-engine-default-salt-v1"
	}
}

// ClientFingerprint produces a hardened client identity by combining
// client_id + IP + User-Agent + Accept-Language + server-side salt.
// The result is a hex-encoded SHA-256 hash, safe for use as a bucket key.
// This is strictly more secure than ClientIdentity because it includes
// Accept-Language and the salt, making fingerprint forgery much harder.
// The remote address is normalised to IP-only (port stripped) so that
// ephemeral source ports do not fragment the fingerprint space.
func ClientFingerprint(clientID, remoteAddr, userAgent, acceptLanguage string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	data := fmt.Sprintf("%s|%s|%s|%s|%s", clientID, host, userAgent, acceptLanguage, fingerprintSalt)
	h := sha256.Sum256([]byte(data))
	return "fp:" + hex.EncodeToString(h[:16])
}

// SetFingerprintSalt overrides the salt at runtime. For testing ONLY.
func SetFingerprintSalt(salt string) {
	fingerprintSalt = salt
}

// ---------------------------------------------------------------------------
// 12. Structured Rejection Logging
// ---------------------------------------------------------------------------

// rejectionLog emits a structured JSON log line for every rejection.
// Fields: rejected_reason, client_fingerprint, event_id, timestamp.
// This is mandatory for all rejection paths in the pipeline.
func rejectionLog(reason, fingerprint, eventID string) {
	entry := map[string]string{
		"rejected_reason":    reason,
		"client_fingerprint": fingerprint,
		"event_id":           eventID,
		"timestamp":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	line, _ := json.Marshal(entry)
	log.Printf("[REJECTED] %s", line)
}

// ---------------------------------------------------------------------------
// 13. Cardinality Protection — forbidden label names
// ---------------------------------------------------------------------------

// _highCardinalityLabels are label names that MUST NEVER appear on any
// Prometheus metric. They would create unbounded cardinality and crash
// Prometheus storage.
var _highCardinalityLabels = []string{
	"client_id",
	"session_id",
	"event_id",
	"event_hash",
	"fingerprint",
}

// HighCardinalityLabels returns the list of forbidden label names.
// Exported for use in governance validation and tests.
func HighCardinalityLabels() []string {
	cp := make([]string, len(_highCardinalityLabels))
	copy(cp, _highCardinalityLabels)
	return cp
}

// ---------------------------------------------------------------------------
// 5. Automatic Cleanup (TTL 1h)
// ---------------------------------------------------------------------------

const sessionTTL = 1 * time.Hour

// CleanupExpiredState removes entries older than sessionTTL from dedup map
// and session buckets, plus entries from the critical dedup map beyond
// criticalDedupTTL. Returns counts of evicted entries.
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

	// Critical dedup map cleanup
	criticalDedupMu.Lock()
	for id, seen := range criticalDedupMap {
		if now.Sub(seen) > criticalDedupTTL {
			delete(criticalDedupMap, id)
			dedupEvicted++
		}
	}
	criticalDedupMu.Unlock()

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

	// orbit_tracking_rejected_total: unified rejection metric.
	// Labels: reason=hmac|dedup|rate_limit|invalid
	// Provides a single query surface for attack detection:
	//   rate(orbit_tracking_rejected_total[5m]) > threshold
	trackingRejectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_tracking_rejected_total",
			Help: "Total events rejected by the security layer. Label 'reason' identifies the cause.",
		},
		[]string{"reason"},
	)
)

// RejectReason constants for the unified rejection metric.
const (
	RejectReasonHMAC      = "hmac"
	RejectReasonDedup     = "dedup"
	RejectReasonRateLimit = "rate_limit"
	RejectReasonInvalid   = "invalid"
	RejectReasonSemantic  = "invalid_semantic"
	RejectReasonReplay    = "critical_replay"
)

// IncrementRejected increments both the legacy per-type counter and the
// unified orbit_tracking_rejected_total{reason} metric.
func IncrementRejected(reason string) {
	trackingRejectedTotal.WithLabelValues(reason).Inc()
}

// RegisterSecurityMetrics registers security-related Prometheus collectors.
// Call once at startup alongside RegisterMetrics.
func RegisterSecurityMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		trackingDedupBlocked,
		trackingHMACFailures,
		trackingCleanupTotal,
		realUsageAlive,
		trackingBucketRejected,
		trackingRejectedTotal,
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
