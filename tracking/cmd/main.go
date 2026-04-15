// Command tracking-server exposes Prometheus metrics at /metrics,
// a health endpoint, and a /track endpoint for event ingestion.
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Register metrics on the default Prometheus registry.
	tracking.RegisterMetrics(prometheus.DefaultRegisterer)
	tracking.SetSeedMode(false) // orbit_seed_mode = 0 → production

	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// /track — ingest a SkillEvent and update Prometheus metrics.
	// Accepts POST with JSON body matching SkillEvent.
	http.HandleFunc("/track", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var event tracking.SkillEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = tracking.NowUTC()
		}
		if err := tracking.TrackSkillEvent(event); err != nil {
			log.Printf("[ERROR] /track: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	addr := ":9100"
	log.Printf("[orbit-tracking] listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
