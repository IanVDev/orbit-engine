// product_metrics_test.go — Anti-regression tests for product-layer counters.
//
// One test per counter (fail-closed):
//
//   - proofs_generated      → incremented by a successful TrackSkillEvent.
//   - quickstart_completed  → incremented by RecordQuickstartCompleted only.
//   - verify_success        → incremented by RecordVerifySuccess only.
//   - verify_failure        → incremented by RecordVerifyFailure only.
//
// The counters are package globals (registering them twice on the same
// registry panics), so tests compare snapshots before/after rather than
// asserting absolute values.
package tracking

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
)

// counterValueFromCollector reads a single counter's current value via its
// Write method. Works regardless of which registry owns the collector.
func counterValueFromCollector(t *testing.T, c interface {
	Write(*dto.Metric) error
}) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("counter Write: %v", err)
	}
	if m.GetCounter() == nil {
		t.Fatalf("metric is not a counter")
	}
	return m.GetCounter().GetValue()
}

func TestProductMetrics_ProofsGenerated_IncrementsOnTrackSuccess(t *testing.T) {
	before := counterValueFromCollector(t, proofsGeneratedTotal)

	// TrackSkillEvent runs inside the test binary's existing registry;
	// a valid event is enough — we don't need /metrics scraping.
	now := NowUTC()
	event := SkillEvent{
		EventType:            "skill_activation",
		SessionID:            "test-proofs-" + t.Name(),
		Timestamp:            now,
		Mode:                 "auto",
		Trigger:              "unit_test",
		EstimatedWaste:       0,
		ActionsSuggested:     1,
		ActionsApplied:       1,
		ImpactEstimatedToken: 10,
	}
	if err := TrackSkillEvent(event); err != nil {
		t.Fatalf("TrackSkillEvent: %v", err)
	}

	after := counterValueFromCollector(t, proofsGeneratedTotal)
	if after-before != 1 {
		t.Fatalf("proofs_generated delta = %v; want 1 (before=%v after=%v)",
			after-before, before, after)
	}
}

func TestProductMetrics_ProofsGenerated_NotIncrementedOnInvalidEvent(t *testing.T) {
	before := counterValueFromCollector(t, proofsGeneratedTotal)

	// Invalid event: empty SessionID fails Validate().
	bad := SkillEvent{
		EventType: "skill_activation",
		SessionID: "", // invalid
		Timestamp: NowUTC(),
		Mode:      "auto",
	}
	if err := TrackSkillEvent(bad); err == nil {
		t.Fatalf("expected validation error on empty session_id")
	}

	after := counterValueFromCollector(t, proofsGeneratedTotal)
	if after != before {
		t.Fatalf("proofs_generated moved on failed TrackSkillEvent: before=%v after=%v",
			before, after)
	}
}

func TestProductMetrics_QuickstartCompleted_IncrementsOnce(t *testing.T) {
	before := counterValueFromCollector(t, quickstartCompletedTotal)
	RecordQuickstartCompleted()
	RecordQuickstartCompleted()
	after := counterValueFromCollector(t, quickstartCompletedTotal)
	if after-before != 2 {
		t.Fatalf("quickstart_completed delta = %v; want 2", after-before)
	}
}

func TestProductMetrics_VerifySuccess_IncrementsIndependently(t *testing.T) {
	sBefore := counterValueFromCollector(t, verifySuccessTotal)
	fBefore := counterValueFromCollector(t, verifyFailureTotal)

	RecordVerifySuccess()

	sAfter := counterValueFromCollector(t, verifySuccessTotal)
	fAfter := counterValueFromCollector(t, verifyFailureTotal)

	if sAfter-sBefore != 1 {
		t.Errorf("verify_success delta = %v; want 1", sAfter-sBefore)
	}
	if fAfter != fBefore {
		t.Errorf("verify_failure should not move on success path: before=%v after=%v",
			fBefore, fAfter)
	}
}

func TestProductMetrics_VerifyFailure_IncrementsIndependently(t *testing.T) {
	sBefore := counterValueFromCollector(t, verifySuccessTotal)
	fBefore := counterValueFromCollector(t, verifyFailureTotal)

	RecordVerifyFailure()

	sAfter := counterValueFromCollector(t, verifySuccessTotal)
	fAfter := counterValueFromCollector(t, verifyFailureTotal)

	if fAfter-fBefore != 1 {
		t.Errorf("verify_failure delta = %v; want 1", fAfter-fBefore)
	}
	if sAfter != sBefore {
		t.Errorf("verify_success should not move on failure path: before=%v after=%v",
			sBefore, sAfter)
	}
}
