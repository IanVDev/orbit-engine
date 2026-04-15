// v1_contract_test.go — Anti-regression test for the orbit-engine v1.0 contract.
//
// This is THE test that gates v1.0 release. If it fails, the release
// MUST NOT proceed. It validates that every metric in V1_CONTRACT.md
// is present, correctly typed, and reachable.
//
// Run:
//
//	cd tracking && go test -run TestV1ContractComplete -v
package tracking

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// TestV1ContractComplete is the single anti-regression test without which
// the v1.0 does not exist. It registers all metrics, fires a valid event,
// and verifies every contracted metric is present with the correct type.
func TestV1ContractComplete(t *testing.T) {
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
	)

	// Simulate production startup sequence
	trackingUpGauge.Set(1)
	seedModeGauge.Set(0)
	instanceIDGauge.WithLabelValues("v1-contract-test").Set(1)
	defer instanceIDGauge.DeleteLabelValues("v1-contract-test")

	// Fire a valid event to populate counters
	ev := SkillEvent{
		EventType:            "activation",
		Timestamp:            NowUTC(),
		SessionID:            "v1-contract-sess",
		Mode:                 "auto",
		Trigger:              "v1_contract_test",
		EstimatedWaste:       100.0,
		ActionsSuggested:     2,
		ActionsApplied:       1,
		ImpactEstimatedToken: 500,
	}
	if err := TrackSkillEvent(ev); err != nil {
		t.Fatalf("TrackSkillEvent failed: %v", err)
	}

	// Fire a second event with different mode to ensure label coverage
	ev2 := ev
	ev2.Mode = "suggest"
	ev2.Timestamp = FlexTime{Time: time.Now().Add(time.Second).UTC()}
	if err := TrackSkillEvent(ev2); err != nil {
		t.Fatalf("TrackSkillEvent(suggest) failed: %v", err)
	}

	// Gather all metrics
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	familyMap := make(map[string]*dto.MetricFamily)
	for _, f := range families {
		familyMap[f.GetName()] = f
	}

	// ── CONTRACT: every metric in V1_CONTRACT.md must exist ──────────

	type contractMetric struct {
		name       string
		metricType dto.MetricType
		minValue   *float64 // nil = don't check value, just existence
	}

	zero := 0.0
	one := 1.0
	positive := 0.001

	contract := []contractMetric{
		// Tracking server metrics
		{"orbit_skill_activations_total", dto.MetricType_COUNTER, &positive},
		{"orbit_skill_tokens_saved_total", dto.MetricType_COUNTER, &positive},
		{"orbit_skill_waste_estimated", dto.MetricType_GAUGE, &positive},
		{"orbit_skill_tracking_failures_total", dto.MetricType_COUNTER, &zero},
		{"orbit_skill_sessions_total", dto.MetricType_COUNTER, nil},
		{"orbit_skill_sessions_with_activation_total", dto.MetricType_COUNTER, nil},
		{"orbit_skill_sessions_without_activation_total", dto.MetricType_COUNTER, nil},
		{"orbit_seed_mode", dto.MetricType_GAUGE, &zero},
		{"orbit_tracking_up", dto.MetricType_GAUGE, &one},
		{"orbit_instance_id", dto.MetricType_GAUGE, &one},
		{"orbit_last_event_timestamp", dto.MetricType_GAUGE, &positive},
	}

	for _, c := range contract {
		t.Run("metric_exists/"+c.name, func(t *testing.T) {
			f, ok := familyMap[c.name]
			if !ok {
				t.Fatalf("CONTRACT VIOLATION: metric %q not found in registry", c.name)
			}

			// Check type
			if f.GetType() != c.metricType {
				t.Fatalf("CONTRACT VIOLATION: metric %q has type %v, want %v",
					c.name, f.GetType(), c.metricType)
			}

			// Check value if specified
			if c.minValue != nil {
				val := extractFirstValue(f)
				if val < *c.minValue {
					t.Fatalf("CONTRACT VIOLATION: metric %q value=%f, want >= %f",
						c.name, val, *c.minValue)
				}
			}
		})
	}

	// ── CONTRACT: mode labels must include auto and suggest ──────────

	t.Run("mode_labels_coverage", func(t *testing.T) {
		f, ok := familyMap["orbit_skill_activations_total"]
		if !ok {
			t.Fatal("orbit_skill_activations_total not found")
		}

		modes := make(map[string]bool)
		for _, m := range f.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "mode" {
					modes[lp.GetValue()] = true
				}
			}
		}

		for _, required := range []string{"auto", "suggest"} {
			if !modes[required] {
				t.Errorf("CONTRACT VIOLATION: mode=%q not found in activations_total", required)
			}
		}
	})

	// ── CONTRACT: instance_id label must be non-empty ────────────────

	t.Run("instance_id_has_label", func(t *testing.T) {
		f, ok := familyMap["orbit_instance_id"]
		if !ok {
			t.Fatal("orbit_instance_id not found")
		}

		for _, m := range f.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "instance_id" && lp.GetValue() != "" {
					return // found a non-empty instance_id
				}
			}
		}
		t.Fatal("CONTRACT VIOLATION: orbit_instance_id has no non-empty instance_id label")
	})

	// ── CONTRACT: PromQL governance rejects raw metrics ──────────────

	t.Run("governance_rejects_raw", func(t *testing.T) {
		rawQueries := []string{
			"orbit_skill_tokens_saved_total",
			"orbit_skill_activations_total",
			"rate(orbit_skill_waste_estimated[5m])",
		}
		for _, q := range rawQueries {
			if err := ValidatePromQLStrict(q); err == nil {
				t.Errorf("CONTRACT VIOLATION: raw query %q was NOT rejected by governance", q)
			}
		}
	})

	// ── CONTRACT: governance allows recording rules ──────────────────

	t.Run("governance_allows_recording_rules", func(t *testing.T) {
		allowedQueries := []string{
			"orbit:tokens_saved_total:prod",
			"orbit:activations_total:prod",
			"orbit:waste_estimated:prod",
			"orbit:sessions_total:prod",
			"orbit:event_staleness_seconds:prod",
			"orbit:seed_contamination",
			"orbit_seed_mode",
			"orbit_tracking_up",
			"orbit_instance_id",
			"orbit_last_event_timestamp",
			"orbit_gateway_requests_total",
		}
		for _, q := range allowedQueries {
			if err := ValidatePromQLStrict(q); err != nil {
				t.Errorf("CONTRACT VIOLATION: allowed query %q was rejected: %v", q, err)
			}
		}
	})

	// ── CONTRACT: fail-closed — empty query rejected ─────────────────

	t.Run("governance_rejects_empty", func(t *testing.T) {
		if err := ValidatePromQLStrict(""); err == nil {
			t.Error("CONTRACT VIOLATION: empty query was NOT rejected")
		}
		if err := ValidatePromQLStrict("   "); err == nil {
			t.Error("CONTRACT VIOLATION: whitespace query was NOT rejected")
		}
	})

	// ── CONTRACT: FlexTime rejects bare timestamps ───────────────────

	t.Run("flextime_rejects_no_timezone", func(t *testing.T) {
		var ft FlexTime
		err := ft.UnmarshalJSON([]byte(`"2026-04-15T12:00:00"`))
		if err == nil {
			t.Error("CONTRACT VIOLATION: bare timestamp without timezone was accepted")
		}
	})

	// ── CONTRACT: SkillEvent validation is fail-closed ───────────────

	t.Run("event_validation_fail_closed", func(t *testing.T) {
		invalid := []SkillEvent{
			{}, // completely empty
			{EventType: "a", SessionID: "s", Mode: "invalid", Timestamp: NowUTC()},
			{EventType: "a", SessionID: "", Mode: "auto", Timestamp: NowUTC()},
		}
		for i, inv := range invalid {
			if err := inv.Validate(); err == nil {
				t.Errorf("CONTRACT VIOLATION: invalid event #%d was accepted", i)
			}
		}
	})
}

// extractFirstValue gets the numeric value from the first metric in a family.
func extractFirstValue(f *dto.MetricFamily) float64 {
	if len(f.GetMetric()) == 0 {
		return 0
	}
	m := f.GetMetric()[0]
	switch f.GetType() {
	case dto.MetricType_COUNTER:
		return m.GetCounter().GetValue()
	case dto.MetricType_GAUGE:
		return m.GetGauge().GetValue()
	case dto.MetricType_HISTOGRAM:
		return float64(m.GetHistogram().GetSampleCount())
	default:
		return 0
	}
}

// TestV1GatewayMetricsContract validates gateway-specific metrics exist.
func TestV1GatewayMetricsContract(t *testing.T) {
	reg := prometheus.NewRegistry()
	RegisterGatewayMetrics(reg)

	// Trigger counters so they appear in gather
	gatewayRequestsTotal.WithLabelValues("/api/v1/query", "GET").Inc()
	gatewayBlockedTotal.WithLabelValues("governance").Inc()
	gatewayErrorsTotal.WithLabelValues("upstream_unreachable").Inc()
	gatewayLatencyMs.Observe(42.0)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	familyMap := make(map[string]bool)
	for _, f := range families {
		familyMap[f.GetName()] = true
	}

	required := []string{
		"orbit_gateway_requests_total",
		"orbit_gateway_blocked_total",
		"orbit_gateway_errors_total",
		"orbit_gateway_latency_ms",
	}

	for _, name := range required {
		if !familyMap[name] {
			t.Errorf("CONTRACT VIOLATION: gateway metric %q not found", name)
		}
	}
}
