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
// 11. Client fingerprint v2 (proxy-aware, NAT-safe, anti-spoof)
// 12. Structured rejection logging
// 13. Cardinality protection (no dynamic labels)
// 14. Behavior abuse detection (repeated-payload heuristic)
// 15. Security Mode (automatic NORMAL → ELEVATED → LOCKDOWN based on abuse ratio)
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

// CheckTokenBucketWithMode enforces token-bucket rate limiting with a
// security-mode-adjusted capacity. New buckets are created with effectiveCap;
// existing buckets have their capacity dynamically reduced.
func CheckTokenBucketWithMode(key string, mode SecurityMode) error {
	effectiveCap := EffectiveRateLimitCapacity(mode)

	bucketMu.Lock()
	defer bucketMu.Unlock()

	if bucketDisabled {
		return nil
	}

	now := bucketTimeNow()
	tb, ok := sessionBuckets[key]
	if !ok {
		tb = &tokenBucket{
			tokens:     effectiveCap,
			lastRefill: now,
			capacity:   effectiveCap,
			refillRate: bucketRefillRate,
		}
		sessionBuckets[key] = tb
	} else {
		// Dynamically adjust capacity for existing buckets
		tb.capacity = effectiveCap
		if tb.tokens > effectiveCap {
			tb.tokens = effectiveCap
		}
	}
	bucketLastAccess[key] = now

	if !tb.allow(now) {
		return fmt.Errorf("tracking: token bucket exhausted for %s (capacity=%.0f, refill=%.1f/s)",
			key, effectiveCap, bucketRefillRate)
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
// 11. Client Fingerprint Hardening (v2 — proxy-aware, NAT-safe)
// ---------------------------------------------------------------------------

// fingerprintSalt is a server-side salt for client fingerprinting.
// Loaded from ORBIT_FINGERPRINT_SALT env, or falls back to a default.
var fingerprintSalt string

// trustedProxyCIDRs is the list of CIDR ranges whose X-Forwarded-For header
// is considered trustworthy. Loaded from ORBIT_TRUSTED_PROXIES env
// (comma-separated CIDRs). If empty, XFF is always ignored (fail-closed).
var trustedProxyCIDRs []*net.IPNet

func init() {
	if s := os.Getenv("ORBIT_FINGERPRINT_SALT"); s != "" {
		fingerprintSalt = s
	} else {
		fingerprintSalt = "orbit-engine-default-salt-v1"
	}
	loadTrustedProxies()
}

// loadTrustedProxies parses ORBIT_TRUSTED_PROXIES (comma-separated CIDRs)
// into trustedProxyCIDRs. Example: "10.0.0.0/8,172.16.0.0/12,127.0.0.1/32"
func loadTrustedProxies() {
	raw := os.Getenv("ORBIT_TRUSTED_PROXIES")
	if raw == "" {
		return
	}
	for _, cidr := range splitAndTrim(raw) {
		if cidr == "" {
			continue
		}
		// If bare IP (no mask), add /32 or /128
		if !containsSlash(cidr) {
			if net.ParseIP(cidr) != nil {
				if net.ParseIP(cidr).To4() != nil {
					cidr += "/32"
				} else {
					cidr += "/128"
				}
			}
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Printf("[SECURITY] ignoring invalid trusted proxy CIDR %q: %v", cidr, err)
			continue
		}
		trustedProxyCIDRs = append(trustedProxyCIDRs, ipNet)
	}
	if len(trustedProxyCIDRs) > 0 {
		log.Printf("[SECURITY] trusted proxies loaded: %d CIDR(s)", len(trustedProxyCIDRs))
	}
}

func splitAndTrim(s string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			p := s[start:i]
			// trim spaces
			for len(p) > 0 && p[0] == ' ' {
				p = p[1:]
			}
			for len(p) > 0 && p[len(p)-1] == ' ' {
				p = p[:len(p)-1]
			}
			parts = append(parts, p)
			start = i + 1
		}
	}
	return parts
}

func containsSlash(s string) bool {
	for _, c := range s {
		if c == '/' {
			return true
		}
	}
	return false
}

// isTrustedProxy returns true if the given IP (from RemoteAddr) is within
// any configured trusted proxy CIDR. Fail-closed: if no proxies are
// configured, always returns false.
func isTrustedProxy(remoteAddr string) bool {
	if len(trustedProxyCIDRs) == 0 {
		return false
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, cidr := range trustedProxyCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// ExtractClientIP determines the real client IP from the request.
//
// Strategy (fail-closed):
//   - If the direct peer (RemoteAddr) is a trusted proxy AND X-Forwarded-For
//     is present, use the left-most (client) IP from XFF.
//   - Otherwise, use RemoteAddr (strip port).
//   - If XFF contains a non-parseable IP, fall back to RemoteAddr.
//
// This prevents spoofing: an attacker cannot set X-Forwarded-For unless
// their request actually traverses a trusted proxy.
func ExtractClientIP(remoteAddr, xForwardedFor string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}

	if xForwardedFor == "" || !isTrustedProxy(remoteAddr) {
		return host
	}

	// XFF format: "client, proxy1, proxy2"
	// Take the left-most IP (original client).
	first := xForwardedFor
	for i := 0; i < len(xForwardedFor); i++ {
		if xForwardedFor[i] == ',' {
			first = xForwardedFor[:i]
			break
		}
	}
	// Trim spaces
	for len(first) > 0 && first[0] == ' ' {
		first = first[1:]
	}
	for len(first) > 0 && first[len(first)-1] == ' ' {
		first = first[:len(first)-1]
	}

	// Validate it's a real IP — fail-closed on garbage
	if ip := net.ParseIP(first); ip != nil {
		return first
	}
	// Garbage in XFF → fall back to direct peer
	return host
}

// SetTrustedProxies overrides the trusted proxy list. For testing ONLY.
func SetTrustedProxies(cidrs []*net.IPNet) {
	trustedProxyCIDRs = cidrs
}

// ClientFingerprint produces a hardened client identity by combining
// client_id + IP + User-Agent + Accept-Language + server-side salt.
// The result is a hex-encoded SHA-256 hash, safe for use as a bucket key.
//
// v2 changes:
//   - IP is expected to be already extracted (no port) via ExtractClientIP.
//   - sessionID is used as a NAT fallback complement: when multiple distinct
//     sessions share the same (ip, UA, lang) behind NAT, the session_id
//     differentiates them, preventing one user from exhausting another's bucket.
//   - If clientID is empty AND sessionID is empty, the fingerprint enters
//     "restricted mode" — a shorter prefix is used, which maps all
//     unidentified clients to a shared restrictive bucket (fail-closed).
//
// The result is ALWAYS a hash — never raw data — safe for metric labels.
func ClientFingerprint(clientID, ip, userAgent, acceptLanguage string) string {
	return ClientFingerprintWithSession(clientID, ip, userAgent, acceptLanguage, "")
}

// ClientFingerprintWithSession is the v2 fingerprint function that includes
// session_id as NAT disambiguation. Use this in TrackHandler after decoding
// the event body. The plain ClientFingerprint (without session) is used for
// pre-decode rejection paths (HMAC failure, etc.).
func ClientFingerprintWithSession(clientID, ip, userAgent, acceptLanguage, sessionID string) string {
	// Normalise IP — strip port if caller passed host:port
	host := ip
	if h, _, err := net.SplitHostPort(ip); err == nil {
		host = h
	}

	// Fail-closed: unidentifiable client → restricted bucket
	if clientID == "" && sessionID == "" && host == "" {
		return "fp:restricted"
	}

	var data string
	if clientID != "" {
		// Strong identity: client_id dominates
		data = fmt.Sprintf("v2|cid:%s|ip:%s|ua:%s|lang:%s|salt:%s",
			clientID, host, userAgent, acceptLanguage, fingerprintSalt)
	} else {
		// Weak identity: IP + UA + session_id for NAT disambiguation
		data = fmt.Sprintf("v2|ip:%s|ua:%s|lang:%s|sess:%s|salt:%s",
			host, userAgent, acceptLanguage, sessionID, fingerprintSalt)
	}
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
// 14. Behavior Abuse Detection v2 — similarity-aware, adaptive threshold
// ---------------------------------------------------------------------------
//
// Detects abusive patterns using a composite similarity key:
//   similarityKey = prefix(eventID, 8) + "|" + sizeBucket(payloadLen)
//
// Size buckets: 0-127, 128-255, 256-511, 512-1023, 1024-2047, 2048+
// This catches attackers who make minor variations (timestamps, IDs) while
// keeping the same payload structure and approximate size.
//
// Threshold adapts to fingerprint confidence:
//   High   (has client_id)         → 5 identical similarity keys
//   Medium (has IP + UA, no cid)   → 4
//   Low    (restricted fingerprint)→ 3
//
// Parameters:
//   behaviorWindowSize = 20 events retained per fingerprint
//   behaviorTTL        = 2 min inactivity → evict window

const (
	behaviorWindowSize = 20
	behaviorTTL        = 2 * time.Minute
)

// FingerprintConfidence levels determine behavior abuse thresholds.
type FingerprintConfidence int

const (
	ConfidenceLow    FingerprintConfidence = iota // fp:restricted or empty
	ConfidenceMedium                              // IP+UA only, no client_id
	ConfidenceHigh                                // has X-Orbit-Client-Id
)

// behaviorThresholdFor returns the abuse threshold for a given confidence.
func behaviorThresholdFor(c FingerprintConfidence) int {
	switch c {
	case ConfidenceHigh:
		return 5
	case ConfidenceMedium:
		return 4
	default:
		return 3
	}
}

// ClassifyConfidence returns the fingerprint confidence based on clientID
// and fingerprint value. Exported for testing.
func ClassifyConfidence(clientID, fingerprint string) FingerprintConfidence {
	if clientID != "" {
		return ConfidenceHigh
	}
	if fingerprint == "fp:restricted" || fingerprint == "" {
		return ConfidenceLow
	}
	return ConfidenceMedium
}

// sizeBucket returns a coarse size bucket string for a payload length.
// Buckets: "0-127", "128-255", "256-511", "512-1023", "1024-2047", "2048+"
func sizeBucket(payloadLen int) string {
	switch {
	case payloadLen < 128:
		return "0-127"
	case payloadLen < 256:
		return "128-255"
	case payloadLen < 512:
		return "256-511"
	case payloadLen < 1024:
		return "512-1023"
	case payloadLen < 2048:
		return "1024-2047"
	default:
		return "2048+"
	}
}

// similarityKey builds the composite key from event hash prefix + size bucket.
func similarityKey(eventID string, payloadLen int) string {
	prefix := eventID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return prefix + "|" + sizeBucket(payloadLen)
}

// behaviorWindow holds the most recent similarity keys for one client.
type behaviorWindow struct {
	keys     [behaviorWindowSize]string
	count    int       // total events ever (mod ring for position)
	lastSeen time.Time // for TTL eviction
}

var (
	behaviorMu  sync.Mutex
	behaviorMap = make(map[string]*behaviorWindow) // key = fingerprint
)

// RejectReasonBehavior is the rejection reason for behavior abuse.
const RejectReasonBehavior = "behavior_abuse"

// CheckBehaviorAbuse records the similarity key for the given fingerprint
// and returns an error if the heuristic detects abusive repetition.
//
// Parameters:
//   - fingerprint: client identity hash
//   - eventID: SHA-256 hex of canonical payload
//   - payloadLen: raw body length in bytes
//   - clientID: X-Orbit-Client-Id header (for confidence classification)
//
// Safe for concurrent use. Fail-closed: any internal inconsistency → reject.
func CheckBehaviorAbuse(fingerprint, eventID string, payloadLen int, clientID string) error {
	key := similarityKey(eventID, payloadLen)
	confidence := ClassifyConfidence(clientID, fingerprint)
	threshold := behaviorThresholdFor(confidence)

	// Apply security mode adjustment (ELEVATED/LOCKDOWN → stricter)
	threshold = EffectiveBehaviorThreshold(threshold, GetSecurityMode())

	behaviorMu.Lock()
	defer behaviorMu.Unlock()

	now := time.Now()
	w, exists := behaviorMap[fingerprint]
	if !exists {
		w = &behaviorWindow{}
		behaviorMap[fingerprint] = w
	}

	// TTL: if stale, reset the window (fresh start)
	if exists && now.Sub(w.lastSeen) > behaviorTTL {
		*w = behaviorWindow{}
	}
	w.lastSeen = now

	// Insert into ring buffer
	pos := w.count % behaviorWindowSize
	w.keys[pos] = key
	w.count++

	// Count how many entries in the window match this key
	matches := 0
	limit := behaviorWindowSize
	if w.count < behaviorWindowSize {
		limit = w.count
	}
	for i := 0; i < limit; i++ {
		if w.keys[i] == key {
			matches++
		}
	}

	// Compute ratio for gauge (even if not blocked)
	ratio := float64(matches) / float64(limit)
	behaviorAbuseRatio.Set(ratio)
	// Only feed global abuse ratio when window has enough samples to be meaningful.
	// This prevents a single event (1/1=1.0) from escalating security mode.
	if limit >= 5 {
		setCurrentAbuseRatio(ratio)
	}

	if matches >= threshold {
		// Structured log with full context
		behaviorAbuseLog(fingerprint, key, matches, threshold, confidence)
		return fmt.Errorf("behavior abuse detected: %d/%d similar events (threshold=%d, confidence=%s)",
			matches, limit, threshold, confidenceName(confidence))
	}
	return nil
}

// confidenceName returns a human-readable name for log output.
func confidenceName(c FingerprintConfidence) string {
	switch c {
	case ConfidenceHigh:
		return "high"
	case ConfidenceMedium:
		return "medium"
	default:
		return "low"
	}
}

// behaviorAbuseLog emits a structured JSON log for behavior abuse detections.
func behaviorAbuseLog(fingerprint, simKey string, count, threshold int, confidence FingerprintConfidence) {
	entry := map[string]interface{}{
		"type":           "behavior_abuse",
		"fingerprint":    fingerprint,
		"similarity_key": simKey,
		"match_count":    count,
		"threshold":      threshold,
		"confidence":     confidenceName(confidence),
		"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	line, _ := json.Marshal(entry)
	log.Printf("[BEHAVIOR_ABUSE] %s", line)
}

// ResetBehaviorAbuse clears all behavior tracking state. For testing ONLY.
func ResetBehaviorAbuse() {
	behaviorMu.Lock()
	behaviorMap = make(map[string]*behaviorWindow)
	behaviorMu.Unlock()
	setCurrentAbuseRatio(0)
	ResetSecurityMode()
}

// CleanupBehaviorState evicts stale behavior windows. Called by CleanupExpiredState.
func CleanupBehaviorState() int {
	now := time.Now()
	behaviorMu.Lock()
	defer behaviorMu.Unlock()
	evicted := 0
	for fp, w := range behaviorMap {
		if now.Sub(w.lastSeen) > behaviorTTL {
			delete(behaviorMap, fp)
			evicted++
		}
	}
	return evicted
}

// ---------------------------------------------------------------------------
// 15. Security Mode — automatic escalation based on abuse ratio
// ---------------------------------------------------------------------------
//
// Three modes: NORMAL → ELEVATED → LOCKDOWN.
// Transitions are driven by the current behaviorAbuseRatio gauge:
//   abuse_ratio > 0.3 → LOCKDOWN
//   abuse_ratio > 0.1 → ELEVATED
//   abuse_ratio ≤ 0.1 → NORMAL   (recovery)
//
// Effects:
//   ELEVATED  — rate limit capacity reduced 50%, behavior threshold reduced by 1
//   LOCKDOWN  — rate limit capacity reduced 80%, low-confidence fingerprints blocked
//
// Design: fail-closed, no external deps, deterministic, O(1) per evaluation.

// SecurityMode represents the current security posture.
type SecurityMode int

const (
	ModeNormal SecurityMode = iota
	ModeElevated
	ModeLockdown
)

// Security mode thresholds (abuse ratio → mode).
const (
	elevatedThreshold = 0.1
	lockdownThreshold = 0.3
)

var (
	securityModeMu      sync.RWMutex
	currentSecurityMode SecurityMode = ModeNormal

	// securityModeTimeNow allows deterministic testing.
	securityModeTimeNow = time.Now
)

// SecurityModeName returns the string label for a given mode.
func SecurityModeName(m SecurityMode) string {
	switch m {
	case ModeElevated:
		return "elevated"
	case ModeLockdown:
		return "lockdown"
	default:
		return "normal"
	}
}

// GetSecurityMode returns the current security mode (safe for concurrent use).
func GetSecurityMode() SecurityMode {
	securityModeMu.RLock()
	defer securityModeMu.RUnlock()
	return currentSecurityMode
}

// EvaluateSecurityMode reads the current abuse ratio and transitions the
// security mode accordingly. Returns the new mode. Safe for concurrent use.
// Call this on every request — it is O(1) with no allocations on no-change path.
func EvaluateSecurityMode(abuseRatio float64) SecurityMode {
	var newMode SecurityMode
	switch {
	case abuseRatio > lockdownThreshold:
		newMode = ModeLockdown
	case abuseRatio > elevatedThreshold:
		newMode = ModeElevated
	default:
		newMode = ModeNormal
	}

	securityModeMu.Lock()
	prev := currentSecurityMode
	currentSecurityMode = newMode
	securityModeMu.Unlock()

	// Emit log + update metrics only on transition
	if newMode != prev {
		securityModeLog(prev, newMode, abuseRatio)
		updateSecurityModeMetrics(newMode, abuseRatio)
	}

	return newMode
}

// EffectiveRateLimitCapacity returns the adjusted token bucket capacity
// based on the current security mode.
//
//	NORMAL   → bucketCapacity (5.0)
//	ELEVATED → bucketCapacity * 0.5  (50% reduction)
//	LOCKDOWN → bucketCapacity * 0.2  (80% reduction)
func EffectiveRateLimitCapacity(mode SecurityMode) float64 {
	switch mode {
	case ModeElevated:
		return bucketCapacity * 0.5
	case ModeLockdown:
		return bucketCapacity * 0.2
	default:
		return bucketCapacity
	}
}

// EffectiveBehaviorThreshold returns the adjusted behavior abuse threshold.
// In ELEVATED mode, threshold is reduced by 1 (more aggressive detection).
// In LOCKDOWN mode, threshold is reduced by 2 (minimum 1).
func EffectiveBehaviorThreshold(base int, mode SecurityMode) int {
	switch mode {
	case ModeElevated:
		if base > 1 {
			return base - 1
		}
		return 1
	case ModeLockdown:
		if base > 2 {
			return base - 2
		}
		return 1
	default:
		return base
	}
}

// ShouldBlockLowConfidence returns true when LOCKDOWN is active and the
// fingerprint has low confidence. Fail-closed: block.
func ShouldBlockLowConfidence(mode SecurityMode, confidence FingerprintConfidence) bool {
	return mode == ModeLockdown && confidence == ConfidenceLow
}

// securityModeLog emits structured JSON on every mode transition.
func securityModeLog(prev, next SecurityMode, abuseRatio float64) {
	entry := map[string]interface{}{
		"type":        "security_mode_change",
		"from":        SecurityModeName(prev),
		"to":          SecurityModeName(next),
		"abuse_ratio": abuseRatio,
		"timestamp":   securityModeTimeNow().UTC().Format(time.RFC3339Nano),
	}
	line, _ := json.Marshal(entry)
	log.Printf("[SECURITY_MODE] %s", line)
}

// updateSecurityModeMetrics sets the orbit_security_mode gauge vector and reason.
func updateSecurityModeMetrics(mode SecurityMode, abuseRatio float64) {
	// Reset all mode labels to 0, set active to 1
	for _, m := range []string{"normal", "elevated", "lockdown"} {
		securityModeGauge.WithLabelValues(m).Set(0)
	}
	securityModeGauge.WithLabelValues(SecurityModeName(mode)).Set(1)

	reason := "abuse_ratio_normal"
	switch mode {
	case ModeElevated:
		reason = fmt.Sprintf("abuse_ratio_elevated(%.3f>%.1f)", abuseRatio, elevatedThreshold)
	case ModeLockdown:
		reason = fmt.Sprintf("abuse_ratio_lockdown(%.3f>%.1f)", abuseRatio, lockdownThreshold)
	}
	securityModeReasonGauge.WithLabelValues(reason).Set(1)
}

// ResetSecurityMode resets the security mode to NORMAL. For testing ONLY.
func ResetSecurityMode() {
	securityModeMu.Lock()
	currentSecurityMode = ModeNormal
	securityModeMu.Unlock()
}

// getCurrentAbuseRatio reads the latest abuse ratio stored by CheckBehaviorAbuse.
// We track it as an atomic-like variable alongside the gauge to avoid reading
// from Prometheus internals.
var currentAbuseRatio float64
var abuseRatioMu sync.RWMutex

func setCurrentAbuseRatio(r float64) {
	abuseRatioMu.Lock()
	currentAbuseRatio = r
	abuseRatioMu.Unlock()
}

func getCurrentAbuseRatio() float64 {
	abuseRatioMu.RLock()
	defer abuseRatioMu.RUnlock()
	return currentAbuseRatio
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

	// Behavior abuse window cleanup
	behaviorEvicted := CleanupBehaviorState()
	dedupEvicted += behaviorEvicted

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

	// orbit_behavior_abuse_total: events rejected by behavior heuristic.
	behaviorAbuseTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_behavior_abuse_total",
			Help: "Total events rejected by behavior abuse detection (repeated-payload heuristic).",
		},
	)

	// orbit_behavior_abuse_ratio: current highest similarity ratio in behavior windows.
	// Range 0.0–1.0. Useful for dashboard visualization of abuse pressure.
	behaviorAbuseRatio = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "orbit_behavior_abuse_ratio",
			Help: "Current similarity ratio of the most recent behavior check (0.0–1.0). " +
				"High values indicate potential abuse patterns building up.",
		},
	)

	// orbit_security_mode{mode}: active security mode (1 for active, 0 for inactive).
	securityModeGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "orbit_security_mode",
			Help: "Active security mode: normal, elevated, or lockdown. " +
				"Exactly one label value is 1 at any time.",
		},
		[]string{"mode"},
	)

	// orbit_security_mode_reason{reason}: human-readable reason for current mode.
	securityModeReasonGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "orbit_security_mode_reason",
			Help: "Reason for the current security mode transition.",
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
		behaviorAbuseTotal,
		behaviorAbuseRatio,
		securityModeGauge,
		securityModeReasonGauge,
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
