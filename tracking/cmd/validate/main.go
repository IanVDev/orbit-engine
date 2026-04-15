// Command validate runs an end-to-end smoke test of the orbit-engine
// tracking system WITHOUT modifying any production code.
//
// It:
//  1. Starts an in-process HTTP server with /metrics + /track endpoints
//  2. Injects Scenario A — session with skill activation
//  3. Injects Scenario B — session with 21 events and NO activation (should
//     trip the no-skill detector)
//  4. Scrapes /metrics and prints observed values
//  5. Validates expected counter/gauge values and exits non-zero on failure
//
// Run:
//
//	cd tracking && go run ./cmd/validate
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---------------------------------------------------------------------------
// In-process HTTP server
// ---------------------------------------------------------------------------

func startServer() (addr string, reg *prometheus.Registry) {
	reg = prometheus.NewRegistry()
	tracking.RegisterMetrics(reg)

	tracker := tracking.NewSessionTracker()
	mux := http.NewServeMux()

	// /metrics — standard Prometheus scrape endpoint
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	// /health
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// /track — accepts a JSON SkillEvent, records via SessionTracker
	mux.HandleFunc("/track", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		var ev tracking.SkillEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			http.Error(w, fmt.Sprintf("decode error: %v", err), http.StatusBadRequest)
			return
		}
		ev.Timestamp = tracking.NowUTC()

		result, err := tracker.RecordEvent(ev)
		if err != nil {
			http.Error(w, fmt.Sprintf("tracking error: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})

	// Pick a free port automatically
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	addr = ln.Addr().String()
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	// Wait until /health responds
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func postEvent(addr string, ev tracking.SkillEvent) {
	body, _ := json.Marshal(ev)
	resp, err := http.Post(
		"http://"+addr+"/track",
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		log.Fatalf("POST /track: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Fatalf("POST /track returned %d: %s", resp.StatusCode, b)
	}
}

func scrapeMetrics(addr string) string {
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		log.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// parseMetricValue scans Prometheus text format for the first line matching
// the given metric name (prefix) and returns the numeric value as a string.
func parseMetricValue(raw, metricName string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		// counter with label: orbit_skill_activations_total{mode="auto"} 3
		// counter no label:   orbit_skill_tokens_saved_total 2400
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		// strip labels
		if idx := strings.Index(name, "{"); idx != -1 {
			name = name[:idx]
		}
		if name == metricName {
			return parts[len(parts)-1]
		}
	}
	return "<not found>"
}

// parseAllValues returns all lines for a given metric name (covers label
// variants like mode="auto").
func parseAllValues(raw, metricName string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		if idx := strings.Index(name, "{"); idx != -1 {
			name = name[:idx]
		}
		if name == metricName {
			out = append(out, line)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

type check struct {
	label    string
	got      string
	expected string
	pass     bool
}

func assertEqual(label, got, expected string) check {
	return check{label: label, got: got, expected: expected, pass: got == expected}
}

func assertNotZero(label, got string) check {
	pass := got != "0" && got != "<not found>"
	return check{label: label, got: got, expected: "!= 0", pass: pass}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║  orbit-engine · tracking validation                     ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()

	// ── 1. Start server ────────────────────────────────────────────────────
	addr, _ := startServer()
	fmt.Printf("▶  server up at http://%s\n\n", addr)

	// ── 2. Scenario A — session WITH skill activation ──────────────────────
	fmt.Println("── Scenario A: session with skill activation ──────────────")
	sessionA := "sess-with-skill"
	eventsA := []tracking.SkillEvent{
		{SessionID: sessionA, EventType: "suggestion", Mode: "auto", Trigger: "correction_chain", EstimatedWaste: 800, ImpactEstimatedToken: 500},
		{SessionID: sessionA, EventType: "suggestion", Mode: "auto", Trigger: "correction_chain", EstimatedWaste: 1000, ImpactEstimatedToken: 700},
		{SessionID: sessionA, EventType: "activation", Mode: "auto", Trigger: "correction_chain", EstimatedWaste: 1200, ImpactEstimatedToken: 900},
		{SessionID: sessionA, EventType: "suggestion", Mode: "suggest", Trigger: "correction_chain", EstimatedWaste: 600, ImpactEstimatedToken: 400},
	}
	for i, ev := range eventsA {
		postEvent(addr, ev)
		fmt.Printf("   sent event %d: type=%-12s mode=%-8s tokens=%d\n",
			i+1, ev.EventType, ev.Mode, ev.ImpactEstimatedToken)
	}
	fmt.Println()

	// ── 3. Scenario B — session WITHOUT skill activation (21 events) ───────
	fmt.Println("── Scenario B: session WITHOUT skill (21 events) ──────────")
	sessionB := "sess-no-skill"
	for i := 0; i < 21; i++ {
		ev := tracking.SkillEvent{
			SessionID:            sessionB,
			EventType:            "suggestion",
			Mode:                 "auto",
			Trigger:              "idle",
			EstimatedWaste:       float64(50 + i*10),
			ImpactEstimatedToken: int64(100 + i*5),
		}
		postEvent(addr, ev)
	}
	fmt.Printf("   sent 21 events — no activation in this session\n")
	fmt.Println()

	// ── 4. Scrape /metrics ─────────────────────────────────────────────────
	raw := scrapeMetrics(addr)

	// ── 5. Print observed values ───────────────────────────────────────────
	fmt.Println("── Observed metrics (/metrics scrape) ──────────────────────")
	metricsOfInterest := []string{
		"orbit_skill_activations_total",
		"orbit_skill_tokens_saved_total",
		"orbit_skill_waste_estimated",
		"orbit_skill_sessions_total",
		"orbit_skill_sessions_with_activation_total",
		"orbit_skill_sessions_without_activation_total",
		"orbit_skill_tracking_failures_total",
	}
	for _, name := range metricsOfInterest {
		lines := parseAllValues(raw, name)
		if len(lines) == 0 {
			fmt.Printf("   %-52s  <not found>\n", name)
		}
		for _, l := range lines {
			fmt.Printf("   %s\n", l)
		}
	}
	fmt.Println()

	// ── 6. Validate ────────────────────────────────────────────────────────
	fmt.Println("── Validation checks ───────────────────────────────────────")

	// orbit_skill_activations_total{mode="auto"} should be ≥ 1
	// (Scenario A sent 1 activation with mode=auto)
	autoActivations := parseMetricValue(raw, "orbit_skill_activations_total")
	// (first line returned is mode="auto" since that was first)

	// tokens saved: 500+700+900+400 (A) + 21*(100..200) (B) = 2500 + sum(B)
	// sum(B): tokens 100,105,...205 = 21 terms, first=100, last=200, sum=21*150=3150
	// total = 2500 + 3150 = 5650
	tokensSaved := parseMetricValue(raw, "orbit_skill_tokens_saved_total")

	// waste gauge: last event posted was sess-no-skill event 21 with waste=50+20*10=250
	wasteGauge := parseMetricValue(raw, "orbit_skill_waste_estimated")

	// sessions: 2 distinct sessions started
	sessionsTotal := parseMetricValue(raw, "orbit_skill_sessions_total")

	// sessions with activation: 1 (only sess-with-skill)
	sessionsWithActivation := parseMetricValue(raw, "orbit_skill_sessions_with_activation_total")

	// sessions without activation: 1 (sess-no-skill hit threshold at event 20)
	sessionsWithoutActivation := parseMetricValue(raw, "orbit_skill_sessions_without_activation_total")

	// tracking failures: 0 (all events valid)
	trackingFailures := parseMetricValue(raw, "orbit_skill_tracking_failures_total")

	checks := []check{
		assertNotZero("orbit_skill_activations_total (auto) is recorded", autoActivations),
		assertNotZero("orbit_skill_tokens_saved_total > 0", tokensSaved),
		assertNotZero("orbit_skill_waste_estimated > 0", wasteGauge),
		assertEqual("orbit_skill_sessions_total == 2", sessionsTotal, "2"),
		assertEqual("orbit_skill_sessions_with_activation_total == 1", sessionsWithActivation, "1"),
		assertEqual("orbit_skill_sessions_without_activation_total == 1", sessionsWithoutActivation, "1"),
		assertEqual("orbit_skill_tracking_failures_total == 0", trackingFailures, "0"),
	}

	allPass := true
	for _, c := range checks {
		icon := "✅"
		if !c.pass {
			icon = "❌"
			allPass = false
		}
		fmt.Printf("   %s  %-56s  got=%s\n", icon, c.label, c.got)
	}
	fmt.Println()

	// ── 7. Final verdict ───────────────────────────────────────────────────
	fmt.Println("── Result ──────────────────────────────────────────────────")
	if allPass {
		fmt.Println("   ✅  ALL CHECKS PASSED — orbit-engine tracking is working correctly")
		fmt.Println()
		os.Exit(0)
	} else {
		fmt.Println("   ❌  SOME CHECKS FAILED — see above for details")
		fmt.Println()
		os.Exit(1)
	}
}
