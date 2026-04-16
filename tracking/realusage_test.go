// realusage_test.go — End-to-end validation of real usage ingestion.
//
// Tests the full pipeline:
//
//	RealUsageClient → POST /track → TrackSkillEvent → Prometheus metrics
//
// All three contract assertions are verified:
//  1. event_accepted:         POST /track returns 200 {"status":"ok"}
//  2. metrics_incremented:    orbit_real_usage_total and orbit_skill_activations_total > 0
//  3. appears_in_metrics:     GET /metrics contains the expected metric names
//
// Run:
//
//	cd tracking && go test -run TestRealUsageClient -v
package tracking

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

// TestRealUsageClientEndToEnd validates the full real-usage ingestion pipeline.
func TestRealUsageClientEndToEnd(t *testing.T) {
	// ── Isolated registry (does not touch prometheus.DefaultRegisterer) ──
	ResetRateLimit() // clears token buckets + dedup
	SetHMACSecret("") // ensure HMAC is disabled for this test
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
		skillActivationLatency,
		heartbeatTotal,
		realUsageTotal,
		skillActivationByReason,
		lastRealUsageTimestamp,
	)
	trackingUpGauge.Set(1)
	seedModeGauge.Set(0)
	const testInstance = "real-usage-e2e-test"
	instanceIDGauge.WithLabelValues(testInstance).Set(1)
	t.Cleanup(func() { instanceIDGauge.DeleteLabelValues(testInstance) })

	// ── In-process server: /track + /metrics ─────────────────────────────
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/track", TrackHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// ── Client targeting the test server ─────────────────────────────────
	sessionID := "real-usage-e2e-" + t.Name()
	client := NewRealUsageClient(srv.URL, sessionID, "auto")

	// ── 1. event_accepted ────────────────────────────────────────────────
	t.Run("event_accepted", func(t *testing.T) {
		err := client.TrackPromptUsage(
			context.Background(),
			"What is the capital of France?",
			"The capital of France is Paris.",
		)
		if err != nil {
			t.Fatalf("TrackPromptUsage returned error: %v", err)
		}
	})

	// ── 2. metrics_incremented ────────────────────────────────────────────
	t.Run("metrics_incremented", func(t *testing.T) {
		families, err := reg.Gather()
		if err != nil {
			t.Fatalf("reg.Gather() failed: %v", err)
		}
		fm := toFamilyMap(families)

		// orbit_real_usage_total >= 1
		checkCounter(t, fm, "orbit_real_usage_total", 1)

		// orbit_skill_activations_total{mode="auto"} >= 1
		checkCounterVecLabel(t, fm, "orbit_skill_activations_total", "mode", "auto", 1)

		// orbit_skill_tokens_saved_total >= 1 (EstimateTokens("The capital…") >= 1)
		checkCounter(t, fm, "orbit_skill_tokens_saved_total", 1)
	})

	// ── 3. appears_in_metrics_endpoint ────────────────────────────────────
	t.Run("appears_in_metrics_endpoint", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/metrics returned HTTP %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		text := string(body)

		for _, name := range []string{
			"orbit_real_usage_total",
			"orbit_skill_activations_total",
			"orbit_skill_tokens_saved_total",
			"orbit_heartbeat_total",
		} {
			if !strings.Contains(text, name) {
				t.Errorf("/metrics missing %q", name)
			} else {
				t.Logf("/metrics contains %q ✓", name)
			}
		}
	})
}

// TestEstimateTokens validates the token estimation heuristic.
func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		text string
		want int64
	}{
		{"", 1},                                // minimum 1
		{"abc", 1},                             // 3/4 → 0, clamped to 1
		{"abcd", 1},                            // 4/4 → 1
		{"abcdefgh", 2},                        // 8/4 → 2
		{"The capital of France is Paris.", 7}, // 31/4 → 7
	}
	for _, c := range cases {
		got := EstimateTokens(c.text)
		if got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}

// TestRealUsageClientFailClosed verifies that unreachable server returns error.
func TestRealUsageClientFailClosed(t *testing.T) {
	client := NewRealUsageClient("http://127.0.0.1:1", "fail-closed-test", "auto")
	err := client.TrackPromptUsage(context.Background(), "input", "output")
	if err == nil {
		t.Fatal("expected error when tracking server is unreachable, got nil")
	}
	t.Logf("fail-closed: got expected error: %v", err)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func toFamilyMap(families []*dto.MetricFamily) map[string]*dto.MetricFamily {
	m := make(map[string]*dto.MetricFamily, len(families))
	for _, f := range families {
		m[f.GetName()] = f
	}
	return m
}

func checkCounter(t *testing.T, fm map[string]*dto.MetricFamily, name string, minVal float64) {
	t.Helper()
	f, ok := fm[name]
	if !ok {
		t.Fatalf("%s not found in registry", name)
	}
	if len(f.GetMetric()) == 0 {
		t.Fatalf("%s has no metric series", name)
	}
	v := f.GetMetric()[0].GetCounter().GetValue()
	if v < minVal {
		t.Errorf("%s = %v, want >= %v", name, v, minVal)
	} else {
		t.Logf("%s = %v ✓", name, v)
	}
}

func checkCounterVecLabel(t *testing.T, fm map[string]*dto.MetricFamily,
	name, labelName, labelValue string, minVal float64) {
	t.Helper()
	f, ok := fm[name]
	if !ok {
		t.Fatalf("%s not found in registry", name)
	}
	for _, m := range f.GetMetric() {
		for _, lp := range m.GetLabel() {
			if lp.GetName() == labelName && lp.GetValue() == labelValue {
				v := m.GetCounter().GetValue()
				if v < minVal {
					t.Errorf("%s{%s=%q} = %v, want >= %v", name, labelName, labelValue, v, minVal)
				} else {
					t.Logf("%s{%s=%q} = %v ✓", name, labelName, labelValue, v)
				}
				return
			}
		}
	}
	t.Errorf("%s{%s=%q} not found", name, labelName, labelValue)
}
