package tracking

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const testReconcileSecret = "orbit-test-reconcile-secret-32b"

// authTestWindow is the tolerance used in all auth tests.
const authTestWindow = 30 * time.Second

// signedReconcileRequest builds an httptest.Request with valid HMAC headers.
// tsUnix overrides the timestamp (pass 0 to use time.Now()).
func signedReconcileRequest(t *testing.T, secret []byte, body []byte, tsUnix int64) *http.Request {
	t.Helper()
	if tsUnix == 0 {
		tsUnix = time.Now().Unix()
	}
	ts := strconv.FormatInt(tsUnix, 10)
	sig := ComputeReconcileSignature(secret, ts, body)
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("X-Orbit-Timestamp", ts)
	req.Header.Set("X-Orbit-Signature", sig)
	return req
}

// reconcileAuthCounterVal reads a specific reason label from reconcileAuthRejected.
func reconcileAuthCounterVal(reason string) float64 {
	mfs, err := reconcileAuthRejected.GetMetricWithLabelValues(reason)
	if err != nil {
		return 0
	}
	var m dto.Metric
	_ = mfs.Write(&m)
	return m.GetCounter().GetValue()
}

// simpleOKHandler is an inner handler that always returns 200 with a fixed body.
var simpleOKHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
})

// validReconcileBody returns a minimal valid /reconcile JSON body.
func validReconcileBody() []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"session_id": "s1",
		"estimated":  300,
		"actual":     420,
	})
	return b
}

// ---------------------------------------------------------------------------
// Core auth tests
// ---------------------------------------------------------------------------

func TestReconcileAuth_ValidSignature(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	req := signedReconcileRequest(t, []byte(testReconcileSecret), body, 0)
	rec := httptest.NewRecorder()

	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200 (valid signature should pass)", rec.Code)
	}
}

func TestReconcileAuth_InvalidSignature(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("X-Orbit-Timestamp", ts)
	req.Header.Set("X-Orbit-Signature", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	rec := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (wrong signature)", rec.Code)
	}
	assertRejectionReason(t, rec.Body.Bytes(), authReasonInvalidSignature)
}

func TestReconcileAuth_ExpiredTimestamp_TooOld(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	// 60s before now — outside the 30s window
	oldTS := time.Now().Add(-60 * time.Second).Unix()
	req := signedReconcileRequest(t, []byte(testReconcileSecret), body, oldTS)
	rec := httptest.NewRecorder()

	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (timestamp too old)", rec.Code)
	}
	assertRejectionReason(t, rec.Body.Bytes(), authReasonExpiredTimestamp)
}

func TestReconcileAuth_ExpiredTimestamp_TooFar(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	// 60s in future — outside the 30s window
	futureTS := time.Now().Add(60 * time.Second).Unix()
	req := signedReconcileRequest(t, []byte(testReconcileSecret), body, futureTS)
	rec := httptest.NewRecorder()

	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (timestamp too far in future)", rec.Code)
	}
	assertRejectionReason(t, rec.Body.Bytes(), authReasonExpiredTimestamp)
}

func TestReconcileAuth_ReplayAttack(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	ts := time.Now().Unix()
	req1 := signedReconcileRequest(t, []byte(testReconcileSecret), body, ts)
	req2 := signedReconcileRequest(t, []byte(testReconcileSecret), body, ts)

	rec1 := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d; want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("replay: status = %d; want 401", rec2.Code)
	}
	assertRejectionReason(t, rec2.Body.Bytes(), authReasonReplayDetected)
}

func TestReconcileAuth_DifferentBodiesNotReplay(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body1, _ := json.Marshal(map[string]interface{}{"session_id": "s1", "estimated": 100, "actual": 120})
	body2, _ := json.Marshal(map[string]interface{}{"session_id": "s1", "estimated": 200, "actual": 240})

	// Same timestamp, different bodies → different signatures → both pass.
	ts := time.Now().Unix()
	req1 := signedReconcileRequest(t, []byte(testReconcileSecret), body1, ts)
	req2 := signedReconcileRequest(t, []byte(testReconcileSecret), body2, ts)

	rec1 := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec1, req1)

	rec2 := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec2, req2)

	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Errorf("different bodies should both pass: status1=%d status2=%d", rec1.Code, rec2.Code)
	}
}

func TestReconcileAuth_MissingSignatureHeader(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("X-Orbit-Timestamp", ts)
	// No X-Orbit-Signature

	rec := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (missing signature)", rec.Code)
	}
	assertRejectionReason(t, rec.Body.Bytes(), authReasonMissingSignature)
}

func TestReconcileAuth_MissingTimestampHeader(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()

	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("X-Orbit-Signature", "aabbcc")
	// No X-Orbit-Timestamp

	rec := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (missing timestamp)", rec.Code)
	}
	assertRejectionReason(t, rec.Body.Bytes(), authReasonMissingTimestamp)
}

func TestReconcileAuth_InvalidTimestampFormat(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()

	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("X-Orbit-Timestamp", "not-a-number")
	req.Header.Set("X-Orbit-Signature", "aabbcc")

	rec := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (invalid timestamp)", rec.Code)
	}
	assertRejectionReason(t, rec.Body.Bytes(), authReasonInvalidTimestamp)
}

func TestReconcileAuth_InvalidSignatureHex(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("X-Orbit-Timestamp", ts)
	req.Header.Set("X-Orbit-Signature", "not-valid-hex!!!")

	rec := httptest.NewRecorder()
	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (invalid hex)", rec.Code)
	}
	assertRejectionReason(t, rec.Body.Bytes(), authReasonInvalidSignature)
}

func TestReconcileAuth_WrongSecret(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	// Sign with a different secret
	req := signedReconcileRequest(t, []byte("wrong-secret"), body, 0)
	rec := httptest.NewRecorder()

	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 (wrong secret)", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// No-secret (dev) mode
// ---------------------------------------------------------------------------

func TestReconcileAuth_NoSecret_PassThrough(t *testing.T) {
	// No secret → auth disabled → inner handler is always called.
	auth := NewReconcileAuth(nil, authTestWindow)
	body := validReconcileBody()
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	// No auth headers — should still pass.
	rec := httptest.NewRecorder()

	auth.Middleware(simpleOKHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200 (no-secret passthrough)", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Body is preserved for inner handler after auth reads it
// ---------------------------------------------------------------------------

func TestReconcileAuth_BodyPreservedForInnerHandler(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	body := validReconcileBody()
	req := signedReconcileRequest(t, []byte(testReconcileSecret), body, 0)

	var capturedBody []byte
	capturingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := peekRequestBody(r)
		capturedBody = b
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	auth.Middleware(capturingHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("auth failed unexpectedly: %d", rec.Code)
	}
	if string(capturedBody) != string(body) {
		t.Errorf("inner handler got body %q; want %q", capturedBody, body)
	}
}

// ---------------------------------------------------------------------------
// Prometheus metric tests
// ---------------------------------------------------------------------------

func TestReconcileAuthMetric_InvalidSignatureIncrement(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	before := reconcileAuthCounterVal(authReasonInvalidSignature)

	body := validReconcileBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("X-Orbit-Timestamp", ts)
	req.Header.Set("X-Orbit-Signature", "0000000000000000000000000000000000000000000000000000000000000000")

	auth.Middleware(simpleOKHandler).ServeHTTP(httptest.NewRecorder(), req)

	after := reconcileAuthCounterVal(authReasonInvalidSignature)
	if after-before != 1 {
		t.Errorf("invalid_signature counter delta = %.0f; want 1", after-before)
	}
}

func TestReconcileAuthMetric_ReplayDetectedIncrement(t *testing.T) {
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	before := reconcileAuthCounterVal(authReasonReplayDetected)

	body := validReconcileBody()
	ts := time.Now().Unix()
	req1 := signedReconcileRequest(t, []byte(testReconcileSecret), body, ts)
	req2 := signedReconcileRequest(t, []byte(testReconcileSecret), body, ts)

	auth.Middleware(simpleOKHandler).ServeHTTP(httptest.NewRecorder(), req1)
	auth.Middleware(simpleOKHandler).ServeHTTP(httptest.NewRecorder(), req2)

	after := reconcileAuthCounterVal(authReasonReplayDetected)
	if after-before != 1 {
		t.Errorf("replay_detected counter delta = %.0f; want 1", after-before)
	}
}

// ---------------------------------------------------------------------------
// PromQL governance
// ---------------------------------------------------------------------------

func TestReconcileAuthMetricNames_PassGovernance(t *testing.T) {
	for _, name := range ReconcileAuthMetricNames {
		if err := ValidatePromQLStrict(name); err != nil {
			t.Errorf("metric %q fails governance: %v", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: ReconcileAuth + ReconcileHandler end-to-end
// ---------------------------------------------------------------------------

func TestReconcileAuthIntegration_ValidRequest(t *testing.T) {
	reg := newTestReconcileRegistry()
	sid := "auth-int-" + reconcileSuffix()
	reg.CheckAndConsume(sid, 300) //nolint:errcheck

	body, _ := json.Marshal(map[string]interface{}{
		"session_id": sid,
		"estimated":  300,
		"actual":     420,
	})

	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	handler := auth.Middleware(ReconcileHandler(reg))

	req := signedReconcileRequest(t, []byte(testReconcileSecret), body, 0)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("integration: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result ReconcileResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Delta != 120 {
		t.Errorf("Delta=%d; want 120", result.Delta)
	}
}

func TestReconcileAuthIntegration_InvalidSigBlocksHandler(t *testing.T) {
	reg := newTestReconcileRegistry()
	auth := NewReconcileAuth([]byte(testReconcileSecret), authTestWindow)
	handler := auth.Middleware(ReconcileHandler(reg))

	body := validReconcileBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/reconcile", bytes.NewReader(body))
	req.Header.Set("X-Orbit-Timestamp", ts)
	req.Header.Set("X-Orbit-Signature", "badbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadb")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d; want 401 — handler must be blocked by auth", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// assertRejectionReason parses the response body and checks the reason field.
// ---------------------------------------------------------------------------

func assertRejectionReason(t *testing.T, body []byte, want string) {
	t.Helper()
	var resp map[string]string
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Errorf("response is not valid JSON: %v (body=%q)", err, body)
		return
	}
	if got := resp["reason"]; got != want {
		t.Errorf("rejection reason = %q; want %q", got, want)
	}
}
