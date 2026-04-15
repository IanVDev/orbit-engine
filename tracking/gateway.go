// gateway.go — Fail-closed PromQL gateway handler for orbit-engine.
//
// This file lives in the tracking package so it can be imported by
// both the CLI entrypoint (cmd/gateway) and integration tests.
//
// Every PromQL query passes through ValidatePromQLStrict BEFORE being
// proxied to the upstream Prometheus. Invalid queries never leave the
// gateway.
//
// Production-hardened:
//   - Prometheus metrics: requests, blocked, errors, latency
//   - Upstream fail-closed: unreachable → 503, never fallback
package tracking

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// -------------------------------------------------------------------------
// Gateway metrics — self-observability
// -------------------------------------------------------------------------

var (
	gatewayRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_gateway_requests_total",
			Help: "Total PromQL requests received by the gateway.",
		},
		[]string{"path", "method"},
	)

	gatewayBlockedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_gateway_blocked_total",
			Help: "Requests blocked by governance policy.",
		},
		[]string{"reason"},
	)

	gatewayErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_gateway_errors_total",
			Help: "Upstream errors (timeouts, connection refused, etc.).",
		},
		[]string{"type"},
	)

	gatewayLatencyMs = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "orbit_gateway_latency_ms",
			Help:    "End-to-end latency of proxied requests in milliseconds.",
			Buckets: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		},
	)
)

// RegisterGatewayMetrics registers gateway-specific Prometheus collectors.
// Call once at startup. Safe to call multiple times (idempotent via registry
// error handling).
func RegisterGatewayMetrics(reg prometheus.Registerer) {
	for _, c := range []prometheus.Collector{
		gatewayRequestsTotal,
		gatewayBlockedTotal,
		gatewayErrorsTotal,
		gatewayLatencyMs,
	} {
		// Ignore AlreadyRegisteredError for idempotency in tests.
		if err := reg.Register(c); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				log.Printf("[WARN] gateway metric registration: %v", err)
			}
		}
	}
}

// -------------------------------------------------------------------------
// Gateway — fail-closed reverse proxy
// -------------------------------------------------------------------------

// Gateway is a fail-closed PromQL reverse proxy. It validates every
// query through ValidatePromQLStrict before forwarding to upstream.
type Gateway struct {
	upstream *url.URL
	client   *http.Client
}

// NewGateway creates a gateway that proxies validated queries to upstream.
func NewGateway(upstream *url.URL, client *http.Client) *Gateway {
	return &Gateway{upstream: upstream, client: client}
}

// Handler returns an http.Handler wired with all gateway routes.
// Useful for httptest in integration tests.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/query", g.HandleQuery)
	mux.HandleFunc("/query_range", g.HandleQuery)
	mux.HandleFunc("/api/v1/query", g.HandleQuery)
	mux.HandleFunc("/api/v1/query_range", g.HandleQuery)
	mux.HandleFunc("/health", g.HandleHealth)
	return mux
}

// HandleHealth is a trivial liveness endpoint.
func (g *Gateway) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

// HandleQuery validates the PromQL query and proxies it to Prometheus
// if it passes governance. Invalid queries are rejected with 400.
// Upstream failures return 503 (fail-closed — never fallback).
func (g *Gateway) HandleQuery(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	query := ExtractQuery(r)

	gatewayRequestsTotal.WithLabelValues(r.URL.Path, r.Method).Inc()

	// ── Fail-closed enforcement ────────────────────────────────────
	if err := ValidatePromQLStrict(query); err != nil {
		log.Printf("[WARN] promql-gateway: blocked query — %s", err)
		gatewayBlockedTotal.WithLabelValues("governance").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"status":"error","errorType":"governance","error":%q}`+"\n", err.Error())
		return
	}

	// ── Proxy to upstream ──────────────────────────────────────────
	g.proxyToUpstream(w, r)

	gatewayLatencyMs.Observe(float64(time.Since(start).Milliseconds()))
}

// ExtractQuery pulls the "query" parameter from GET query string or
// POST form body. Returns empty string if absent (fail-closed will
// reject it).
func ExtractQuery(r *http.Request) string {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil {
			if q := r.PostFormValue("query"); q != "" {
				return q
			}
		}
	}
	return r.URL.Query().Get("query")
}

// IsQueryBlocked returns true if the query would be blocked by strict
// governance. Convenience helper for CLI tools.
func IsQueryBlocked(query string) bool {
	return ValidatePromQLStrict(query) != nil
}

// -------------------------------------------------------------------------
// Internal: proxy to upstream Prometheus
// -------------------------------------------------------------------------

// proxyToUpstream forwards the original request to Prometheus and
// copies the response back to the client.
//
// FAIL-CLOSED: if upstream is unreachable → 503 Service Unavailable.
// We never return cached data, fallback results, or empty success
// responses. The caller (Grafana) sees the error and retries.
func (g *Gateway) proxyToUpstream(w http.ResponseWriter, r *http.Request) {
	target := *g.upstream
	target.Path = r.URL.Path
	target.RawQuery = r.URL.RawQuery

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		log.Printf("[ERROR] promql-gateway: failed to build upstream request — %v", err)
		gatewayErrorsTotal.WithLabelValues("request_build").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"status":"error","errorType":"upstream","error":"internal gateway error"}`)
		return
	}

	upReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	upReq.Header.Set("Accept", r.Header.Get("Accept"))

	resp, err := g.client.Do(upReq)
	if err != nil {
		log.Printf("[ERROR] promql-gateway: upstream unreachable — %v", err)
		gatewayErrorsTotal.WithLabelValues("upstream_unreachable").Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"status":"error","errorType":"upstream","error":"upstream unreachable"}`)
		return
	}
	defer resp.Body.Close()

	// Copy upstream response headers and status.
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
