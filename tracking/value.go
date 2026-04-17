// value.go — User-perceived value observability for orbit-engine.
//
// Measures value from the user's perspective, beyond raw technical metrics.
//
// Tracks:
//   - orbit_user_perceived_value_total{level}        — perceived value events (high/medium/low)
//   - orbit_user_returned_total{fingerprint}          — retention signal per pseudonymous user
//   - orbit_user_accepted_suggestion_total            — suggestions accepted
//   - orbit_user_ignored_suggestion_total             — suggestions ignored
//   - orbit_user_ignore_reason_total{reason}          — WHY suggestions were ignored
//
// Fingerprinting:
//   - UserFingerprint(sessionID) = sha256(session_id + salt)[:16]
//   - Salt is a package constant — consistent across calls, opaque to outsiders.
//   - Fingerprint is used ONLY on the returned-total metric to detect returning users
//     without storing raw session IDs in metric labels.
//
// Ignore-reason heuristics (applied only when ActionsApplied == 0):
//   - low_confidence:    EstimatedWaste < 0.05 AND ImpactEstimatedToken < 100
//   - no_perceived_value: ImpactEstimatedToken == 0
//   - latency:           Mode == "suggest" (suggestion shown but not immediately acted on)
//   - unknown:           fallback for all other cases
//
// Design:
//   - Fail-closed: invalid level or unclassifiable event → nothing recorded; error returned.
//   - No partial events: if any step fails, the whole recording is aborted.
//   - Structured JSONL log ([VALUE] prefix) for every decision.
//   - Thread-safe: only Prometheus counters and atomic-safe log calls used.
//
// Integration:
//   - Call RegisterValueMetrics(reg) once at startup.
//   - TrackSkillEvent automatically calls RecordEventValue to auto-classify.
//   - For manual control use RecordPerceivedValue / RecordSuggestionIgnored / etc.
package tracking

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// Value level constants
// ---------------------------------------------------------------------------

// ValueLevel is a discrete tier of perceived value.
type ValueLevel string

const (
	// ValueHigh means all suggested actions were applied (full acceptance).
	ValueHigh ValueLevel = "high"
	// ValueMedium means some (but not all) suggested actions were applied.
	ValueMedium ValueLevel = "medium"
	// ValueLow means no suggested actions were applied (ignored).
	ValueLow ValueLevel = "low"
)

// validValueLevels is the closed set of accepted levels.
// Any level outside this set is rejected (fail-closed).
var validValueLevels = map[ValueLevel]bool{
	ValueHigh:   true,
	ValueMedium: true,
	ValueLow:    true,
}

// ---------------------------------------------------------------------------
// User fingerprint — pseudonymous identity for retention tracking
// ---------------------------------------------------------------------------

// valueFingerprintSalt is mixed into every fingerprint hash.
// It is stable (constant) so fingerprints for the same session_id are always
// identical, and opaque (secret-free) — it just prevents trivial reversal.
const valueFingerprintSalt = "orbit-value-v1"

// UserFingerprint returns a 16-character hex string derived from
// sha256(sessionID + valueFingerprintSalt). It is deterministic and consistent:
// the same session_id always produces the same fingerprint within this binary.
//
// Purpose: label orbit_user_returned_total without storing raw session IDs.
func UserFingerprint(sessionID string) string {
	h := sha256.New()
	h.Write([]byte(sessionID + valueFingerprintSalt))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ---------------------------------------------------------------------------
// Ignore reason — heuristic classification of WHY a suggestion was ignored
// ---------------------------------------------------------------------------

// IgnoreReason is a typed enum of inferred causes for suggestion ignoral.
type IgnoreReason string

const (
	// IgnoreReasonLowConfidence — orbit's waste estimate was very low; user
	// likely didn't trust the signal enough to act.
	IgnoreReasonLowConfidence IgnoreReason = "low_confidence"

	// IgnoreReasonNoPerceivedValue — no token impact was estimated; user
	// saw no tangible benefit in applying the suggestion.
	IgnoreReasonNoPerceivedValue IgnoreReason = "no_perceived_value"

	// IgnoreReasonLatency — skill ran in suggest mode; suggestion was shown
	// but the user did not act on it in this session (latency proxy).
	IgnoreReasonLatency IgnoreReason = "latency"

	// IgnoreReasonUnknown — fallback when no other heuristic applies.
	IgnoreReasonUnknown IgnoreReason = "unknown"
)

// validIgnoreReasons is the closed set of accepted reasons.
var validIgnoreReasons = map[IgnoreReason]bool{
	IgnoreReasonLowConfidence:    true,
	IgnoreReasonNoPerceivedValue: true,
	IgnoreReasonLatency:          true,
	IgnoreReasonUnknown:          true,
}

// lowConfidenceWasteThreshold is the EstimatedWaste cutoff for low_confidence.
const lowConfidenceWasteThreshold = 0.05

// lowConfidenceTokenThreshold is the ImpactEstimatedToken cutoff for low_confidence.
const lowConfidenceTokenThreshold = 100

// InferIgnoreReason returns the most likely reason a suggestion was ignored,
// inferred heuristically from the SkillEvent fields.
//
// Rules (evaluated in order — first match wins):
//  1. low_confidence:    EstimatedWaste < 0.05 AND ImpactEstimatedToken < 100
//  2. no_perceived_value: ImpactEstimatedToken == 0
//  3. latency:           Mode == "suggest"
//  4. unknown:           fallback
//
// Only call when ActionsApplied == 0; the result is meaningless otherwise.
func InferIgnoreReason(e SkillEvent) IgnoreReason {
	// Rule 1: very low waste AND very low token impact → orbit wasn't confident.
	if e.EstimatedWaste < lowConfidenceWasteThreshold &&
		e.ImpactEstimatedToken < lowConfidenceTokenThreshold {
		return IgnoreReasonLowConfidence
	}

	// Rule 2: no token impact estimated → no tangible value perceived.
	if e.ImpactEstimatedToken == 0 {
		return IgnoreReasonNoPerceivedValue
	}

	// Rule 3: suggest mode → suggestion was presented but not acted on immediately.
	if e.Mode == "suggest" {
		return IgnoreReasonLatency
	}

	// Rule 4: fallback.
	return IgnoreReasonUnknown
}

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var (
	// orbit_user_perceived_value_total{level} — counter per value tier.
	userPerceivedValueTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_user_perceived_value_total",
			Help: "Total perceived-value events by level (high/medium/low). High = all suggestions accepted.",
		},
		[]string{"level"},
	)

	// orbit_user_returned_total{fingerprint} — retention signal per pseudonymous user.
	// fingerprint = UserFingerprint(session_id) — deterministic, non-reversible.
	userReturnedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_user_returned_total",
			Help: "Total sessions where the user returned for a new interaction. Label: fingerprint (sha256-derived, pseudonymous).",
		},
		[]string{"fingerprint"},
	)

	// orbit_user_accepted_suggestion_total — at least one action was applied.
	userAcceptedSuggestionTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_user_accepted_suggestion_total",
			Help: "Total skill suggestions accepted (actions_applied > 0) by the user.",
		},
	)

	// orbit_user_ignored_suggestion_total — no actions applied despite suggestions.
	userIgnoredSuggestionTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_user_ignored_suggestion_total",
			Help: "Total skill suggestions ignored (actions_applied == 0) by the user.",
		},
	)

	// orbit_user_ignore_reason_total{reason} — WHY suggestions were ignored.
	// reason is inferred heuristically by InferIgnoreReason.
	userIgnoreReasonTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_user_ignore_reason_total",
			Help: "Total ignored suggestions by inferred reason (low_confidence|no_perceived_value|latency|unknown).",
		},
		[]string{"reason"},
	)
)

// RegisterValueMetrics registers all value-observability Prometheus collectors
// on the provided registerer. Call once at startup, after RegisterMetrics.
// Each test that needs isolated metrics should call this on a fresh registry.
func RegisterValueMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		userPerceivedValueTotal,
		userReturnedTotal,
		userAcceptedSuggestionTotal,
		userIgnoredSuggestionTotal,
		userIgnoreReasonTotal,
	)
}

// ---------------------------------------------------------------------------
// Structured JSONL log
// ---------------------------------------------------------------------------

// valueLogEntry is the JSONL schema emitted for every value decision.
type valueLogEntry struct {
	Timestamp    string `json:"timestamp"`
	Event        string `json:"event"`
	Level        string `json:"level,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	IgnoreReason string `json:"ignore_reason,omitempty"`
}

// emitValueLog writes a single JSONL line to the process log ([VALUE] prefix).
// Thread-safe: log.Printf is already serialised.
func emitValueLog(event, level, sessionID string) {
	entry := valueLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Event:     event,
		Level:     level,
		SessionID: sessionID,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[VALUE][WARN] failed to marshal log entry: %v", err)
		return
	}
	log.Printf("[VALUE] %s", line)
}

// emitValueLogFull writes a JSONL line with all optional fields populated.
func emitValueLogFull(event, level, sessionID, fingerprint, ignoreReason string) {
	entry := valueLogEntry{
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		Event:        event,
		Level:        level,
		SessionID:    sessionID,
		Fingerprint:  fingerprint,
		IgnoreReason: ignoreReason,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[VALUE][WARN] failed to marshal log entry: %v", err)
		return
	}
	log.Printf("[VALUE] %s", line)
}

// ---------------------------------------------------------------------------
// Public API — fail-closed
// ---------------------------------------------------------------------------

// RecordPerceivedValue records a perceived-value event at the given level.
//
// Fail-closed: unknown levels are rejected and NOTHING is recorded.
// Returns a non-nil error if level is invalid so callers can abort.
func RecordPerceivedValue(level ValueLevel, sessionID string) error {
	if !validValueLevels[level] {
		return fmt.Errorf("value: unknown level %q (want high|medium|low)", level)
	}
	userPerceivedValueTotal.WithLabelValues(string(level)).Inc()
	emitValueLog("perceived_value", string(level), sessionID)
	return nil
}

// RecordUserReturned records that a user started a new session after a previous one.
// The fingerprint label is derived from sessionID so retention can be tracked
// per pseudonymous user without storing raw session IDs in metric labels.
func RecordUserReturned(sessionID string) {
	fp := UserFingerprint(sessionID)
	userReturnedTotal.WithLabelValues(fp).Inc()
	emitValueLogFull("user_returned", "", sessionID, fp, "")
}

// RecordSuggestionAccepted records that the user applied at least one suggested action.
func RecordSuggestionAccepted(sessionID string) {
	userAcceptedSuggestionTotal.Inc()
	emitValueLog("suggestion_accepted", "", sessionID)
}

// RecordSuggestionIgnored records that the user applied no suggested actions,
// and increments the ignore-reason counter with the inferred reason.
//
// Fail-closed: if reason is not in the valid set, nothing is recorded and an
// error is returned. Call InferIgnoreReason to produce a valid reason.
func RecordSuggestionIgnored(sessionID string, reason IgnoreReason) error {
	if !validIgnoreReasons[reason] {
		return fmt.Errorf("value: unknown ignore reason %q", reason)
	}
	userIgnoredSuggestionTotal.Inc()
	userIgnoreReasonTotal.WithLabelValues(string(reason)).Inc()
	emitValueLogFull("suggestion_ignored", "", sessionID, "", string(reason))
	return nil
}

// ---------------------------------------------------------------------------
// Auto-classification from SkillEvent
// ---------------------------------------------------------------------------

// ClassifyEventValue infers a perceived value level from a SkillEvent:
//
//   - high:   ActionsApplied == ActionsSuggested (full acceptance, >= 1)
//   - medium: ActionsApplied > 0 but < ActionsSuggested (partial)
//   - low:    ActionsApplied == 0 (all suggestions ignored)
//
// Returns ("", nil) when ActionsSuggested == 0 (no suggestions → cannot classify).
// Fail-closed: the empty level causes RecordEventValue to skip recording entirely.
func ClassifyEventValue(e SkillEvent) (ValueLevel, error) {
	if e.ActionsSuggested <= 0 {
		// No suggestions were made — not classifiable. Not an error.
		return "", nil
	}
	switch {
	case e.ActionsApplied >= e.ActionsSuggested:
		return ValueHigh, nil
	case e.ActionsApplied > 0:
		return ValueMedium, nil
	default:
		return ValueLow, nil
	}
}

// RecordEventValue auto-classifies value from a SkillEvent and records
// the appropriate metrics + log entries.
//
// Fail-closed:
//   - If no suggestions were made (ActionsSuggested == 0), nothing is recorded.
//   - If classification returns an unknown level, nothing is recorded.
//   - If RecordPerceivedValue rejects the level, a [VALUE][WARN] log is emitted
//     but the function returns without panicking.
//   - If ActionsApplied == 0 and InferIgnoreReason cannot produce a valid reason,
//     the ignore-reason metric is NOT recorded (no partial recording).
//
// Concurrency: safe to call from multiple goroutines.
func RecordEventValue(e SkillEvent) {
	level, err := ClassifyEventValue(e)
	if err != nil || level == "" {
		// Cannot classify → fail-closed, no partial recording.
		return
	}

	if err := RecordPerceivedValue(level, e.SessionID); err != nil {
		// Should never happen (ClassifyEventValue only returns valid levels),
		// but guard defensively.
		log.Printf("[VALUE][WARN] RecordPerceivedValue failed: %v", err)
		return
	}

	// Record suggestion engagement signal.
	if e.ActionsApplied > 0 {
		RecordSuggestionAccepted(e.SessionID)
	} else {
		// Infer WHY the suggestion was ignored.
		reason := InferIgnoreReason(e)
		if err := RecordSuggestionIgnored(e.SessionID, reason); err != nil {
			// Fail-closed: unknown reason → do not record the ignore metric.
			log.Printf("[VALUE][WARN] RecordSuggestionIgnored failed: %v", err)
		}
	}
}

