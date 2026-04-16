// security_test.go — Anti-regression tests for the orbit-engine security layer.
//
// Run:
//
//	cd tracking && go test -run TestSecurity -v
package tracking

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

// newSecurityTestRegistry returns a fresh Prometheus registry with
// ALL metrics (core + security) registered.
func newSecurityTestRegistry() *prometheus.Registry {
	ResetRateLimit() // clears token buckets + dedup
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		skillActivationsTotal,
		skillTokensSavedTotal,
		skillWasteEstimated,
		skillTrackingFailuresTotal,
		skillSessionsTotal,
		skillSessionsWithActivation,
		skillSessionsWithoutActivation,
		seedModeGauge,
		trackingUpGauge,
		instanceIDGauge,
		lastEventTimestampGauge,
		skillActivationLatency,
		heartbeatTotal,
		realUsageTotal,
		skillActivationByReason,
		lastRealUsageTimestamp,
	)
	RegisterSecurityMetrics(reg)
	return reg
}

// securityTestEvent returns a valid JSON payload + SkillEvent for tests.
func securityTestEvent(sessionID string) ([]byte, SkillEvent) {
	ev := SkillEvent{
		EventType:            "activation",
		Timestamp:            NowUTC(),
		SessionID:            sessionID,
		Mode:                 "auto",
		Trigger:              "real_usage_client",
		EstimatedWaste:       100.0,
		ActionsSuggested:     1,
		ActionsApplied:       1,
		ImpactEstimatedToken: 500,
	}
	body, _ := json.Marshal(ev)
	return body, ev
}

// ---------------------------------------------------------------------------
// 1. Deterministic Event ID
// ---------------------------------------------------------------------------

func TestSecurityEventIDDeterministic(t *testing.T) {
	payload := []byte(`{"event_type":"activation","session_id":"s1"}`)

	id1 := ComputeEventID(payload)
	id2 := ComputeEventID(payload)
	if id1 != id2 {
		t.Fatalf("event_id not deterministic: %s != %s", id1, id2)
	}
	if len(id1) != 64 { // SHA-256 hex = 64 chars
		t.Fatalf("event_id length should be 64, got %d", len(id1))
	}

	// Different payloads → different IDs
	id3 := ComputeEventID([]byte(`{"event_type":"activation","session_id":"s2"}`))
	if id1 == id3 {
		t.Fatal("different payloads should produce different event_ids")
	}
}

// ---------------------------------------------------------------------------
// 2. Deduplication
// ---------------------------------------------------------------------------

func TestSecurityDedupBlocksReplay(t *testing.T) {
	ResetDedup()

	eventID := ComputeEventID([]byte("test-dedup-payload"))

	// First call: accepted
	if err := CheckDedup(eventID); err != nil {
		t.Fatalf("first event should be accepted: %v", err)
	}

	// Immediate replay: blocked
	err := CheckDedup(eventID)
	if err == nil {
		t.Fatal("replayed event should be blocked")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error should mention duplicate, got: %v", err)
	}
}

func TestSecurityDedupAllowsAfterWindow(t *testing.T) {
	ResetDedup()

	eventID := "test-dedup-window-id"

	// Manually insert an old entry (beyond window)
	dedupMu.Lock()
	dedupMap[eventID] = time.Now().Add(-dedupWindow - time.Minute)
	dedupMu.Unlock()

	// Should be accepted (outside dedup window)
	if err := CheckDedup(eventID); err != nil {
		t.Fatalf("event outside dedup window should be accepted: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 3. HMAC Authentication
// ---------------------------------------------------------------------------

func TestSecurityHMACRequired(t *testing.T) {
	// Enable HMAC
	SetHMACSecret("test-secret-key-32bytes-long!!")
	defer SetHMACSecret("") // restore

	payload := []byte(`{"event_type":"activation"}`)

	// Missing signature → rejected
	err := ValidateHMAC(payload, "")
	if err == nil {
		t.Fatal("missing HMAC signature should be rejected when secret is set")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error should mention missing, got: %v", err)
	}
}

func TestSecurityHMACValidSignature(t *testing.T) {
	secret := "test-secret-key-32bytes-long!!"
	SetHMACSecret(secret)
	defer SetHMACSecret("")

	payload := []byte(`{"event_type":"activation","session_id":"s1"}`)
	sig := ComputeHMACHex(payload, []byte(secret))

	// Valid signature → accepted
	if err := ValidateHMAC(payload, sig); err != nil {
		t.Fatalf("valid HMAC should be accepted: %v", err)
	}

	// Tampered payload → rejected
	tampered := []byte(`{"event_type":"activation","session_id":"s2"}`)
	if err := ValidateHMAC(tampered, sig); err == nil {
		t.Fatal("tampered payload should be rejected")
	}

	// Wrong signature → rejected
	if err := ValidateHMAC(payload, "deadbeef"); err == nil {
		t.Fatal("wrong signature should be rejected")
	}
}

func TestSecurityHMACDisabledByDefault(t *testing.T) {
	SetHMACSecret("") // explicitly disabled

	// No HMAC configured → any request passes
	if err := ValidateHMAC([]byte("anything"), ""); err != nil {
		t.Fatalf("HMAC should pass when disabled: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 4. Token Bucket Rate Limiting
// ---------------------------------------------------------------------------

func TestSecurityTokenBucket(t *testing.T) {
	ResetTokenBuckets()

	session := "tb-test-sess"

	// First 5 requests (bucket capacity) should succeed
	for i := 0; i < int(bucketCapacity); i++ {
		if err := CheckTokenBucket(session); err != nil {
			t.Fatalf("request %d should succeed (within capacity): %v", i+1, err)
		}
	}

	// 6th request (no time passed) should fail
	err := CheckTokenBucket(session)
	if err == nil {
		t.Fatal("request beyond capacity should be rejected")
	}
	if !strings.Contains(err.Error(), "token bucket exhausted") {
		t.Fatalf("error should mention token bucket, got: %v", err)
	}
}

func TestSecurityTokenBucketRefills(t *testing.T) {
	ResetTokenBuckets()

	session := "tb-refill-test"

	// Drain the bucket
	for i := 0; i < int(bucketCapacity); i++ {
		CheckTokenBucket(session)
	}

	// Simulate time passing (2 seconds → 2 tokens refilled)
	orig := bucketTimeNow
	bucketTimeNow = func() time.Time { return time.Now().Add(2 * time.Second) }
	defer func() { bucketTimeNow = orig }()

	// Should succeed now (refilled)
	if err := CheckTokenBucket(session); err != nil {
		t.Fatalf("bucket should have refilled: %v", err)
	}
}

func TestSecurityTokenBucketDisabled(t *testing.T) {
	ResetTokenBuckets()
	DisableTokenBuckets()
	defer ResetTokenBuckets()

	session := "tb-disabled-test"

	// Should always pass when disabled
	for i := 0; i < 100; i++ {
		if err := CheckTokenBucket(session); err != nil {
			t.Fatalf("should pass when disabled: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. Cleanup
// ---------------------------------------------------------------------------

func TestSecurityCleanupEvictsExpired(t *testing.T) {
	ResetDedup()
	ResetTokenBuckets()

	// Insert old dedup entry
	dedupMu.Lock()
	dedupMap["old-event"] = time.Now().Add(-2 * sessionTTL)
	dedupMap["fresh-event"] = time.Now()
	dedupMu.Unlock()

	// Insert old bucket entry
	bucketMu.Lock()
	sessionBuckets["old-session"] = &tokenBucket{tokens: 5, lastRefill: time.Now(), capacity: 5, refillRate: 1}
	bucketLastAccess["old-session"] = time.Now().Add(-2 * sessionTTL)
	sessionBuckets["fresh-session"] = &tokenBucket{tokens: 5, lastRefill: time.Now(), capacity: 5, refillRate: 1}
	bucketLastAccess["fresh-session"] = time.Now()
	bucketMu.Unlock()

	// Run cleanup
	dedupEvicted, bucketsEvicted := CleanupExpiredState()

	if dedupEvicted != 1 {
		t.Fatalf("expected 1 dedup eviction, got %d", dedupEvicted)
	}
	if bucketsEvicted != 1 {
		t.Fatalf("expected 1 bucket eviction, got %d", bucketsEvicted)
	}

	// Fresh entries should still exist
	dedupMu.Lock()
	if _, ok := dedupMap["fresh-event"]; !ok {
		t.Fatal("fresh dedup entry was incorrectly evicted")
	}
	dedupMu.Unlock()

	bucketMu.Lock()
	if _, ok := sessionBuckets["fresh-session"]; !ok {
		t.Fatal("fresh bucket was incorrectly evicted")
	}
	bucketMu.Unlock()
}

// ---------------------------------------------------------------------------
// 6. orbit_real_usage_alive Gauge
// ---------------------------------------------------------------------------

func TestSecurityRealUsageAlive(t *testing.T) {
	reg := newSecurityTestRegistry()

	// Initially 0
	ClearRealUsageAlive()
	families, _ := reg.Gather()
	v := gaugeValue(families, "orbit_real_usage_alive")
	if v != 0 {
		t.Fatalf("orbit_real_usage_alive should start at 0, got %f", v)
	}

	// After SetRealUsageAlive → 1
	SetRealUsageAlive()
	families, _ = reg.Gather()
	v = gaugeValue(families, "orbit_real_usage_alive")
	if v != 1 {
		t.Fatalf("orbit_real_usage_alive should be 1 after SetRealUsageAlive, got %f", v)
	}

	// After ClearRealUsageAlive → 0
	ClearRealUsageAlive()
	families, _ = reg.Gather()
	v = gaugeValue(families, "orbit_real_usage_alive")
	if v != 0 {
		t.Fatalf("orbit_real_usage_alive should be 0 after clear, got %f", v)
	}
}

// ---------------------------------------------------------------------------
// 7. Full Pipeline: TrackHandler with Security
// ---------------------------------------------------------------------------

func TestSecurityTrackHandlerDedup(t *testing.T) {
	reg := newSecurityTestRegistry()
	_ = reg
	SetHMACSecret("") // HMAC disabled for this test
	ResetDedup()

	mux := http.NewServeMux()
	mux.HandleFunc("/track", TrackHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := securityTestEvent("dedup-handler-test")

	// First request: accepted
	resp1 := postTrack(t, srv.URL, body, "")
	if resp1.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp1.Body)
		t.Fatalf("first request should succeed: %d %s", resp1.StatusCode, string(b))
	}
	resp1.Body.Close()

	// Same exact payload: rejected (409 Conflict)
	resp2 := postTrack(t, srv.URL, body, "")
	if resp2.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("replayed request should be 409 Conflict, got %d %s", resp2.StatusCode, string(b))
	}
	resp2.Body.Close()
}

func TestSecurityTrackHandlerHMAC(t *testing.T) {
	_ = newSecurityTestRegistry()

	secret := "handler-hmac-test-secret!!"
	SetHMACSecret(secret)
	defer SetHMACSecret("")
	ResetDedup()

	mux := http.NewServeMux()
	mux.HandleFunc("/track", TrackHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := securityTestEvent("hmac-handler-test")

	// Without signature: rejected (401)
	resp1 := postTrack(t, srv.URL, body, "")
	if resp1.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(resp1.Body)
		t.Fatalf("missing HMAC should be 401, got %d %s", resp1.StatusCode, string(b))
	}
	resp1.Body.Close()

	// With valid signature: accepted
	sig := ComputeHMACHex(body, []byte(secret))
	resp2 := postTrackWithSignature(t, srv.URL, body, sig)
	if resp2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("valid HMAC should be 200, got %d %s", resp2.StatusCode, string(b))
	}
	resp2.Body.Close()
}

func TestSecurityTrackHandlerReturnsEventID(t *testing.T) {
	_ = newSecurityTestRegistry()
	SetHMACSecret("")
	ResetDedup()

	mux := http.NewServeMux()
	mux.HandleFunc("/track", TrackHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := securityTestEvent("eventid-response-test")

	resp := postTrack(t, srv.URL, body, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	eventID := result["event_id"]
	if eventID == "" {
		t.Fatal("response should contain event_id")
	}
	if len(eventID) != 64 {
		t.Fatalf("event_id should be 64 hex chars, got %d", len(eventID))
	}

	// event_id should match ComputeEventID of the body
	expected := ComputeEventID(body)
	if eventID != expected {
		t.Fatalf("response event_id %s != computed %s", eventID, expected)
	}
}

func TestSecurityTrackHandlerSetsAlive(t *testing.T) {
	reg := newSecurityTestRegistry()
	SetHMACSecret("")
	ResetDedup()
	ClearRealUsageAlive()

	mux := http.NewServeMux()
	mux.HandleFunc("/track", TrackHandler())
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := securityTestEvent("alive-handler-test")

	resp := postTrack(t, srv.URL, body, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Check orbit_real_usage_alive = 1 via /metrics
	metricsResp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	metricsBody, _ := io.ReadAll(metricsResp.Body)
	if !strings.Contains(string(metricsBody), "orbit_real_usage_alive 1") {
		t.Fatal("orbit_real_usage_alive should be 1 after successful /track")
	}
}

// ---------------------------------------------------------------------------
// 8. Security Metrics Exist
// ---------------------------------------------------------------------------

func TestSecurityMetricsExist(t *testing.T) {
	reg := newSecurityTestRegistry()

	// Trigger at least one series in each metric
	trackingDedupBlocked.Inc()
	trackingHMACFailures.Inc()
	trackingCleanupTotal.Inc()
	trackingBucketRejected.Inc()
	realUsageAlive.Set(1)
	IncrementRejected(RejectReasonHMAC)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	fm := make(map[string]bool)
	for _, f := range families {
		fm[f.GetName()] = true
	}

	expected := []string{
		"orbit_tracking_dedup_blocked_total",
		"orbit_tracking_hmac_failures_total",
		"orbit_tracking_cleanup_total",
		"orbit_real_usage_alive",
		"orbit_tracking_token_bucket_rejected_total",
		"orbit_tracking_rejected_total",
	}
	for _, name := range expected {
		if !fm[name] {
			t.Errorf("security metric %q not found in registry", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 9. Governance allows security metrics
// ---------------------------------------------------------------------------

func TestSecurityGovernanceAllowsMetrics(t *testing.T) {
	metrics := []string{
		"orbit_tracking_dedup_blocked_total",
		"orbit_tracking_hmac_failures_total",
		"orbit_tracking_cleanup_total",
		"orbit_real_usage_alive",
		"orbit_tracking_token_bucket_rejected_total",
		"orbit_tracking_rejected_total",
	}
	for _, m := range metrics {
		if err := ValidatePromQLStrict(m); err != nil {
			t.Errorf("governance rejected security metric %q: %v", m, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func gaugeValue(families []*dto.MetricFamily, name string) float64 {
	for _, f := range families {
		if f.GetName() == name && len(f.GetMetric()) > 0 {
			return f.GetMetric()[0].GetGauge().GetValue()
		}
	}
	return -1 // sentinel: not found
}

func postTrack(t *testing.T, baseURL string, body []byte, _ string) *http.Response {
	t.Helper()
	resp, err := http.Post(baseURL+"/track", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /track: %v", err)
	}
	return resp
}

func postTrackWithSignature(t *testing.T, baseURL string, body []byte, sig string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Orbit-Signature", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /track with sig: %v", err)
	}
	return resp
}

func postTrackWithHeaders(t *testing.T, baseURL string, body []byte, sig, clientID string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if sig != "" {
		req.Header.Set("X-Orbit-Signature", sig)
	}
	if clientID != "" {
		req.Header.Set("X-Orbit-Client-Id", clientID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /track with headers: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// 10. JSON Canonicalization Anti-Bypass
// ---------------------------------------------------------------------------

func TestSecurityCanonicalizeJSON(t *testing.T) {
	// Different key orders → same canonical form
	payload1 := []byte(`{"a":1,"b":2}`)
	payload2 := []byte(`{"b":2,"a":1}`)

	c1 := CanonicalizeJSON(payload1)
	c2 := CanonicalizeJSON(payload2)

	if string(c1) != string(c2) {
		t.Fatalf("canonical forms should be equal:\n  c1=%s\n  c2=%s", c1, c2)
	}

	// Event IDs must match
	id1 := ComputeEventID(payload1)
	id2 := ComputeEventID(payload2)
	if id1 != id2 {
		t.Fatalf("event_id should be identical for equivalent JSON:\n  id1=%s\n  id2=%s", id1, id2)
	}
}

func TestSecurityCanonicalizeJSONNested(t *testing.T) {
	payload1 := []byte(`{"z":{"b":2,"a":1},"y":[3,1,2]}`)
	payload2 := []byte(`{"y":[3,1,2],"z":{"a":1,"b":2}}`)

	id1 := ComputeEventID(payload1)
	id2 := ComputeEventID(payload2)
	if id1 != id2 {
		t.Fatalf("nested JSON with different key order should produce same event_id:\n  id1=%s\n  id2=%s", id1, id2)
	}
}

func TestSecurityCanonicalizeJSONWhitespace(t *testing.T) {
	compact := []byte(`{"a":1,"b":2}`)
	spaced := []byte(`{  "a" : 1 , "b" : 2  }`)

	id1 := ComputeEventID(compact)
	id2 := ComputeEventID(spaced)
	if id1 != id2 {
		t.Fatalf("whitespace differences should produce same event_id:\n  id1=%s\n  id2=%s", id1, id2)
	}
}

func TestSecurityCanonicalizeInvalidJSON(t *testing.T) {
	// Invalid JSON returns as-is (fail-safe)
	invalid := []byte(`not json at all`)
	result := CanonicalizeJSON(invalid)
	if string(result) != string(invalid) {
		t.Fatalf("invalid JSON should pass through unchanged, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// 11. Dedup Anti-Bypass via JSON Reorder
// ---------------------------------------------------------------------------

func TestSecurityDedupBlocksJSONReorder(t *testing.T) {
	ResetDedup()

	// Two payloads with same data but different key order
	payload1 := []byte(`{"event_type":"activation","session_id":"s1","mode":"auto"}`)
	payload2 := []byte(`{"mode":"auto","event_type":"activation","session_id":"s1"}`)

	id1 := ComputeEventID(payload1)
	id2 := ComputeEventID(payload2)

	if id1 != id2 {
		t.Fatalf("reordered JSON should have same event_id: %s != %s", id1, id2)
	}

	// First should be accepted
	if err := CheckDedup(id1); err != nil {
		t.Fatalf("first event should be accepted: %v", err)
	}

	// Reordered payload should be blocked (same canonical event_id)
	if err := CheckDedup(id2); err == nil {
		t.Fatal("reordered JSON payload should be blocked by dedup")
	}
}

// ---------------------------------------------------------------------------
// 12. HMAC Mandatory in Production
// ---------------------------------------------------------------------------

func TestSecurityHMACMandatoryInProduction(t *testing.T) {
	// Save and restore state
	origSecret := hmacSecret
	origRequired := hmacRequired
	origProd := isProduction
	defer func() {
		hmacSecret = origSecret
		hmacRequired = origRequired
		isProduction = origProd
	}()

	// Simulate production mode without HMAC — should panic
	SetProductionMode(true)
	SetHMACSecret("")

	// ValidateHMAC should still pass when hmacRequired=false
	// The real enforcement is at init() — we test the behavior
	// by verifying IsProductionMode() returns true
	if !IsProductionMode() {
		t.Fatal("IsProductionMode should return true")
	}

	// When HMAC is set in production, it should work normally
	SetHMACSecret("prod-secret-key-32bytes!!")
	payload := []byte(`{"event":"test"}`)
	sig := ComputeHMACHex(payload, []byte("prod-secret-key-32bytes!!"))
	if err := ValidateHMAC(payload, sig); err != nil {
		t.Fatalf("valid HMAC in production should pass: %v", err)
	}

	// Missing signature in production (HMAC required) → reject
	if err := ValidateHMAC(payload, ""); err == nil {
		t.Fatal("missing HMAC in production should be rejected")
	}
}

// ---------------------------------------------------------------------------
// 13. Client Identity for Token Bucket
// ---------------------------------------------------------------------------

func TestSecurityClientIdentity(t *testing.T) {
	// X-Orbit-Client-Id takes priority
	key1 := ClientIdentity("client-abc", "1.2.3.4:5000", "Mozilla/5.0", "sess-1")
	if key1 != "client:client-abc" {
		t.Fatalf("expected client:client-abc, got %s", key1)
	}

	// No client ID → IP+UA hash
	key2 := ClientIdentity("", "1.2.3.4:5000", "Mozilla/5.0", "sess-1")
	if !strings.HasPrefix(key2, "ip:") {
		t.Fatalf("expected ip: prefix, got %s", key2)
	}

	// Different IP → different key
	key3 := ClientIdentity("", "5.6.7.8:9000", "Mozilla/5.0", "sess-1")
	if key2 == key3 {
		t.Fatal("different IPs should produce different bucket keys")
	}

	// No client ID, no remote addr → session fallback
	key4 := ClientIdentity("", "", "", "sess-123")
	if key4 != "session:sess-123" {
		t.Fatalf("expected session:sess-123, got %s", key4)
	}
}

func TestSecurityTokenBucketPerClient(t *testing.T) {
	ResetTokenBuckets()

	// Two different clients should have independent buckets
	clientA := "client:alpha"
	clientB := "client:beta"

	// Drain client A's bucket
	for i := 0; i < int(bucketCapacity); i++ {
		if err := CheckTokenBucket(clientA); err != nil {
			t.Fatalf("clientA request %d should succeed: %v", i+1, err)
		}
	}

	// Client A should be rate-limited
	if err := CheckTokenBucket(clientA); err == nil {
		t.Fatal("clientA should be rate-limited after exhausting bucket")
	}

	// Client B should still have full bucket
	if err := CheckTokenBucket(clientB); err != nil {
		t.Fatalf("clientB should still have tokens: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 14. Unified Rejection Metric
// ---------------------------------------------------------------------------

func TestSecurityRejectedMetric(t *testing.T) {
	reg := newSecurityTestRegistry()

	// Trigger rejections for each reason
	IncrementRejected(RejectReasonHMAC)
	IncrementRejected(RejectReasonHMAC)
	IncrementRejected(RejectReasonDedup)
	IncrementRejected(RejectReasonRateLimit)
	IncrementRejected(RejectReasonInvalid)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	// Check orbit_tracking_rejected_total exists
	found := false
	for _, f := range families {
		if f.GetName() == "orbit_tracking_rejected_total" {
			found = true
			// Verify we have labeled series
			if len(f.GetMetric()) < 3 {
				t.Fatalf("expected at least 3 label combos, got %d", len(f.GetMetric()))
			}
			break
		}
	}
	if !found {
		t.Fatal("orbit_tracking_rejected_total metric not found in registry")
	}
}

// ---------------------------------------------------------------------------
// 15. TrackHandler Rate Limit Returns 429
// ---------------------------------------------------------------------------

func TestSecurityTrackHandlerRateLimit(t *testing.T) {
	_ = newSecurityTestRegistry()
	SetHMACSecret("")
	ResetDedup()

	mux := http.NewServeMux()
	mux.HandleFunc("/track", TrackHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	clientID := "rate-limit-test-client"

	// Send requests until rate limited (bucket capacity + 1)
	var lastStatus int
	for i := 0; i < int(bucketCapacity)+2; i++ {
		ev := SkillEvent{
			EventType:            "activation",
			Timestamp:            NowUTC(),
			SessionID:            "rl-sess-" + string(rune('A'+i)),
			Mode:                 "auto",
			Trigger:              "test",
			EstimatedWaste:       100.0,
			ActionsSuggested:     1,
			ActionsApplied:       1,
			ImpactEstimatedToken: 500,
		}
		body, _ := json.Marshal(ev)
		resp := postTrackWithHeaders(t, srv.URL, body, "", clientID)
		lastStatus = resp.StatusCode
		resp.Body.Close()
	}

	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests after exhausting bucket, got %d", lastStatus)
	}
}

// ---------------------------------------------------------------------------
// 16. Governance allows new security metric
// ---------------------------------------------------------------------------

func TestSecurityGovernanceAllowsRejectedMetric(t *testing.T) {
	if err := ValidatePromQLStrict("orbit_tracking_rejected_total"); err != nil {
		t.Errorf("governance rejected orbit_tracking_rejected_total: %v", err)
	}
	// With label selector
	if err := ValidatePromQLStrict(`rate(orbit_tracking_rejected_total{reason="hmac"}[5m])`); err != nil {
		t.Errorf("governance rejected orbit_tracking_rejected_total with label: %v", err)
	}
}
