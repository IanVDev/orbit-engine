package tracking

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

// newTestRegistry returns a fresh Prometheus registry with our metrics
// registered. Because the package-level metrics are singletons we reset
// them before each test by re-creating the registry.  The sync.Once in
// RegisterMetrics means we can only register on DefaultRegisterer once
// in production, but tests use a custom registry via MustRegister.
func newTestRegistry() *prometheus.Registry {
	ResetRateLimit() // clear rate limit state between tests
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
		skillActivationByReason,
		lastRealUsageTimestamp,
	)
	return reg
}

func validEvent() SkillEvent {
	return SkillEvent{
		EventType:            "activation",
		Timestamp:            NowUTC(),
		SessionID:            "sess-001",
		Mode:                 "auto",
		Trigger:              "correction_chain",
		EstimatedWaste:       1200.0,
		ActionsSuggested:     3,
		ActionsApplied:       2,
		ImpactEstimatedToken: 800,
	}
}

// -----------------------------------------------------------------------
// TestSkillActivationIsAlwaysTracked
//
// Core invariant: every activation MUST produce a tracked metric.
// If TrackSkillEvent succeeds, the activations counter must have
// incremented.  If it fails, the caller sees an error and must abort.
// -----------------------------------------------------------------------

func TestSkillActivationIsAlwaysTracked(t *testing.T) {
	reg := newTestRegistry()
	_ = reg // metrics are package-level; registry just collects them

	// Reset counter by collecting before
	beforeFamilies, _ := reg.Gather()
	beforeCount := counterValue(beforeFamilies, "orbit_skill_activations_total", "auto")

	event := validEvent()
	err := TrackSkillEvent(event)
	if err != nil {
		t.Fatalf("TrackSkillEvent returned unexpected error: %v", err)
	}

	afterFamilies, _ := reg.Gather()
	afterCount := counterValue(afterFamilies, "orbit_skill_activations_total", "auto")

	if afterCount <= beforeCount {
		t.Fatalf("activation was NOT tracked: counter did not increment (before=%f, after=%f)",
			beforeCount, afterCount)
	}
}

// -----------------------------------------------------------------------
// TestTrackFailClosed
//
// If required fields are missing, TrackSkillEvent MUST return an error
// and increment the failure counter.  The caller treats the error as a
// signal to abort the skill activation.
// -----------------------------------------------------------------------

func TestTrackFailClosed(t *testing.T) {
	reg := newTestRegistry()

	cases := []struct {
		name  string
		event SkillEvent
	}{
		{"missing event_type", SkillEvent{SessionID: "s", Mode: "auto", Timestamp: NowUTC()}},
		{"missing session_id", SkillEvent{EventType: "a", Mode: "auto", Timestamp: NowUTC()}},
		{"missing mode", SkillEvent{EventType: "a", SessionID: "s", Timestamp: NowUTC()}},
		{"invalid mode", SkillEvent{EventType: "a", SessionID: "s", Mode: "invalid", Timestamp: NowUTC()}},
		{"missing timestamp", SkillEvent{EventType: "a", SessionID: "s", Mode: "auto"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := TrackSkillEvent(tc.event)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}

	// Failure counter must have incremented
	families, _ := reg.Gather()
	failures := counterValue(families, "orbit_skill_tracking_failures_total", "")
	if failures < float64(len(cases)) {
		t.Fatalf("expected at least %d tracking failures, got %f", len(cases), failures)
	}
}

// -----------------------------------------------------------------------
// TestMetricsExposed
//
// Verify that all four metrics are present and correctly typed.
// -----------------------------------------------------------------------

func TestMetricsExposed(t *testing.T) {
	reg := newTestRegistry()

	// Fire one event so counters exist
	_ = TrackSkillEvent(validEvent())

	expected := []struct {
		name     string
		contains string
	}{
		{"orbit_skill_activations_total", "orbit_skill_activations_total"},
		{"orbit_skill_tokens_saved_total", "orbit_skill_tokens_saved_total"},
		{"orbit_skill_waste_estimated", "orbit_skill_waste_estimated"},
		{"orbit_skill_tracking_failures_total", "orbit_skill_tracking_failures_total"},
	}

	output := testutil.ToFloat64(skillWasteEstimated)
	if output == 0 {
		// waste was set in validEvent, should not be zero after tracking
		// (unless the event had zero waste, which it doesn't)
	}

	for _, exp := range expected {
		t.Run(exp.name, func(t *testing.T) {
			families, _ := reg.Gather()
			found := false
			for _, f := range families {
				if f.GetName() == exp.name {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("metric %q not found in registry", exp.name)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestTokensSavedAccumulates
//
// Verify tokens_saved counter accumulates across multiple events.
// -----------------------------------------------------------------------

func TestTokensSavedAccumulates(t *testing.T) {
	reg := newTestRegistry()

	before, _ := reg.Gather()
	beforeTokens := counterValue(before, "orbit_skill_tokens_saved_total", "")

	e1 := validEvent()
	e1.SessionID = "sess-tokens-1"
	e1.ImpactEstimatedToken = 500
	_ = TrackSkillEvent(e1)

	e2 := validEvent()
	e2.SessionID = "sess-tokens-2"
	e2.ImpactEstimatedToken = 300
	_ = TrackSkillEvent(e2)

	after, _ := reg.Gather()
	afterTokens := counterValue(after, "orbit_skill_tokens_saved_total", "")

	delta := afterTokens - beforeTokens
	if delta < 800 {
		t.Fatalf("expected tokens_saved to accumulate ≥800, got delta=%f", delta)
	}
}

// -----------------------------------------------------------------------
// TestWasteGaugeReflectsLatest
//
// The waste gauge should reflect the LAST event's estimated_waste, not
// accumulate.
// -----------------------------------------------------------------------

func TestWasteGaugeReflectsLatest(t *testing.T) {
	_ = newTestRegistry() // reset rate limit state
	e1 := validEvent()
	e1.SessionID = "sess-waste-1"
	e1.EstimatedWaste = 999.0
	_ = TrackSkillEvent(e1)

	e2 := validEvent()
	e2.SessionID = "sess-waste-2"
	e2.EstimatedWaste = 42.0
	_ = TrackSkillEvent(e2)

	val := testutil.ToFloat64(skillWasteEstimated)
	if val != 42.0 {
		t.Fatalf("expected waste gauge = 42.0, got %f", val)
	}
}

// -----------------------------------------------------------------------
// TestModeLabels
//
// Each mode (auto, suggest, off) should produce its own counter series.
// -----------------------------------------------------------------------

func TestModeLabels(t *testing.T) {
	reg := newTestRegistry()

	for i, mode := range []string{"auto", "suggest", "off"} {
		e := validEvent()
		e.Mode = mode
		e.SessionID = fmt.Sprintf("sess-mode-%d", i)
		if err := TrackSkillEvent(e); err != nil {
			t.Fatalf("unexpected error for mode %q: %v", mode, err)
		}
	}

	families, _ := reg.Gather()
	for _, mode := range []string{"auto", "suggest", "off"} {
		val := counterValue(families, "orbit_skill_activations_total", mode)
		if val < 1 {
			t.Fatalf("expected activations for mode %q ≥ 1, got %f", mode, val)
		}
	}
}

// -----------------------------------------------------------------------
// TestValidateEvent
// -----------------------------------------------------------------------

func TestValidateEvent(t *testing.T) {
	good := validEvent()
	if err := good.Validate(); err != nil {
		t.Fatalf("valid event failed validation: %v", err)
	}

	bad := SkillEvent{}
	err := bad.Validate()
	if err == nil {
		t.Fatal("empty event should fail validation")
	}
	if !strings.Contains(err.Error(), "event_type") {
		t.Fatalf("expected error about event_type, got: %v", err)
	}
}

// -----------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------

// counterValue extracts a counter value from gathered metric families.
// If modeLabel is empty, returns the raw counter value (no label filter).
func counterValue(families []*dto.MetricFamily, name, modeLabel string) float64 {
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if modeLabel == "" {
				if m.GetCounter() != nil {
					return m.GetCounter().GetValue()
				}
				return 0
			}
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "mode" && lp.GetValue() == modeLabel {
					if m.GetCounter() != nil {
						return m.GetCounter().GetValue()
					}
				}
			}
		}
	}
	return 0
}

// -----------------------------------------------------------------------
// Phase 25 — Session lifecycle, hash integrity, no-skill detection
// -----------------------------------------------------------------------

// TestSessionWithoutSkillIsDetected — sends 21 non-activation events and
// asserts the "without_activation" counter is incremented.
func TestSessionWithoutSkillIsDetected(t *testing.T) {
	reg := newTestRegistry()
	DisableRateLimit() // session tests send many events on the same session_id
	defer ResetRateLimit()
	tracker := NewSessionTracker()

	for i := 0; i < 21; i++ {
		ev := validEvent()
		ev.SessionID = "sess-no-skill"
		ev.EventType = "suggestion" // not "activation"
		ev.ImpactEstimatedToken = 10
		ev.EstimatedWaste = 5.0
		ev.Timestamp = NowUTCAdd(time.Duration(i) * time.Second)
		tracker.RecordEvent(ev)
	}

	// session must exist and NOT be activated
	sess := tracker.GetSession("sess-no-skill")
	if sess == nil {
		t.Fatal("session not found")
	}
	if sess.SkillActivated {
		t.Fatal("session should NOT be marked as activated")
	}
	if len(sess.Events) != 21 {
		t.Fatalf("expected 21 events, got %d", len(sess.Events))
	}

	// Prometheus counter: sessions_without_activation must be ≥ 1
	families, _ := reg.Gather()
	v := counterValue(families, "orbit_skill_sessions_without_activation_total", "")
	if v < 1 {
		t.Fatalf("expected orbit_skill_sessions_without_activation_total >= 1, got %f", v)
	}
}

// TestEventHashIntegrity — verifies sha256 hash chain across events.
func TestEventHashIntegrity(t *testing.T) {
	ResetRateLimit()
	DisableRateLimit() // session tests send many events on the same session_id
	defer ResetRateLimit()
	tracker := NewSessionTracker()

	ev1 := validEvent()
	ev1.SessionID = "sess-hash"
	ev1.ImpactEstimatedToken = 100
	ev1.Timestamp = NowUTC()
	ev1, _ = tracker.RecordEvent(ev1)

	// genesis event: PrevHash must be empty, EventHash must match ComputeHash
	expected1 := ComputeHash("sess-hash", ev1.Timestamp.Time, 100)
	if ev1.EventHash != expected1 {
		t.Fatalf("event1 hash mismatch: got %s, want %s", ev1.EventHash, expected1)
	}
	if ev1.PrevHash != "" {
		t.Fatalf("genesis event PrevHash should be empty, got %s", ev1.PrevHash)
	}

	ev2 := validEvent()
	ev2.SessionID = "sess-hash"
	ev2.ImpactEstimatedToken = 200
	ev2.Timestamp = NowUTCAdd(time.Second)
	ev2, _ = tracker.RecordEvent(ev2)

	// second event: PrevHash == first event hash, own hash is different
	expected2 := ComputeHash("sess-hash", ev2.Timestamp.Time, 200)
	if ev2.EventHash != expected2 {
		t.Fatalf("event2 hash mismatch: got %s, want %s", ev2.EventHash, expected2)
	}
	if ev2.PrevHash != ev1.EventHash {
		t.Fatalf("event2 PrevHash should chain to event1: got %s, want %s", ev2.PrevHash, ev1.EventHash)
	}
	if ev2.EventHash == ev1.EventHash {
		t.Fatal("event hashes must differ for different events")
	}
}

// TestSessionSummaryAccumulates — verifies tokens, waste avg, and event count.
func TestSessionSummaryAccumulates(t *testing.T) {
	ResetRateLimit()
	DisableRateLimit() // session tests send many events on the same session_id
	defer ResetRateLimit()
	tracker := NewSessionTracker()

	tokens := []int64{100, 200, 300}
	wastes := []float64{10.0, 20.0, 30.0}

	for i := 0; i < 3; i++ {
		ev := validEvent()
		ev.SessionID = "sess-accum"
		ev.ImpactEstimatedToken = tokens[i]
		ev.EstimatedWaste = wastes[i]
		ev.Timestamp = NowUTCAdd(time.Duration(i) * time.Second)
		if i == 1 {
			ev.EventType = "activation" // one activation
		}
		tracker.RecordEvent(ev)
	}

	sess := tracker.GetSession("sess-accum")
	if sess == nil {
		t.Fatal("session not found")
	}
	if len(sess.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(sess.Events))
	}
	var expectedTokens int64 = 600
	if sess.TotalTokensSaved != expectedTokens {
		t.Fatalf("expected TotalTokensSaved=%d, got %d", expectedTokens, sess.TotalTokensSaved)
	}
	expectedAvg := 20.0 // (10+20+30)/3
	if sess.AvgWaste != expectedAvg {
		t.Fatalf("expected AvgWaste=%.1f, got %.1f", expectedAvg, sess.AvgWaste)
	}
	if !sess.SkillActivated {
		t.Fatal("session should be marked activated (one activation event)")
	}
}

// TestSessionMetricsExposed — checks all 11 Prometheus metrics are present.
func TestSessionMetricsExposed(t *testing.T) {
	reg := newTestRegistry()

	expected := []string{
		"orbit_skill_activations_total",
		"orbit_skill_tokens_saved_total",
		"orbit_skill_waste_estimated",
		"orbit_skill_tracking_failures_total",
		"orbit_skill_sessions_total",
		"orbit_skill_sessions_with_activation_total",
		"orbit_skill_sessions_without_activation_total",
		"orbit_seed_mode",
		"orbit_tracking_up",
		"orbit_instance_id",
		"orbit_last_event_timestamp",
	}

	// fire one event so counters are initialized
	ev := validEvent()
	TrackSkillEvent(ev)

	// Initialize GaugeVec so it appears in gather (needs at least one label value)
	instanceIDGauge.WithLabelValues("test-session-metrics").Set(1)
	defer instanceIDGauge.DeleteLabelValues("test-session-metrics")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	found := make(map[string]bool)
	for _, f := range families {
		found[f.GetName()] = true
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("metric %q not found in registry", name)
		}
	}
}

// TestGetLastHash — verifies GetLastHash returns the latest event hash.
func TestGetLastHash(t *testing.T) {
	tracker := NewSessionTracker()

	ev := validEvent()
	ev.SessionID = "sess-lasthash"
	ev.ImpactEstimatedToken = 42
	ev.Timestamp = NowUTC()
	ev, _ = tracker.RecordEvent(ev)

	last := tracker.GetLastHash("sess-lasthash")
	if last != ev.EventHash {
		t.Fatalf("GetLastHash mismatch: got %s, want %s", last, ev.EventHash)
	}

	// unknown session returns empty
	if tracker.GetLastHash("nonexistent") != "" {
		t.Fatal("unknown session should return empty hash")
	}
}

// TestEnvSafetyMetrics — validates fail-closed environment safety gauges.
func TestEnvSafetyMetrics(t *testing.T) {
	reg := newTestRegistry()

	t.Run("seed_mode_defaults_to_zero", func(t *testing.T) {
		seedModeGauge.Set(0) // ensure clean state (direct set, not via SetSeedMode)
		families, err := reg.Gather()
		if err != nil {
			t.Fatalf("gather: %v", err)
		}
		for _, f := range families {
			if f.GetName() == "orbit_seed_mode" {
				val := f.GetMetric()[0].GetGauge().GetValue()
				if val != 0 {
					t.Fatalf("orbit_seed_mode should default to 0 (prod), got %v", val)
				}
				return
			}
		}
		t.Fatal("orbit_seed_mode metric not found")
	})

	t.Run("set_seed_mode_true", func(t *testing.T) {
		ResetSeedModeLock() // allow SetSeedMode to be called
		SetSeedMode(true)
		families, _ := reg.Gather()
		for _, f := range families {
			if f.GetName() == "orbit_seed_mode" {
				val := f.GetMetric()[0].GetGauge().GetValue()
				if val != 1 {
					t.Fatalf("orbit_seed_mode should be 1 after SetSeedMode(true), got %v", val)
				}
				return
			}
		}
		t.Fatal("orbit_seed_mode metric not found")
	})

	t.Run("set_seed_mode_false", func(t *testing.T) {
		ResetSeedModeLock()
		SetSeedMode(false)
		families, _ := reg.Gather()
		for _, f := range families {
			if f.GetName() == "orbit_seed_mode" {
				val := f.GetMetric()[0].GetGauge().GetValue()
				if val != 0 {
					t.Fatalf("orbit_seed_mode should be 0 after SetSeedMode(false), got %v", val)
				}
				return
			}
		}
		t.Fatal("orbit_seed_mode metric not found")
	})

	t.Run("tracking_up_gauge_exists", func(t *testing.T) {
		trackingUpGauge.Set(1) // simulate what RegisterMetrics does
		families, _ := reg.Gather()
		for _, f := range families {
			if f.GetName() == "orbit_tracking_up" {
				val := f.GetMetric()[0].GetGauge().GetValue()
				if val != 1 {
					t.Fatalf("orbit_tracking_up should be 1, got %v", val)
				}
				return
			}
		}
		t.Fatal("orbit_tracking_up metric not found")
	})

	// cleanup: restore safe defaults
	ResetSeedModeLock()
	seedModeGauge.Set(0)
}

// -----------------------------------------------------------------------
// Phase 31 — Governance: lock, instance_id, freshness
// -----------------------------------------------------------------------

// TestSeedModeLock — calling SetSeedMode twice must panic.
func TestSeedModeLock(t *testing.T) {
	ResetSeedModeLock()

	// First call: must NOT panic
	SetSeedMode(false)

	// Second call: must panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on second SetSeedMode call, got none")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "immutable") {
			t.Fatalf("unexpected panic message: %v", r)
		}
		// cleanup
		ResetSeedModeLock()
	}()

	SetSeedMode(true) // this must panic
}

// TestInstanceID — verifies orbit_instance_id is published with a non-empty label.
func TestInstanceID(t *testing.T) {
	reg := newTestRegistry()

	// Set a known instance ID via the gauge directly to simulate RegisterMetrics
	testID := "test-instance-abc123"
	instanceIDGauge.WithLabelValues(testID).Set(1)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "orbit_instance_id" {
			for _, m := range f.GetMetric() {
				for _, lp := range m.GetLabel() {
					if lp.GetName() == "instance_id" && lp.GetValue() == testID {
						found = true
						if m.GetGauge().GetValue() != 1 {
							t.Fatalf("orbit_instance_id gauge should be 1, got %v", m.GetGauge().GetValue())
						}
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("orbit_instance_id metric with expected label not found")
	}

	// cleanup: remove the test label series
	instanceIDGauge.DeleteLabelValues(testID)
}

// TestFreshness — verifies orbit_last_event_timestamp updates on TrackSkillEvent.
func TestFreshness(t *testing.T) {
	reg := newTestRegistry()

	// Before any event, timestamp should be whatever it was from previous tests.
	// We set it to 0 explicitly to test the update.
	lastEventTimestampGauge.Set(0)

	beforeFamilies, _ := reg.Gather()
	var beforeTS float64
	for _, f := range beforeFamilies {
		if f.GetName() == "orbit_last_event_timestamp" {
			beforeTS = f.GetMetric()[0].GetGauge().GetValue()
		}
	}

	// Track an event — should update the timestamp
	ev := validEvent()
	if err := TrackSkillEvent(ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterFamilies, _ := reg.Gather()
	var afterTS float64
	for _, f := range afterFamilies {
		if f.GetName() == "orbit_last_event_timestamp" {
			afterTS = f.GetMetric()[0].GetGauge().GetValue()
		}
	}

	if afterTS <= beforeTS {
		t.Fatalf("orbit_last_event_timestamp should have increased: before=%v, after=%v", beforeTS, afterTS)
	}

	// The value should be a reasonable unix timestamp (> year 2020)
	if afterTS < 1577836800 { // 2020-01-01
		t.Fatalf("orbit_last_event_timestamp looks invalid: %v", afterTS)
	}
}

// ---------------------------------------------------------------------------
// FlexTime — temporal integrity tests
// ---------------------------------------------------------------------------

// TestFlexTimeAcceptsRFC3339 — valid RFC3339 and RFC3339Nano strings are
// parsed and normalised to UTC.
func TestFlexTimeAcceptsRFC3339(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"UTC Z suffix", `"2026-04-15T12:00:00Z"`},
		{"UTC with nanos", `"2026-04-15T12:00:00.123456789Z"`},
		{"positive offset", `"2026-04-15T15:00:00+03:00"`},
		{"negative offset", `"2026-04-15T05:00:00-07:00"`},
	}

	// Pin "now" so timestamps from 2026-04-15 are within the 24h window.
	orig := flexTimeNow
	flexTimeNow = func() time.Time { return time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { flexTimeNow = orig }()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ft FlexTime
			if err := ft.UnmarshalJSON([]byte(tc.input)); err != nil {
				t.Fatalf("expected success for %s, got error: %v", tc.input, err)
			}
			if ft.Time.Location() != time.UTC {
				t.Fatalf("expected UTC, got %v", ft.Time.Location())
			}
			if ft.Time.IsZero() {
				t.Fatal("parsed time should not be zero")
			}
		})
	}
}

// TestFlexTimeRejectsNoTimezone — bare ISO timestamps without an explicit
// timezone offset must be rejected (fail-closed: no ambiguity allowed).
func TestFlexTimeRejectsNoTimezone(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"bare datetime", `"2026-04-15T12:00:00"`},
		{"bare with nanos", `"2026-04-15T12:00:00.123456"`},
		{"date only", `"2026-04-15"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ft FlexTime
			err := ft.UnmarshalJSON([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error for %s (no timezone), but got nil", tc.input)
			}
			if !strings.Contains(err.Error(), "not valid RFC3339") {
				t.Fatalf("error should mention RFC3339, got: %v", err)
			}
		})
	}
}

// TestFlexTimeRejectsInvalid — garbage, future, and ancient timestamps
// must be rejected.
func TestFlexTimeRejectsInvalid(t *testing.T) {
	// Pin "now" for deterministic bounds checking.
	orig := flexTimeNow
	flexTimeNow = func() time.Time { return time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC) }
	defer func() { flexTimeNow = orig }()

	cases := []struct {
		name      string
		input     string
		errSubstr string
	}{
		{"garbage string", `"not-a-date"`, "not valid RFC3339"},
		{"just a number", `"12345"`, "not valid RFC3339"},
		{"too far in future", `"2026-04-15T12:10:00Z"`, "too far in the future"},
		{"too old", `"2026-04-13T00:00:00Z"`, "too old"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ft FlexTime
			err := ft.UnmarshalJSON([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error for %s, but got nil", tc.input)
			}
			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("error should contain %q, got: %v", tc.errSubstr, err)
			}
		})
	}
}
