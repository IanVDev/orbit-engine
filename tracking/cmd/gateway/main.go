// cmd/gateway/main.go — Entrypoint for the fail-closed PromQL gateway.
//
// All handler logic lives in the tracking package (gateway.go).
// This file is only the CLI wrapper.
//
// Usage:
//
//	go run ./cmd/gateway -listen :9091 -upstream http://localhost:9090
//
// Self-observability:
//
//	curl http://localhost:9091/metrics  → gateway Prometheus metrics
package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const proxyTimeout = 10 * time.Second

func main() {
	listen := flag.String("listen", ":9091", "address to listen on")
	upstream := flag.String("upstream", "http://localhost:9090", "Prometheus upstream URL")
	flag.Parse()

	upstreamURL, err := url.Parse(*upstream)
	if err != nil {
		log.Fatalf("invalid upstream URL %q: %v", *upstream, err)
	}

	// Register gateway self-metrics on default registry.
	tracking.RegisterGatewayMetrics(prometheus.DefaultRegisterer)

	gw := tracking.NewGateway(upstreamURL, &http.Client{Timeout: proxyTimeout})

	// Wrap the gateway mux to add /metrics for self-observability.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", gw.Handler())

	log.Printf("[GATEWAY] listening on %s → upstream %s", *listen, *upstream)
	log.Fatal(http.ListenAndServe(*listen, mux))
}
