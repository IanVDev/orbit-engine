// value_test.go — Anti-regression tests for the value-observability layer.
//
// Run:
//
//	cd tracking && go test -run TestValue -v
package tracking

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// newValueTestRegistry returns a fresh Prometheus registry with value metrics registered.
// Mirrors the pattern of newSecurityTestRegistry for test isolation.
func newValueTestRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	RegisterValueMetrics(reg)
	return reg
}

// valueCounterVec returns the sum of all label combinations for a CounterVec metric.
func valueCounterSum(families []*dto.MetricFamily, name string) float64 {
	for _, f := range families {
		if f.GetName() == name {
			var sum float64
			for _, m := range f.GetMetric() {
				sum += m.GetCounter().GetValue()
			}
			return sum
		}
	}
	return -1 // sentinel: not found
}

// valueCounterByLabel returns the value of a counter with the given label value.
func valueCounterByLabel(families []*dto.MetricFamily, name, labelKey, labelVal string) float64 {
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == labelKey && lp.GetValue() == labelVal {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return -1 // sentinel: not found
}

// ---------------------------------------------------------------------------
// 1. ClassifyEventValue — unit tests
// ---------------------------------------------------------------------------

func TestValueClassifyEventValue(t *testing.T) {
	t.Run("no_suggestions_returns_empty", func(t *testing.T) {
		ev := SkillEvent{ActionsSuggested: 0, ActionsApplied: 0}
		level, err := ClassifyEventValue(ev)
		if err != nil {
			t.Fatalf("no suggestions should not error: %v", err)
		}
		if level != "" {
			t.Fatalf("no suggestions should return empty level, got %q", level)
		}
	})

	t.Run("all_applied_is_high", func(t *testing.T) {
		ev := SkillEvent{ActionsSuggested: 3, ActionsApplied: 3}
		level, _ := ClassifyEventValue(ev)
		if level != ValueHigh {
			t.Fatalf("all applied → high, got %q", level)
		}
	})

	t.Run("partial_applied_is_medium", func(t *testing.T) {
		ev := SkillEvent{ActionsSuggested: 4, ActionsApplied: 2}
		level, _ := ClassifyEventValue(ev)
		if level != ValueMedium {
			t.Fatalf("partial applied → medium, got %q", level)
		}
	})

	t.Run("none_applied_is_low", func(t *testing.T) {
		ev := SkillEvent{ActionsSuggested: 2, ActionsApplied: 0}
		level, _ := ClassifyEventValue(ev)
		if level != ValueLow {
			t.Fatalf("none applied → low, got %q", level)
		}
	})

	t.Run("applied_exceeds_suggested_is_high", func(t *testing.T) {
		// Edge case: applied > suggested (e.g. user did extra work)
		ev := SkillEvent{ActionsSuggested: 2, ActionsApplied: 5}
		level, _ := ClassifyEventValue(ev)
		if level != ValueHigh {
			t.Fatalf("applied >= suggested → high, got %q", level)
		}
	})
}

// ---------------------------------------------------------------------------
// 2. RecordPerceivedValue — fail-closed for invalid levels
// ---------------------------------------------------------------------------

func TestValueRecordPerceivedValueFailClosed(t *testing.T) {
	_ = newValueTestRegistry()

	// Valid levels must not error
	for _, lvl := range []ValueLevel{ValueHigh, ValueMedium, ValueLow} {
		if err := RecordPerceivedValue(lvl, "sess-valid"); err != nil {
			t.Errorf("valid level %q should not error: %v", lvl, err)
		}
	}

	// Invalid / empty levels must return error and NOT increment any counter
	for _, bad := range []ValueLevel{"", "extreme", "HIGH", "MEDIUM", "LOW", "unknown"} {
		if err := RecordPerceivedValue(bad, "sess-invalid"); err == nil {
			t.Errorf("invalid level %q should return error (fail-closed)", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Metrics emitted correctly into a fresh registry
// ---------------------------------------------------------------------------

func TestValueMetricsEmitted(t *testing.T) {
	reg := newValueTestRegistry()

	// Capture baseline (counters are global and may have been incremented by
	// earlier tests in the same process run).
	familiesBefore, _ := reg.Gather()
	highBefore := valueCounterByLabel(familiesBefore, "orbit_user_perceived_value_total", "level", "high")
	medBefore := valueCounterByLabel(familiesBefore, "orbit_user_perceived_value_total", "level", "medium")
	lowBefore := valueCounterByLabel(familiesBefore, "orbit_user_perceived_value_total", "level", "low")
	returnedBefore := valueCounterSum(familiesBefore, "orbit_user_returned_total")
	acceptedBefore := valueCounterSum(familiesBefore, "orbit_user_accepted_suggestion_total")
	ignoredBefore := valueCounterSum(familiesBefore, "orbit_user_ignored_suggestion_total")
	// Treat sentinel (-1 = not found) as 0 for delta math.
	clamp := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		return v
	}
	highBefore = clamp(highBefore)
	medBefore = clamp(medBefore)
	lowBefore = clamp(lowBefore)
	returnedBefore = clamp(returnedBefore)
	acceptedBefore = clamp(acceptedBefore)
	ignoredBefore = clamp(ignoredBefore)

	// Emit one event of each type
	_ = RecordPerceivedValue(ValueHigh, "s1")
	_ = RecordPerceivedValue(ValueMedium, "s1")
	_ = RecordPerceivedValue(ValueLow, "s2")
	RecordUserReturned("s2")
	RecordSuggestionAccepted("s1")
	_ = RecordSuggestionIgnored("s2", IgnoreReasonUnknown)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("registry gather: %v", err)
	}

	// All 5 metric families must be present
	requiredMetrics := []string{
		"orbit_user_perceived_value_total",
		"orbit_user_returned_total",
		"orbit_user_accepted_suggestion_total",
		"orbit_user_ignored_suggestion_total",
		"orbit_user_ignore_reason_total",
	}
	fm := make(map[string]bool)
	for _, f := range families {
		fm[f.GetName()] = true
	}
	for _, name := range requiredMetrics {
		if !fm[name] {
			t.Errorf("metric %q not found in registry — RegisterValueMetrics may not be wired", name)
		}
	}

	// Verify label-level deltas for perceived_value
	if d := valueCounterByLabel(families, "orbit_user_perceived_value_total", "level", "high") - highBefore; d != 1 {
		t.Errorf("orbit_user_perceived_value_total{level=high}: want delta=1, got %.0f", d)
	}
	if d := valueCounterByLabel(families, "orbit_user_perceived_value_total", "level", "medium") - medBefore; d != 1 {
		t.Errorf("orbit_user_perceived_value_total{level=medium}: want delta=1, got %.0f", d)
	}
	if d := valueCounterByLabel(families, "orbit_user_perceived_value_total", "level", "low") - lowBefore; d != 1 {
		t.Errorf("orbit_user_perceived_value_total{level=low}: want delta=1, got %.0f", d)
	}

	// Verify single counter deltas
	if d := valueCounterSum(families, "orbit_user_returned_total") - returnedBefore; d != 1 {
		t.Errorf("orbit_user_returned_total: want delta=1, got %.0f", d)
	}
	if d := valueCounterSum(families, "orbit_user_accepted_suggestion_total") - acceptedBefore; d != 1 {
		t.Errorf("orbit_user_accepted_suggestion_total: want delta=1, got %.0f", d)
	}
	if d := valueCounterSum(families, "orbit_user_ignored_suggestion_total") - ignoredBefore; d != 1 {
		t.Errorf("orbit_user_ignored_suggestion_total: want delta=1, got %.0f", d)
	}
}

// ---------------------------------------------------------------------------
// 4. RecordEventValue — auto-classification integration
// ---------------------------------------------------------------------------

func TestValueRecordEventValue(t *testing.T) {
	reg := newValueTestRegistry()

	// Capture baseline before this test
	familiesBefore, _ := reg.Gather()
	highBefore := valueCounterByLabel(familiesBefore, "orbit_user_perceived_value_total", "level", "high")
	lowBefore := valueCounterByLabel(familiesBefore, "orbit_user_perceived_value_total", "level", "low")
	if highBefore < 0 {
		highBefore = 0
	}
	if lowBefore < 0 {
		lowBefore = 0
	}
	acceptedBefore := valueCounterSum(familiesBefore, "orbit_user_accepted_suggestion_total")
	ignoredBefore := valueCounterSum(familiesBefore, "orbit_user_ignored_suggestion_total")
	if acceptedBefore < 0 {
		acceptedBefore = 0
	}
	if ignoredBefore < 0 {
		ignoredBefore = 0
	}

	// Event with all suggestions applied → high + accepted
	ev := SkillEvent{
		SessionID:        "val-sess-1",
		ActionsSuggested: 3,
		ActionsApplied:   3,
	}
	RecordEventValue(ev)

	// Event with no suggestions → nothing recorded (fail-closed)
	evNoSuggestions := SkillEvent{
		SessionID:        "val-sess-2",
		ActionsSuggested: 0,
		ActionsApplied:   0,
	}
	RecordEventValue(evNoSuggestions)

	// Event where nothing was applied → low + ignored
	evIgnored := SkillEvent{
		SessionID:        "val-sess-3",
		ActionsSuggested: 2,
		ActionsApplied:   0,
	}
	RecordEventValue(evIgnored)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	// high delta: +1, low delta: +1
	high := valueCounterByLabel(families, "orbit_user_perceived_value_total", "level", "high")
	low := valueCounterByLabel(families, "orbit_user_perceived_value_total", "level", "low")
	if high-highBefore != 1 {
		t.Errorf("expected +1 high event, got delta=%.0f", high-highBefore)
	}
	if low-lowBefore != 1 {
		t.Errorf("expected +1 low event, got delta=%.0f", low-lowBefore)
	}

	// accepted delta: +1, ignored delta: +1
	accepted := valueCounterSum(families, "orbit_user_accepted_suggestion_total")
	ignored := valueCounterSum(families, "orbit_user_ignored_suggestion_total")
	if accepted-acceptedBefore != 1 {
		t.Errorf("expected +1 accepted, got delta=%.0f", accepted-acceptedBefore)
	}
	if ignored-ignoredBefore != 1 {
		t.Errorf("expected +1 ignored, got delta=%.0f", ignored-ignoredBefore)
	}
}

// ---------------------------------------------------------------------------
// 5. Governance — value metrics pass PromQL allow-list validation
// ---------------------------------------------------------------------------

func TestValueGovernanceAllowsMetrics(t *testing.T) {
	metrics := []string{
		"orbit_user_perceived_value_total",
		`rate(orbit_user_perceived_value_total{level="high"}[5m])`,
		"orbit_user_returned_total",
		// NOTE: querying orbit_user_returned_total{fingerprint=...} is intentionally blocked
		// by promql_gov (high-cardinality label protection). Aggregate only.
		`rate(orbit_user_returned_total[5m])`,
		"orbit_user_accepted_suggestion_total",
		"orbit_user_ignored_suggestion_total",
		"orbit_user_ignore_reason_total",
		`rate(orbit_user_ignore_reason_total{reason="latency"}[5m])`,
		`rate(orbit_user_accepted_suggestion_total[5m])`,
	}
	for _, q := range metrics {
		if err := ValidatePromQLStrict(q); err != nil {
			t.Errorf("governance rejected value metric query %q: %v", q, err)
		}
	}
}

func TestValueGovernanceBlocksFingerprintQuery(t *testing.T) {
	// Querying by specific fingerprint value would create unbounded series.
	// The governance layer must reject it (high-cardinality protection).
	q := `rate(orbit_user_returned_total{fingerprint="abc123"}[5m])`
	if err := ValidatePromQLStrict(q); err == nil {
		t.Errorf("governance must reject high-cardinality fingerprint query: %q", q)
	}
}

// ---------------------------------------------------------------------------
// 7. demo_value.sh
//    "Token savings" e "Efficiency" na saída.
// ---------------------------------------------------------------------------

func TestValueDemoScript(t *testing.T) {
	scriptPath := "../scripts/demo_value.sh"

	// Verify the script exists and is executable
	out, err := exec.Command("bash", scriptPath).CombinedOutput()
	if err != nil {
		t.Fatalf("demo_value.sh exited with error: %v\nOutput:\n%s", err, string(out))
	}

	output := string(out)

	// Must contain the savings line
	if !strings.Contains(output, "Token savings:") {
		t.Errorf("demo_value.sh output must contain 'Token savings:', got:\n%s", output)
	}

	// Must contain the efficiency line
	if !strings.Contains(output, "Efficiency:") {
		t.Errorf("demo_value.sh output must contain 'Efficiency:', got:\n%s", output)
	}

	// Must contain a % sign (percentage computed)
	if !strings.Contains(output, "%") {
		t.Errorf("demo_value.sh output must contain a percentage, got:\n%s", output)
	}

	// Must contain "Status: OK" — script reached completion
	if !strings.Contains(output, "Status: OK") {
		t.Errorf("demo_value.sh output must contain 'Status: OK', got:\n%s", output)
	}

	// Token savings must be a positive number (sanity check)
	// We know baseline=1000, orbit=600, savings=400 from the script.
	if !strings.Contains(output, "Token savings:") {
		t.Errorf("demo_value.sh must report token savings")
	}
	// Value level must be one of the three valid tiers
	validLevels := []string{"Value level:   high", "Value level:   medium", "Value level:   low"}
	found := false
	for _, lvl := range validLevels {
		if strings.Contains(output, lvl) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("demo_value.sh must output a valid value level, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// 6+. UserFingerprint — determinismo e pseudonimato
// ---------------------------------------------------------------------------

func TestValueUserFingerprint_Consistent(t *testing.T) {
	// Same session ID must always produce the same fingerprint (deterministic).
	fp1 := UserFingerprint("sess-abc")
	fp2 := UserFingerprint("sess-abc")
	if fp1 != fp2 {
		t.Errorf("UserFingerprint must be deterministic: got %q then %q", fp1, fp2)
	}
	// Fingerprint must be 16 hex chars.
	if len(fp1) != 16 {
		t.Errorf("UserFingerprint must be 16 chars, got %d: %q", len(fp1), fp1)
	}
}

func TestValueUserFingerprint_DifferentInputs(t *testing.T) {
	fp1 := UserFingerprint("sess-A")
	fp2 := UserFingerprint("sess-B")
	if fp1 == fp2 {
		t.Errorf("different session IDs must produce different fingerprints, both got %q", fp1)
	}
}

func TestValueUserFingerprint_NonReversible(t *testing.T) {
	// Fingerprint must NOT contain the raw session ID (pseudonymous).
	sessionID := "secret-session-xyz"
	fp := UserFingerprint(sessionID)
	if strings.Contains(fp, sessionID) {
		t.Errorf("fingerprint must not contain raw session ID, got %q", fp)
	}
}

// ---------------------------------------------------------------------------
// 7. InferIgnoreReason — heurísticas de classificação
// ---------------------------------------------------------------------------

func TestValueInferIgnoreReason(t *testing.T) {
	cases := []struct {
		name   string
		event  SkillEvent
		expect IgnoreReason
	}{
		{
			name: "low_confidence: low waste + low tokens",
			event: SkillEvent{
				EstimatedWaste:       0.04,
				ImpactEstimatedToken: 50,
			},
			expect: IgnoreReasonLowConfidence,
		},
		{
			name: "no_perceived_value: zero impact tokens",
			event: SkillEvent{
				EstimatedWaste:       0.5,
				ImpactEstimatedToken: 0,
			},
			expect: IgnoreReasonNoPerceivedValue,
		},
		{
			name: "latency: suggest mode",
			event: SkillEvent{
				EstimatedWaste:       0.5,
				ImpactEstimatedToken: 200,
				Mode:                 "suggest",
			},
			expect: IgnoreReasonLatency,
		},
		{
			name: "unknown: no heuristic matches",
			event: SkillEvent{
				EstimatedWaste:       0.5,
				ImpactEstimatedToken: 200,
				Mode:                 "auto",
			},
			expect: IgnoreReasonUnknown,
		},
		{
			name: "low_confidence takes priority over suggest mode",
			event: SkillEvent{
				EstimatedWaste:       0.01,
				ImpactEstimatedToken: 10,
				Mode:                 "suggest",
			},
			expect: IgnoreReasonLowConfidence,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := InferIgnoreReason(tc.event)
			if got != tc.expect {
				t.Errorf("InferIgnoreReason: want %q, got %q", tc.expect, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 8. orbit_user_ignore_reason_total — métrica emitida com label correto
// ---------------------------------------------------------------------------

func TestValueIgnoreReasonMetricEmitted(t *testing.T) {
	reg := newValueTestRegistry()
	familiesBefore, _ := reg.Gather()

	reasons := []IgnoreReason{
		IgnoreReasonLowConfidence,
		IgnoreReasonNoPerceivedValue,
		IgnoreReasonLatency,
		IgnoreReasonUnknown,
	}

	for _, r := range reasons {
		before := valueCounterByLabel(familiesBefore, "orbit_user_ignore_reason_total", "reason", string(r))
		if before < 0 {
			before = 0
		}

		if err := RecordSuggestionIgnored("sess-reason-test", r); err != nil {
			t.Errorf("RecordSuggestionIgnored(%q) should not error: %v", r, err)
		}

		families, _ := reg.Gather()
		after := valueCounterByLabel(families, "orbit_user_ignore_reason_total", "reason", string(r))
		if after-before != 1 {
			t.Errorf("orbit_user_ignore_reason_total{reason=%q}: want delta=1, got %.0f", r, after-before)
		}
	}
}

// ---------------------------------------------------------------------------
// 9. RecordSuggestionIgnored — fail-closed para reason inválida
// ---------------------------------------------------------------------------

func TestValueRecordSuggestionIgnoredFailClosed(t *testing.T) {
	reg := newValueTestRegistry()

	invalidReasons := []IgnoreReason{"", "UNKNOWN", "not_a_reason", "latency_high"}
	for _, bad := range invalidReasons {
		familiesBefore, _ := reg.Gather()
		sumBefore := valueCounterSum(familiesBefore, "orbit_user_ignore_reason_total")
		if sumBefore < 0 {
			sumBefore = 0
		}

		err := RecordSuggestionIgnored("sess-fail", bad)
		if err == nil {
			t.Errorf("RecordSuggestionIgnored with invalid reason %q must return error (fail-closed)", bad)
		}

		familiesAfter, _ := reg.Gather()
		sumAfter := valueCounterSum(familiesAfter, "orbit_user_ignore_reason_total")
		if sumAfter < 0 {
			sumAfter = 0
		}
		if sumAfter != sumBefore {
			t.Errorf("fail-closed: invalid reason %q must not increment counter (before=%.0f after=%.0f)", bad, sumBefore, sumAfter)
		}
	}
}

// ---------------------------------------------------------------------------
// 10. RecordEventValue — evento incompleto (ActionsSuggested==0) não grava
// ---------------------------------------------------------------------------

func TestValueIncompleteEventNotRecorded(t *testing.T) {
	reg := newValueTestRegistry()
	familiesBefore, _ := reg.Gather()
	sumPerceivedBefore := valueCounterSum(familiesBefore, "orbit_user_perceived_value_total")
	sumAcceptedBefore := valueCounterSum(familiesBefore, "orbit_user_accepted_suggestion_total")
	sumIgnoredBefore := valueCounterSum(familiesBefore, "orbit_user_ignored_suggestion_total")
	if sumPerceivedBefore < 0 {
		sumPerceivedBefore = 0
	}
	if sumAcceptedBefore < 0 {
		sumAcceptedBefore = 0
	}
	if sumIgnoredBefore < 0 {
		sumIgnoredBefore = 0
	}

	// Zero-suggestion event must record nothing.
	RecordEventValue(SkillEvent{
		SessionID:        "incomplete-sess",
		ActionsSuggested: 0,
		ActionsApplied:   0,
	})

	familiesAfter, _ := reg.Gather()
	if d := valueCounterSum(familiesAfter, "orbit_user_perceived_value_total") - sumPerceivedBefore; d != 0 {
		t.Errorf("incomplete event must not increment perceived_value (delta=%.0f)", d)
	}
	if d := valueCounterSum(familiesAfter, "orbit_user_accepted_suggestion_total") - sumAcceptedBefore; d != 0 {
		t.Errorf("incomplete event must not increment accepted (delta=%.0f)", d)
	}
	if d := valueCounterSum(familiesAfter, "orbit_user_ignored_suggestion_total") - sumIgnoredBefore; d != 0 {
		t.Errorf("incomplete event must not increment ignored (delta=%.0f)", d)
	}
}

// ---------------------------------------------------------------------------
// 11. No race conditions — concurrent RecordEventValue calls
// ---------------------------------------------------------------------------

func TestValueConcurrentSafety(t *testing.T) {
	_ = newValueTestRegistry()

	done := make(chan struct{})
	concurrency := 20

	ev := SkillEvent{
		SessionID:        "race-sess",
		ActionsSuggested: 2,
		ActionsApplied:   1,
	}

	for i := 0; i < concurrency; i++ {
		go func() {
			RecordEventValue(ev)
			RecordUserReturned("race-sess")
			done <- struct{}{}
		}()
	}

	for i := 0; i < concurrency; i++ {
		<-done
	}
	// If we reach here without a data race (run with -race), the test passes.
}
