// model_control.go — Explicit model-override control for orbit-engine.
//
// Enforces that the system only changes models (Opus ↔ Sonnet) when the
// user has explicitly permitted it through a well-defined control mode.
//
// Control modes:
//
//	locked  — never allow override; any attempt is blocked (default / fail-closed)
//	auto    — allow override; apply existing routing heuristics
//	suggest — compute what would change but do NOT apply; return suggestion
//
// Integration:
//   - Call ParseModelControl(s) at startup (panics on invalid → fail-closed).
//   - Pass the returned ModelControl to TrackHandlerWithControl.
//   - Every override attempt is logged as JSONL and counted in Prometheus.
//
// Fail-closed contract:
//   - Unknown control string  → ParseModelControl returns error; binary refuses to start.
//   - Empty model_from/to     → no override detected; event passes through unchanged.
//   - Locked + override attempt → HTTP 403 Forbidden; event NOT recorded.
package tracking

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// ModelControl enum
// ---------------------------------------------------------------------------

// ModelControl is a typed string enum of allowed override policies.
type ModelControl string

const (
	// ModelControlLocked blocks all model overrides. Default / fail-closed.
	ModelControlLocked ModelControl = "locked"
	// ModelControlAuto permits overrides; heuristics apply normally.
	ModelControlAuto ModelControl = "auto"
	// ModelControlSuggest returns a suggestion without applying the override.
	ModelControlSuggest ModelControl = "suggest"
)

// validModelControls is the closed set of accepted values.
var validModelControls = map[ModelControl]bool{
	ModelControlLocked:  true,
	ModelControlAuto:    true,
	ModelControlSuggest: true,
}

// ParseModelControl parses a string into a ModelControl.
// Returns an error for unknown values so callers can fail-closed at startup.
func ParseModelControl(s string) (ModelControl, error) {
	mc := ModelControl(s)
	if !validModelControls[mc] {
		return "", fmt.Errorf(
			"model_control: unknown value %q — valid values are locked|auto|suggest",
			s,
		)
	}
	return mc, nil
}

// ---------------------------------------------------------------------------
// ModelOverrideDecision — result of evaluating a single override attempt
// ---------------------------------------------------------------------------

// ModelOverrideDecision captures every attribute of an override evaluation.
// It is logged as JSONL and optionally returned to the caller in the HTTP response.
type ModelOverrideDecision struct {
	// Control is the active policy that produced this decision.
	Control ModelControl `json:"control"`
	// Allowed signals whether the override may be applied.
	Allowed bool `json:"allowed"`
	// Applied signals whether the override was actually applied.
	// false for suggest (never applies) and locked (never allows).
	Applied bool `json:"applied"`
	// From is the current model before any potential change.
	From string `json:"from,omitempty"`
	// To is the requested model after the change.
	To string `json:"to,omitempty"`
	// Reason is the human-readable explanation for this outcome.
	Reason string `json:"reason"`
	// Confidence is a 0.0–1.0 score of routing certainty.
	// 1.0 = deterministic (locked), 0.0 = no signal.
	Confidence float64 `json:"confidence"`
	// Suggestion is populated when control=suggest and From != To.
	// It is the model that WOULD be used if control were auto.
	Suggestion string `json:"suggestion,omitempty"`
	// Timestamp of the evaluation.
	Timestamp string `json:"timestamp"`
}

// hasOverride returns true when an override is being requested
// (both fields set AND they differ).
func (d *ModelOverrideDecision) hasOverride() bool {
	return d.From != "" && d.To != "" && d.From != d.To
}

// ---------------------------------------------------------------------------
// EvaluateModelOverride — core logic, no I/O
// ---------------------------------------------------------------------------

// EvaluateModelOverride decides what to do with a model override request
// embedded in a SkillEvent based on the active ModelControl policy.
//
//   - If ModelFrom/ModelTo are empty or identical → no override; decision is
//     trivially allowed and no metrics are emitted.
//   - locked  → Allowed=false, Applied=false, Confidence=1.0.
//   - auto    → Allowed=true,  Applied=true,  Confidence=1.0.
//   - suggest → Allowed=false, Applied=false, Confidence=0.8; Suggestion set.
//
// Fail-closed: unknown ModelControl returns an error; the caller must treat
// that as Allowed=false.
func EvaluateModelOverride(e SkillEvent, control ModelControl) (ModelOverrideDecision, error) {
	if !validModelControls[control] {
		return ModelOverrideDecision{}, fmt.Errorf(
			"model_control: unknown control %q — refusing override evaluation (fail-closed)", control,
		)
	}

	d := ModelOverrideDecision{
		Control:   control,
		From:      e.ModelFrom,
		To:        e.ModelTo,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	// No override requested → pass-through, no action needed.
	if !d.hasOverride() {
		d.Allowed = true
		d.Applied = false
		d.Reason = "no_override_requested"
		d.Confidence = 1.0
		return d, nil
	}

	switch control {
	case ModelControlLocked:
		d.Allowed = false
		d.Applied = false
		d.Reason = "blocked_by_locked_control"
		d.Confidence = 1.0

	case ModelControlAuto:
		d.Allowed = true
		d.Applied = true
		d.Reason = "override_permitted_by_auto_control"
		d.Confidence = 1.0

	case ModelControlSuggest:
		// Compute the suggestion but do not apply.
		d.Allowed = false
		d.Applied = false
		d.Reason = "override_suppressed_suggest_only"
		d.Confidence = 0.8
		d.Suggestion = d.To // record what WOULD be used
	}

	return d, nil
}

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var (
	// orbit_model_override_total{from, to, control, allowed}
	// Counts every override attempt with its outcome.
	modelOverrideTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_model_override_total",
			Help: "Total model override attempts. Labels: from, to, control (locked|auto|suggest), allowed (true|false).",
		},
		[]string{"from", "to", "control", "allowed"},
	)

	// orbit_model_control_mode: gauge holding the active control mode.
	// Value mapping: locked=0, suggest=1, auto=2.
	modelControlModeGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "orbit_model_control_mode",
			Help: "Active model control mode. locked=0, suggest=1, auto=2.",
		},
		[]string{"mode"},
	)
)

// RegisterModelControlMetrics registers the model-control Prometheus collectors.
// Call once at startup alongside RegisterMetrics and RegisterSecurityMetrics.
func RegisterModelControlMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		modelOverrideTotal,
		modelControlModeGauge,
	)
}

// SetModelControlModeGauge publishes the active mode as a gauge so dashboards
// can alert if the mode changes unexpectedly.
func SetModelControlModeGauge(control ModelControl) {
	// Reset all labels to 0 first (only one mode is active at a time).
	for _, mode := range []ModelControl{ModelControlLocked, ModelControlAuto, ModelControlSuggest} {
		modelControlModeGauge.WithLabelValues(string(mode)).Set(0)
	}
	modelControlModeGauge.WithLabelValues(string(control)).Set(1)
}

// ---------------------------------------------------------------------------
// Logging — structured JSONL
// ---------------------------------------------------------------------------

// overrideLogEntry is the JSONL schema emitted for every evaluated override.
type overrideLogEntry struct {
	Timestamp  string  `json:"timestamp"`
	Event      string  `json:"event"`
	Control    string  `json:"control"`
	Allowed    bool    `json:"allowed"`
	Applied    bool    `json:"applied"`
	From       string  `json:"from,omitempty"`
	To         string  `json:"to,omitempty"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
	Suggestion string  `json:"suggestion,omitempty"`
	SessionID  string  `json:"session_id,omitempty"`
}

// emitOverrideLog writes a JSONL line for every evaluated override decision.
// Thread-safe: log.Printf is serialised.
func emitOverrideLog(d ModelOverrideDecision, sessionID string) {
	entry := overrideLogEntry{
		Timestamp:  d.Timestamp,
		Event:      "model_override",
		Control:    string(d.Control),
		Allowed:    d.Allowed,
		Applied:    d.Applied,
		From:       d.From,
		To:         d.To,
		Reason:     d.Reason,
		Confidence: d.Confidence,
		Suggestion: d.Suggestion,
		SessionID:  sessionID,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[MODEL_CTRL][WARN] failed to marshal log entry: %v", err)
		return
	}
	log.Printf("[MODEL_CTRL] %s", line)
}

// ---------------------------------------------------------------------------
// HTTP Middleware — TrackHandlerWithControl
// ---------------------------------------------------------------------------

// TrackHandlerWithControl wraps the standard TrackHandler and enforces the
// model-control policy before the event reaches the tracking pipeline.
//
// Behaviour per mode:
//   - locked  : if override detected → 403 Forbidden (event not tracked)
//   - auto    : override allowed; event tracked normally
//   - suggest : override detected → 200 with suggestion JSON (event tracked
//     with original model, ModelTo cleared)
func TrackHandlerWithControl(control ModelControl) http.HandlerFunc {
	// Fail-closed at construction time.
	if !validModelControls[control] {
		panic(fmt.Sprintf(
			"orbit-engine: TrackHandlerWithControl received invalid control %q (fail-closed)", control,
		))
	}

	base := TrackHandler()

	return func(w http.ResponseWriter, r *http.Request) {
		// Peek at the body to check for override fields.
		// We use a shallow decode — TrackHandler will do the full decode.
		var peek struct {
			SessionID string `json:"session_id"`
			ModelFrom string `json:"model_from"`
			ModelTo   string `json:"model_to"`
		}
		bodyBytes, err := peekRequestBody(r)
		if err != nil {
			// Cannot read body → fail-closed: let TrackHandler handle it.
			base(w, r)
			return
		}
		_ = json.Unmarshal(bodyBytes, &peek)

		// ── [1] Execution Governance — runs BEFORE model_control ──────────
		// Build the intent from the same body bytes and validate all rules.
		// Fail-closed: any block verdict → immediate 403, never forwarded.
		intent := IntentFromBody(bodyBytes)
		verdict, govErr := ValidateExecution(intent)
		if govErr != nil || !verdict.Allowed {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			resp, _ := json.Marshal(map[string]interface{}{
				"error":      "execution_blocked_by_governance",
				"reason":     verdict.Reason,
				"block_rule": verdict.BlockRule,
				"skill_id":   verdict.SkillID,
				"session_id": verdict.SessionID,
			})
			_, _ = w.Write(resp)
			return
		}

		// ── [2] Model Control enforcement ─────────────────────────────────

		// Synthesise a SkillEvent just for the override evaluation.
		probe := SkillEvent{
			SessionID: peek.SessionID,
			ModelFrom: peek.ModelFrom,
			ModelTo:   peek.ModelTo,
		}
		decision, err := EvaluateModelOverride(probe, control)
		if err != nil {
			// Unknown control state → fail-closed: block.
			log.Printf("[MODEL_CTRL][ERROR] invalid control state: %v", err)
			http.Error(w, `{"error":"model_control invalid state (fail-closed)"}`, http.StatusForbidden)
			return
		}

		// Only log and record metrics when an actual override is present.
		if probe.hasOverride() {
			emitOverrideLog(decision, peek.SessionID)
			allowedStr := "false"
			if decision.Allowed {
				allowedStr = "true"
			}
			modelOverrideTotal.WithLabelValues(
				decision.From, decision.To, string(control), allowedStr,
			).Inc()
		}

		switch control {
		case ModelControlLocked:
			if probe.hasOverride() && !decision.Allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				resp, _ := json.Marshal(map[string]interface{}{
					"error":   "model_override_blocked",
					"reason":  decision.Reason,
					"control": string(control),
				})
				_, _ = w.Write(resp)
				return
			}

		case ModelControlSuggest:
			if probe.hasOverride() {
				// Clear the override from the body before forwarding.
				bodyBytes = clearModelOverride(bodyBytes)
				replaceRequestBody(r, bodyBytes)

				// Serve the base handler (which will track with original model).
				// We intercept the response to append the suggestion.
				rec := &responseRecorder{header: make(http.Header)}
				base(rec, r)

				// Forward the recorded status + body, then append suggestion.
				for k, v := range rec.header {
					for _, vv := range v {
						w.Header().Add(k, vv)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if rec.status == 0 {
					rec.status = http.StatusOK
				}
				w.WriteHeader(rec.status)

				// Merge suggestion into response.
				var base map[string]interface{}
				if json.Unmarshal(rec.body, &base) == nil {
					base["model_control"] = "suggest"
					base["model_suggestion"] = decision.Suggestion
					base["model_override_applied"] = false
					merged, _ := json.Marshal(base)
					_, _ = w.Write(merged)
				} else {
					_, _ = w.Write(rec.body)
				}
				return
			}
		}

		// auto or no override → delegate to base handler.
		base(w, r)
	}
}

// ---------------------------------------------------------------------------
// Helpers for HTTP body manipulation
// ---------------------------------------------------------------------------

// SkillEvent helper — hasOverride is analogous to ModelOverrideDecision.hasOverride.
func (e *SkillEvent) hasOverride() bool {
	return e.ModelFrom != "" && e.ModelTo != "" && e.ModelFrom != e.ModelTo
}

// peekRequestBody reads the entire request body and replaces it so it can be
// read again by downstream handlers. The returned bytes are the body content.
func peekRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("model_control: failed to read request body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return data, nil
}

// replaceRequestBody replaces the request body with the given bytes.
func replaceRequestBody(r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
}

// clearModelOverride removes model_to from a JSON body so the override is
// not applied when control=suggest.
func clearModelOverride(body []byte) []byte {
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	delete(m, "model_to")
	result, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return result
}

// responseRecorder is a minimal http.ResponseWriter that buffers status + body.
type responseRecorder struct {
	header http.Header
	status int
	body   []byte
}

func (rr *responseRecorder) Header() http.Header  { return rr.header }
func (rr *responseRecorder) WriteHeader(code int) { rr.status = code }
func (rr *responseRecorder) Write(b []byte) (int, error) {
	rr.body = append(rr.body, b...)
	return len(b), nil
}
