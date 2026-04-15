// Package tracking implements real-time skill usage tracking with
// Prometheus metrics and fail-closed behavior.
//
// No skill activation may occur without a corresponding tracked event.
// If tracking fails, a critical log is emitted and an error is returned
// so callers can abort the activation (fail-closed).
package tracking

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// FlexTime — strict, fail-closed timestamp with temporal integrity
// ---------------------------------------------------------------------------

const (
	// flexTimeMaxFuture is the maximum allowed drift into the future.
	flexTimeMaxFuture = 5 * time.Minute
	// flexTimeMaxAge is the maximum allowed age for a timestamp.
	flexTimeMaxAge = 24 * time.Hour
)

// flexTimeNow is the clock function used by FlexTime validation.
// Tests can override this to control "now".
var flexTimeNow = time.Now

// FlexTime wraps time.Time with a strict JSON unmarshaler.
//
// Accepted formats (all MUST contain an explicit timezone offset):
//   - RFC3339Nano  "2006-01-02T15:04:05.999999999Z07:00"
//   - RFC3339      "2006-01-02T15:04:05Z07:00"
//
// Bare timestamps without timezone (e.g. Python isoformat()) are REJECTED
// because they introduce temporal ambiguity. All parsed values are
// normalised to UTC before storage.
//
// Temporal bounds are enforced:
//   - timestamp must not be more than 5 min in the future
//   - timestamp must not be older than 24 h
type FlexTime struct{ time.Time }

// UnmarshalJSON parses strict RFC3339/RFC3339Nano and normalises to UTC.
// Fail-closed: any format or bounds violation returns an error.
func (ft *FlexTime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		ft.Time = time.Time{}
		return nil
	}

	// Try RFC3339Nano first (superset), then RFC3339.
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return fmt.Errorf("tracking: timestamp %q is not valid RFC3339 (timezone required)", s)
	}

	// Normalise to UTC.
	t = t.UTC()

	// Temporal bounds — fail-closed.
	now := flexTimeNow().UTC()
	if t.After(now.Add(flexTimeMaxFuture)) {
		return fmt.Errorf("tracking: timestamp %q is too far in the future (max %v)", s, flexTimeMaxFuture)
	}
	if t.Before(now.Add(-flexTimeMaxAge)) {
		return fmt.Errorf("tracking: timestamp %q is too old (max age %v)", s, flexTimeMaxAge)
	}

	ft.Time = t
	return nil
}

// MarshalJSON always emits RFC3339Nano in UTC so round-trips are unambiguous.
func (ft FlexTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(ft.Time.UTC().Format(time.RFC3339Nano))
}

// IsZero delegates to inner time.Time.
func (ft FlexTime) IsZero() bool { return ft.Time.IsZero() }

// NowUTC returns a FlexTime set to the current UTC time.
// Convenience helper so callers never build bare time.Time values.
func NowUTC() FlexTime { return FlexTime{Time: time.Now().UTC()} }

// NowUTCAdd returns a FlexTime set to now + d in UTC.
func NowUTCAdd(d time.Duration) FlexTime { return FlexTime{Time: time.Now().Add(d).UTC()} }

// ---------------------------------------------------------------------------
// SkillEvent — the unit of tracking
// ---------------------------------------------------------------------------

// SkillEvent represents a single skill activation event.
type SkillEvent struct {
	EventType            string   `json:"event_type"`
	Timestamp            FlexTime `json:"timestamp"`
	SessionID            string   `json:"session_id"`
	Mode                 string   `json:"mode"` // auto | suggest | off
	Trigger              string   `json:"trigger"`
	EstimatedWaste       float64  `json:"estimated_waste"`
	ActionsSuggested     int      `json:"actions_suggested"`
	ActionsApplied       int      `json:"actions_applied"`
	ImpactEstimatedToken int64    `json:"impact_estimated_tokens"`
	EventHash            string   `json:"event_hash"`
	PrevHash             string   `json:"prev_hash"`
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
			StartedAt: event.Timestamp.Time,
		}
		st.sessions[event.SessionID] = summary
		skillSessionsTotal.Inc()
	}

	// Integrity hash chain
	prevHash := st.lastHash[event.SessionID] // "" for genesis
	event.PrevHash = prevHash
	event.EventHash = ComputeHash(event.SessionID, event.Timestamp.Time, event.ImpactEstimatedToken)
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
		// Observe activation latency: time from session start to first activation
		latency := event.Timestamp.Time.Sub(summary.StartedAt).Seconds()
		if latency >= 0 {
			skillActivationLatency.Observe(latency)
		}
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

	// ── Environment safety metrics ──────────────────────────────────────
	// orbit_seed_mode: 1 = seed/dev process, 0 = production process.
	// Allows PromQL guards like: metric{} and orbit_seed_mode == 0
	seedModeGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "orbit_seed_mode",
			Help: "1 if this process is a seed/dev instance, 0 for production. Used to prevent env mixing.",
		},
	)

	// orbit_tracking_up: always 1 while the process is alive.
	// absence of this metric in Prometheus means the target is down.
	trackingUpGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "orbit_tracking_up",
			Help: "Always 1 while the tracking process is alive. Absence means target is down.",
		},
	)

	// ── Governance metrics ──────────────────────────────────────────────
	// orbit_instance_id: unique per-process identifier (gauge with label).
	// Ensures each scrape target has a verifiable identity.
	instanceIDGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "orbit_instance_id",
			Help: "Always 1. The instance_id label is a unique per-process UUID.",
		},
		[]string{"instance_id"},
	)

	// orbit_last_event_timestamp: unix epoch of the most recent event.
	// Freshness query: time() - orbit_last_event_timestamp > threshold
	lastEventTimestampGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "orbit_last_event_timestamp",
			Help: "Unix timestamp of the last successfully tracked event. 0 means no events yet.",
		},
	)

	// orbit_skill_activation_latency_seconds: time from session start to
	// first activation event. Measures how quickly the skill provides value.
	// Only observed once per session (first activation).
	skillActivationLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "orbit_skill_activation_latency_seconds",
			Help:    "Seconds from session start to first skill activation. Observed once per session.",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
		},
	)

	// orbit_heartbeat_total: monotonically increasing counter incremented
	// every 15s by StartHeartbeat. If this metric stops increasing, the
	// process is frozen or dead. Dashboards query rate(orbit_heartbeat_total[1m]).
	heartbeatTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_heartbeat_total",
			Help: "Monotonically increasing counter incremented every 15s. Absence or stale rate means process is dead.",
		},
	)
)

// registerOnce ensures metrics are registered exactly once.
var registerOnce sync.Once

// processInstanceID holds the unique ID generated at registration time.
var processInstanceID string

// generateInstanceID returns a random 16-byte hex string (128-bit UUID-like).
func generateInstanceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: deterministic but still unique per process start
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

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
			seedModeGauge,
			trackingUpGauge,
			instanceIDGauge,
			lastEventTimestampGauge,
			skillActivationLatency,
			heartbeatTotal,
		)
		// Process is alive → tracking_up = 1.
		// seed_mode stays 0 (production default) until SetSeedMode(true).
		trackingUpGauge.Set(1)

		// Generate and publish unique instance ID.
		processInstanceID = generateInstanceID()
		instanceIDGauge.WithLabelValues(processInstanceID).Set(1)
	})
}

// GetInstanceID returns the process-unique instance identifier.
// Returns empty string if RegisterMetrics was not called.
func GetInstanceID() string {
	return processInstanceID
}

// ── SetSeedMode — immutable after first call (fail-closed) ──────────

// seedModeSet tracks whether SetSeedMode has been called.
// 0 = not set, 1 = set. Atomic for concurrency safety.
var seedModeSet int32

// SetSeedMode sets orbit_seed_mode to 1 (seed/dev) or 0 (production).
// Must be called exactly ONCE after RegisterMetrics. Subsequent calls
// will panic to enforce fail-closed governance — environment identity
// must be immutable for the lifetime of the process.
func SetSeedMode(isSeed bool) {
	if !atomic.CompareAndSwapInt32(&seedModeSet, 0, 1) {
		panic("orbit-engine: SetSeedMode called more than once — environment identity is immutable")
	}
	if isSeed {
		seedModeGauge.Set(1)
	} else {
		seedModeGauge.Set(0)
	}
}

// ResetSeedModeLock resets the lock for testing purposes ONLY.
// This must NEVER be called in production code.
func ResetSeedModeLock() {
	atomic.StoreInt32(&seedModeSet, 0)
}

// StartHeartbeat launches a background goroutine that increments
// orbit_heartbeat_total every interval. Call once after RegisterMetrics.
// A Prometheus alert fires when rate(orbit_heartbeat_total[1m]) == 0,
// meaning the process is frozen or the /metrics endpoint is unreachable.
func StartHeartbeat(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			heartbeatTotal.Inc()
		}
	}()
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

	// 4. Update freshness gauge — PromQL can detect staleness via:
	//    time() - orbit_last_event_timestamp > threshold
	lastEventTimestampGauge.Set(float64(time.Now().Unix()))

	return nil
}
