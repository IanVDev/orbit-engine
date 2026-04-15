// Command tracking-server exposes Prometheus metrics at /metrics
// and provides a simple health endpoint.
package main

import (
	"log"
	"net/http"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Register metrics on the default Prometheus registry.
	tracking.RegisterMetrics(prometheus.DefaultRegisterer)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	addr := ":9100"
	log.Printf("[orbit-tracking] listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
