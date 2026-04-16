// realusage.go — Real usage ingestion client for orbit-engine.
//
// RealUsageClient captures real prompt executions (input/output/tokens) and
// sends them to the tracking server via POST /track. Fail-closed: any
// failure is returned as an error and logged at [CRITICAL] level.
//
// Usage:
//
//	c := tracking.NewRealUsageClient("http://localhost:9100", sessionID, "auto")
//	if err := c.TrackPromptUsage(ctx, inputText, outputText); err != nil {
//	    log.Printf("[CRITICAL] usage tracking failed: %v", err)
//	}
//
// TrackHandler exports the canonical /track HTTP handler so that both the
// production server (cmd/main.go) and tests share a single implementation.
package tracking

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// RealUsageClient
// ---------------------------------------------------------------------------

// RealUsageClient sends SkillEvents to the orbit tracking server.
// Create one per session; reuse across multiple prompt executions.
type RealUsageClient struct {
	trackURL  string       // e.g. "http://localhost:9100/track"
	sessionID string       // unique per user session
	mode      string       // "auto" | "suggest" | "off"
	http      *http.Client // connection-reusing client
}

// NewRealUsageClient creates a client targeting trackingServerURL.
// sessionID must be unique per user session (e.g. UUID or hostname+pid).
// mode is one of "auto", "suggest", "off"; empty defaults to "auto".
func NewRealUsageClient(trackingServerURL, sessionID, mode string) *RealUsageClient {
	if mode == "" {
		mode = "auto"
	}
	return &RealUsageClient{
		trackURL:  trackingServerURL + "/track",
		sessionID: sessionID,
		mode:      mode,
		http:      &http.Client{Timeout: 5 * time.Second},
	}
}

// EstimateTokens returns a conservative token estimate: len(text)/4, min 1.
// Based on the standard LLM heuristic (1 token ≈ 4 characters).
func EstimateTokens(text string) int64 {
	n := int64(len(text)) / 4
	if n < 1 {
		n = 1
	}
	return n
}

// TrackPromptUsage records a real prompt execution as a SkillEvent.
// Estimates token savings from output length; estimated waste from input.
// Fail-closed: returns non-nil error on any failure. Caller must handle.
func (c *RealUsageClient) TrackPromptUsage(ctx context.Context, input, output string) error {
	event := SkillEvent{
		EventType:            "activation",
		Timestamp:            NowUTC(),
		SessionID:            c.sessionID,
		Mode:                 c.mode,
		Trigger:              "real_usage_client",
		EstimatedWaste:       float64(EstimateTokens(input)),
		ActionsSuggested:     1,
		ActionsApplied:       1,
		ImpactEstimatedToken: EstimateTokens(output),
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("realusage: marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.trackURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("realusage: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		log.Printf("[CRITICAL] orbit real usage tracking failed (url=%s): %v", c.trackURL, err)
		return fmt.Errorf("realusage: POST /track: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		log.Printf("[CRITICAL] tracking server rejected event: status=%d err=%s",
			resp.StatusCode, apiErr.Error)
		return fmt.Errorf("realusage: server returned HTTP %d: %s", resp.StatusCode, apiErr.Error)
	}

	return nil
}

// ---------------------------------------------------------------------------
// TrackHandler — canonical /track HTTP handler
// ---------------------------------------------------------------------------

// TrackHandler returns the http.HandlerFunc for POST /track.
// Exported so the production server and tests share one implementation.
//
// Security pipeline (fail-closed):
//  1. Method check (POST only)
//  2. Read raw body → canonicalize → compute deterministic event_id
//  3. Validate HMAC signature (X-Orbit-Signature) if configured
//  4. Compute client fingerprint (hardened identity)
//  5. Token bucket rate limit per client fingerprint
//  6. Decode JSON → semantic validation gate
//  7. Replay hard lock for critical events (persistent 24h dedup)
//  8. Standard dedup check (5-min window on event_id)
//  9. Validate → TrackSkillEvent
//
// 10. Set orbit_real_usage_alive = 1 on success
func TrackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		// 1. Read raw body for deterministic event_id and HMAC
		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
			return
		}

		// 2. Compute deterministic event_id from canonical JSON
		eventID := ComputeEventID(rawBody)

		// 3. HMAC authentication (fail-closed when configured)
		signature := r.Header.Get("X-Orbit-Signature")
		if err := ValidateHMAC(rawBody, signature); err != nil {
			// Pre-decode: no session_id available yet, use basic fingerprint
			clientIP := ExtractClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
			fingerprint := ClientFingerprint(
				r.Header.Get("X-Orbit-Client-Id"), clientIP,
				r.Header.Get("User-Agent"), r.Header.Get("Accept-Language"))
			trackingHMACFailures.Inc()
			IncrementRejected(RejectReasonHMAC)
			rejectionLog(RejectReasonHMAC, fingerprint, eventID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":    err.Error(),
				"event_id": eventID,
			})
			return
		}

		// 4. Extract real client IP (proxy-aware) and compute pre-decode fingerprint
		clientID := r.Header.Get("X-Orbit-Client-Id")
		clientIP := ExtractClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
		userAgent := r.Header.Get("User-Agent")
		acceptLang := r.Header.Get("Accept-Language")
		fingerprint := ClientFingerprint(clientID, clientIP, userAgent, acceptLang)

		// 4b. Security Mode evaluation — adapts enforcement based on abuse ratio
		confidence := ClassifyConfidence(clientID, fingerprint)
		secMode := EvaluateSecurityMode(getCurrentAbuseRatio())

		// LOCKDOWN: block low-confidence fingerprints immediately (fail-closed)
		if ShouldBlockLowConfidence(secMode, confidence) {
			IncrementRejected("security_mode_lockdown")
			rejectionLog("security_mode_lockdown", fingerprint, eventID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":    "blocked: security lockdown active for low-confidence clients",
				"event_id": eventID,
			})
			return
		}

		// 5. Token bucket rate limit per client fingerprint (mode-adjusted)
		if err := CheckTokenBucketWithMode(fingerprint, secMode); err != nil {
			trackingBucketRejected.Inc()
			IncrementRejected(RejectReasonRateLimit)
			rejectionLog(RejectReasonRateLimit, fingerprint, eventID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":    err.Error(),
				"event_id": eventID,
			})
			return
		}

		// 6. Decode JSON and semantic validation gate
		var event SkillEvent
		if err := json.Unmarshal(rawBody, &event); err != nil {
			IncrementRejected(RejectReasonInvalid)
			rejectionLog(RejectReasonInvalid, fingerprint, eventID)
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = NowUTC()
		}

		// Semantic gate — validates session_id, timestamp not-future, payload not-empty
		if err := ValidateSemantic(event, rawBody); err != nil {
			IncrementRejected(RejectReasonSemantic)
			rejectionLog(RejectReasonSemantic, fingerprint, eventID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":    err.Error(),
				"event_id": eventID,
			})
			return
		}

		// 6b. Behavior abuse detection — similarity-aware, adaptive threshold
		if err := CheckBehaviorAbuse(fingerprint, eventID, len(rawBody), clientID); err != nil {
			behaviorAbuseTotal.Inc()
			IncrementRejected(RejectReasonBehavior)
			rejectionLog(RejectReasonBehavior, fingerprint, eventID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":    err.Error(),
				"event_id": eventID,
			})
			return
		}

		// 7. Replay hard lock for critical events (24h persistent dedup)
		if IsCritical(event) {
			if err := CheckCriticalDedup(eventID); err != nil {
				trackingDedupBlocked.Inc()
				IncrementRejected(RejectReasonReplay)
				rejectionLog(RejectReasonReplay, fingerprint, eventID)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":    err.Error(),
					"event_id": eventID,
				})
				return
			}
		}

		// 8. Standard dedup check (reject replayed events within 5-min window)
		if err := CheckDedup(eventID); err != nil {
			trackingDedupBlocked.Inc()
			IncrementRejected(RejectReasonDedup)
			rejectionLog(RejectReasonDedup, fingerprint, eventID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":    err.Error(),
				"event_id": eventID,
			})
			return
		}

		// 9. Validate and process
		if err := TrackSkillEvent(event); err != nil {
			IncrementRejected(RejectReasonInvalid)
			rejectionLog(RejectReasonInvalid, fingerprint, eventID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// 10. Success — mark alive and respond
		SetRealUsageAlive()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "ok",
			"event_id": eventID,
		})
	}
}
