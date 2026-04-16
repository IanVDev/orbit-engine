// Command tracking-server exposes Prometheus metrics at /metrics,
// a health endpoint, and a /track endpoint for event ingestion.
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Register core metrics on the default Prometheus registry.
	tracking.RegisterMetrics(prometheus.DefaultRegisterer)
	// Register security metrics (rejected_total, behavior_abuse_total, security_mode, etc.)
	// Must be called after RegisterMetrics so the registry is initialized.
	tracking.RegisterSecurityMetrics(prometheus.DefaultRegisterer)
	tracking.SetSeedMode(false) // orbit_seed_mode = 0 → production

	// Heartbeat: increments orbit_heartbeat_total every 15s.
	// Alert fires when rate(orbit_heartbeat_total[1m]) == 0.
	tracking.StartHeartbeat(15 * time.Second)

	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// /track — canonical handler shared with tests via tracking.TrackHandler().
	http.HandleFunc("/track", tracking.TrackHandler())

	addr := ":9100"
	log.Printf("[orbit-tracking] listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
