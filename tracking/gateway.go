// gateway.go — Fail-closed PromQL gateway handler for orbit-engine.
//
// This file lives in the tracking package so it can be imported by
// both the CLI entrypoint (cmd/gateway) and integration tests.
//
// Every PromQL query passes through ValidatePromQLStrict BEFORE being
// proxied to the upstream Prometheus. Invalid queries never leave the
// gateway.
package tracking

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

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
func (g *Gateway) HandleQuery(w http.ResponseWriter, r *http.Request) {
	query := ExtractQuery(r)

	// ── Fail-closed enforcement ────────────────────────────────────
	if err := ValidatePromQLStrict(query); err != nil {
		log.Printf("[WARN] blocked query: %s", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		// Prometheus-compatible error envelope so Grafana shows a clean
		// message instead of a cryptic parse error.
		fmt.Fprintf(w, `{"status":"error","errorType":"governance","error":%q}`+"\n", err.Error())
		return
	}

	// ── Proxy to upstream ──────────────────────────────────────────
	g.proxyToUpstream(w, r)
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
func (g *Gateway) proxyToUpstream(w http.ResponseWriter, r *http.Request) {
	// Build upstream URL keeping the original path and query string.
	target := *g.upstream
	target.Path = r.URL.Path
	target.RawQuery = r.URL.RawQuery

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		log.Printf("[ERROR] failed to build upstream request: %v", err)
		http.Error(w, `{"status":"error","error":"internal gateway error"}`, http.StatusBadGateway)
		return
	}

	// Copy essential headers.
	upReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	upReq.Header.Set("Accept", r.Header.Get("Accept"))

	resp, err := g.client.Do(upReq)
	if err != nil {
		log.Printf("[ERROR] upstream request failed: %v", err)
		http.Error(w, `{"status":"error","error":"upstream unreachable"}`, http.StatusBadGateway)
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
