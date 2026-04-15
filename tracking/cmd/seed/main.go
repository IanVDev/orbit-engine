// Command seed populates a standalone :9101 metrics server with synthetic
// orbit-engine events for local Prometheus validation.
//
// Usage:
//
//	cd tracking && go run ./cmd/seed
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Register on a fresh registry so we don't conflict with any running server.
	reg := prometheus.NewRegistry()
	tracking.RegisterMetrics(reg)
	tracking.SetSeedMode(true) // orbit_seed_mode = 1 → this is NOT production

	tracker := tracking.NewSessionTracker()

	// ── Scenario A: session WITH skill activation ─────────────────────────
	fmt.Println("▶  Seeding Scenario A: sess-with-skill")
	eventsA := []tracking.SkillEvent{
		{SessionID: "sess-with-skill", EventType: "suggestion", Mode: "auto", Trigger: "correction_chain", EstimatedWaste: 800, ImpactEstimatedToken: 500, Timestamp: time.Now()},
		{SessionID: "sess-with-skill", EventType: "suggestion", Mode: "auto", Trigger: "correction_chain", EstimatedWaste: 1000, ImpactEstimatedToken: 700, Timestamp: time.Now().Add(time.Second)},
		{SessionID: "sess-with-skill", EventType: "activation", Mode: "auto", Trigger: "correction_chain", EstimatedWaste: 1200, ImpactEstimatedToken: 900, Timestamp: time.Now().Add(2 * time.Second)},
		{SessionID: "sess-with-skill", EventType: "suggestion", Mode: "suggest", Trigger: "correction_chain", EstimatedWaste: 600, ImpactEstimatedToken: 400, Timestamp: time.Now().Add(3 * time.Second)},
	}
	for _, ev := range eventsA {
		if _, err := tracker.RecordEvent(ev); err != nil {
			log.Printf("[WARN] A: %v", err)
		}
	}
	fmt.Printf("   %d events recorded\n", len(eventsA))

	// ── Scenario B: session WITHOUT skill activation (21 events) ──────────
	fmt.Println("▶  Seeding Scenario B: sess-no-skill (21 events)")
	for i := 0; i < 21; i++ {
		ev := tracking.SkillEvent{
			SessionID:            "sess-no-skill",
			EventType:            "suggestion",
			Mode:                 "auto",
			Trigger:              "idle",
			EstimatedWaste:       float64(50 + i*10),
			ImpactEstimatedToken: int64(100 + i*5),
			Timestamp:            time.Now().Add(time.Duration(i) * time.Second),
		}
		if _, err := tracker.RecordEvent(ev); err != nil {
			log.Printf("[WARN] B[%d]: %v", i, err)
		}
	}
	fmt.Println("   21 events recorded — no-skill detector should have fired")

	// ── Expose /metrics on :9101 for Prometheus scrape ────────────────────
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	fmt.Println()
	fmt.Println("▶  Metrics server listening on :9101")
	fmt.Println("   Add to prometheus.yml:")
	fmt.Println("     - job_name: orbit-engine-seed")
	fmt.Println("       static_configs:")
	fmt.Println("         - targets: [\"localhost:9101\"]")
	fmt.Println()
	fmt.Println("   Press Ctrl+C to stop.")
	log.Fatal(http.ListenAndServe(":9101", mux))
}
