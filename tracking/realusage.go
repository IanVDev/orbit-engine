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
func TrackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var event SkillEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = NowUTC()
		}
		if err := TrackSkillEvent(event); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
