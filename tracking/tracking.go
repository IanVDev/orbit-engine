// Package tracking implements real-time skill usage tracking with
// Prometheus metrics and fail-closed behavior.
//
// No skill activation may occur without a corresponding tracked event.
// If tracking fails, a critical log is emitted and an error is returned
// so callers can abort the activation (fail-closed).
package tracking

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// SkillEvent — the unit of tracking
// ---------------------------------------------------------------------------

// SkillEvent represents a single skill activation event.
type SkillEvent struct {
	EventType            string    `json:"event_type"`
	Timestamp            time.Time `json:"timestamp"`
	SessionID            string    `json:"session_id"`
	Mode                 string    `json:"mode"` // auto | suggest | off
	Trigger              string    `json:"trigger"`
	EstimatedWaste       float64   `json:"estimated_waste"`
	ActionsSuggested     int       `json:"actions_suggested"`
	ActionsApplied       int       `json:"actions_applied"`
	ImpactEstimatedToken int64     `json:"impact_estimated_tokens"`
	EventHash            string    `json:"event_hash"`
	PrevHash             string    `json:"prev_hash"`
}

// Validate returns an error if required fields are missing.
func (e SkillEvent) Validate() error {
	if e.EventType == "" {
		return fmt.Errorf("tracking: event_type is required")
	}
	if e.SessionID == "" {
		return fmt.Errorf("tracking: session_id is required")
	}
	if e.Mode == "" {
		return fmt.Errorf("tracking: mode is required")
	}
	switch e.Mode {
	case "auto", "suggest", "off":
		// ok
	default:
		return fmt.Errorf("tracking: invalid mode %q (want auto|suggest|off)", e.Mode)
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("tracking: timestamp is required")
	}
	return nil
}

// ComputeHash returns sha256(session_id + timestamp + impact_estimated_tokens).
func ComputeHash(sessionID string, ts time.Time, tokens int64) string {
	data := fmt.Sprintf("%s|%s|%d", sessionID, ts.Format(time.RFC3339Nano), tokens)
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}

// ---------------------------------------------------------------------------
// SessionSummary — lifecycle of a session
// ---------------------------------------------------------------------------

// SessionSummary aggregates all events within a single session.
type SessionSummary struct {
	SessionID        string       `json:"session_id"`
	StartedAt        time.Time    `json:"started_at"`
	Events           []SkillEvent `json:"events"`
	TotalTokensSaved int64        `json:"total_tokens_saved"`
	AvgWaste         float64      `json:"avg_waste"`
	SkillActivated   bool         `json:"skill_activated"`
}

// _NoSkillThreshold is the minimum number of events in a session
// before we flag the absence of a skill activation.
const _NoSkillThreshold = 20

// ---------------------------------------------------------------------------
// SessionTracker — in-memory session state
// ---------------------------------------------------------------------------

// SessionTracker manages session state and detects sessions without
// skill activation.  It is safe for concurrent use.
type SessionTracker struct {
	mu       sync.Mutex
	sessions map[string]*SessionSummary
	// lastHash stores the most recent event hash per session for chaining.
	lastHash map[string]string
}

// NewSessionTracker creates a ready-to-use tracker.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		sessions: make(map[string]*SessionSummary),
		lastHash: make(map[string]string),
	}
}

// RecordEvent adds an event to the session, computes the integrity hash
// chain, updates Prometheus session metrics, and detects sessions without
// skill activation.  Returns the enriched event (with hashes) or an error.
func (st *SessionTracker) RecordEvent(event SkillEvent) (SkillEvent, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Initialise session on first event
	summary, exists := st.sessions[event.SessionID]
	if !exists {
		summary = &SessionSummary{
			SessionID: event.SessionID,
			StartedAt: event.Timestamp,
		}
		st.sessions[event.SessionID] = summary
		skillSessionsTotal.Inc()
	}

	// Integrity hash chain
	prevHash := st.lastHash[event.SessionID] // "" for genesis
	event.PrevHash = prevHash
	event.EventHash = ComputeHash(event.SessionID, event.Timestamp, event.ImpactEstimatedToken)
	st.lastHash[event.SessionID] = event.EventHash

	// Track via existing fail-closed path
	if err := TrackSkillEvent(event); err != nil {
		return event, err
	}

	// Accumulate in summary
	summary.Events = append(summary.Events, event)
	summary.TotalTokensSaved += event.ImpactEstimatedToken

	// Recalculate average waste
	var totalWaste float64
	for _, e := range summary.Events {
		totalWaste += e.EstimatedWaste
	}
	summary.AvgWaste = totalWaste / float64(len(summary.Events))

	// Detect skill activation (any event with type "activation")
	if event.EventType == "activation" && !summary.SkillActivated {
		summary.SkillActivated = true
		skillSessionsWithActivation.Inc()
	}

	// Detect session without skill after threshold
	if len(summary.Events) == _NoSkillThreshold && !summary.SkillActivated {
		skillSessionsWithoutActivation.Inc()
		log.Printf("[WARN] session %s has %d events without skill activation",
			event.SessionID, _NoSkillThreshold)
	}

	return event, nil
}

// GetSession returns a copy of the session summary, or nil if not found.
func (st *SessionTracker) GetSession(sessionID string) *SessionSummary {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sessions[sessionID]
	if !ok {
		return nil
	}
	// Return a shallow copy
	cp := *s
	cp.Events = make([]SkillEvent, len(s.Events))
	copy(cp.Events, s.Events)
	return &cp
}

// GetLastHash returns the last event hash for a session (for verification).
func (st *SessionTracker) GetLastHash(sessionID string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.lastHash[sessionID]
}

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var (
	skillActivationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_skill_activations_total",
			Help: "Total number of skill activations.",
		},
		[]string{"mode"},
	)

	skillTokensSavedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_skill_tokens_saved_total",
			Help: "Cumulative estimated tokens saved by the skill.",
		},
	)

	skillWasteEstimated = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "orbit_skill_waste_estimated",
			Help: "Latest estimated waste value from the most recent activation.",
		},
	)

	skillTrackingFailuresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_skill_tracking_failures_total",
			Help: "Total number of tracking failures (critical).",
		},
	)

	skillSessionsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_skill_sessions_total",
			Help: "Total number of tracked sessions.",
		},
	)

	skillSessionsWithActivation = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_skill_sessions_with_activation_total",
			Help: "Sessions where the skill was activated at least once.",
		},
	)

	skillSessionsWithoutActivation = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_skill_sessions_without_activation_total",
			Help: "Sessions that exceeded the event threshold without any skill activation.",
		},
	)
)

// registerOnce ensures metrics are registered exactly once.
var registerOnce sync.Once

// RegisterMetrics registers all Prometheus collectors on the given
// registerer. Pass prometheus.DefaultRegisterer for production use
// or a custom registry for tests.
func RegisterMetrics(reg prometheus.Registerer) {
	registerOnce.Do(func() {
		reg.MustRegister(
			skillActivationsTotal,
			skillTokensSavedTotal,
			skillWasteEstimated,
			skillTrackingFailuresTotal,
			skillSessionsTotal,
			skillSessionsWithActivation,
			skillSessionsWithoutActivation,
		)
	})
}

// ---------------------------------------------------------------------------
// TrackSkillEvent — fail-closed tracking
// ---------------------------------------------------------------------------

// TrackSkillEvent records a SkillEvent to Prometheus metrics and emits a
// structured JSON log line. It returns a non-nil error if anything fails,
// in which case the caller MUST abort the skill activation (fail-closed).
func TrackSkillEvent(event SkillEvent) error {
	// 1. Validate
	if err := event.Validate(); err != nil {
		skillTrackingFailuresTotal.Inc()
		log.Printf("[CRITICAL] tracking validation failed: %v", err)
		return err
	}

	// 2. Record Prometheus metrics.
	//    Wrap in a recover so a panic in the prom client
	//    doesn't crash the caller — it becomes a returned error.
	func() {
		defer func() {
			if r := recover(); r != nil {
				// will be caught by the outer error path
				panic(r)
			}
		}()
		skillActivationsTotal.WithLabelValues(event.Mode).Inc()
		if event.ImpactEstimatedToken > 0 {
			skillTokensSavedTotal.Add(float64(event.ImpactEstimatedToken))
		}
		skillWasteEstimated.Set(event.EstimatedWaste)
	}()

	// 3. Structured log
	logLine, err := json.Marshal(event)
	if err != nil {
		skillTrackingFailuresTotal.Inc()
		log.Printf("[CRITICAL] tracking serialisation failed: %v", err)
		return fmt.Errorf("tracking: failed to serialise event: %w", err)
	}
	log.Printf("[TRACK] %s", logLine)

	return nil
}
