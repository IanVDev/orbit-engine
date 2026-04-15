// gateway_test.go — Anti-regression tests for the PromQL fail-closed gateway.
//
// These tests use httptest to spin up a fake Prometheus upstream and
// the real gateway handler. No network ports are opened.
package tracking_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/IanVDev/orbit-engine/tracking"
)

// fakePrometheus returns an httptest.Server that echoes a valid
// Prometheus-style JSON response for any query it receives.
func fakePrometheus() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
}

// gatewayFor creates a gateway httptest.Server pointing to the given
// fake upstream. Caller must defer .Close() on both servers.
func gatewayFor(upstream *httptest.Server) *httptest.Server {
	u, _ := url.Parse(upstream.URL)
	gw := tracking.NewGateway(u, upstream.Client())
	return httptest.NewServer(gw.Handler())
}

// -------------------------------------------------------------------------
// Test: valid queries are proxied to Prometheus
// -------------------------------------------------------------------------

func TestGatewayAllowsValidQueries(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"recording_rule_prod", "orbit:tokens_saved_total:prod"},
		{"recording_rule_with_func", `rate(orbit:activations_total:prod[5m])`},
		{"governance_gauge", "orbit_seed_mode"},
		{"governance_tracking_up", "orbit_tracking_up"},
		{"non_orbit_metric", "up"},
		{"instance_id", "orbit_instance_id"},
		{"freshness", "orbit_last_event_timestamp"},
	}

	prom := fakePrometheus()
	defer prom.Close()
	gateway := gatewayFor(prom)
	defer gateway.Close()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// GET request
			resp, err := http.Get(gateway.URL + "/api/v1/query?query=" + url.QueryEscape(tc.query))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
			}

			body, _ := io.ReadAll(resp.Body)
			var envelope map[string]interface{}
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatalf("invalid JSON response: %v", err)
			}
			if envelope["status"] != "success" {
				t.Fatalf("expected status=success, got %v", envelope["status"])
			}
		})
	}
}

// -------------------------------------------------------------------------
// Test: invalid queries are blocked with 400
// -------------------------------------------------------------------------

func TestGatewayBlocksInvalidQueries(t *testing.T) {
	cases := []struct {
		name  string
		query string
		check string // substring expected in the error body
	}{
		{"raw_metric", "orbit_skill_tokens_saved_total", "orbit_skill_"},
		{"raw_in_expression", `rate(orbit_skill_activations_total[5m])`, "orbit_skill_"},
		{"empty_query", "", "empty query"},
		{"whitespace_only", "   ", "empty query"},
		{"unknown_orbit_metric", "orbit_typo_metric", "not in the governance allow-list"},
	}

	prom := fakePrometheus()
	defer prom.Close()
	gateway := gatewayFor(prom)
	defer gateway.Close()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(gateway.URL + "/api/v1/query?query=" + url.QueryEscape(tc.query))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
			}

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)

			// Must return Prometheus-compatible error envelope
			var envelope map[string]interface{}
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatalf("response is not valid JSON: %v — body: %s", err, bodyStr)
			}
			if envelope["status"] != "error" {
				t.Fatalf("expected status=error, got %v", envelope["status"])
			}
			if envelope["errorType"] != "governance" {
				t.Fatalf("expected errorType=governance, got %v", envelope["errorType"])
			}

			// Check the violation reason is in the body
			if tc.check != "" && !strings.Contains(bodyStr, tc.check) {
				t.Fatalf("expected body to contain %q, got: %s", tc.check, bodyStr)
			}
		})
	}
}

// -------------------------------------------------------------------------
// Test: POST form body is also checked
// -------------------------------------------------------------------------

func TestGatewayBlocksPostQuery(t *testing.T) {
	prom := fakePrometheus()
	defer prom.Close()
	gateway := gatewayFor(prom)
	defer gateway.Close()

	// POST with raw metric in form body
	resp, err := http.PostForm(gateway.URL+"/api/v1/query", url.Values{
		"query": {"orbit_skill_waste_estimated"},
	})
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for POST, got %d: %s", resp.StatusCode, body)
	}
}

func TestGatewayAllowsPostValidQuery(t *testing.T) {
	prom := fakePrometheus()
	defer prom.Close()
	gateway := gatewayFor(prom)
	defer gateway.Close()

	resp, err := http.PostForm(gateway.URL+"/api/v1/query", url.Values{
		"query": {"orbit:tokens_saved_total:prod"},
	})
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for valid POST, got %d: %s", resp.StatusCode, body)
	}
}

// -------------------------------------------------------------------------
// Test: /health endpoint
// -------------------------------------------------------------------------

func TestGatewayHealth(t *testing.T) {
	prom := fakePrometheus()
	defer prom.Close()
	gateway := gatewayFor(prom)
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// -------------------------------------------------------------------------
// Test: /query_range is also governed
// -------------------------------------------------------------------------

func TestGatewayBlocksQueryRange(t *testing.T) {
	prom := fakePrometheus()
	defer prom.Close()
	gateway := gatewayFor(prom)
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/api/v1/query_range?query=" + url.QueryEscape("orbit_skill_activations_total") + "&start=0&end=1&step=1")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for query_range, got %d: %s", resp.StatusCode, body)
	}
}

// -------------------------------------------------------------------------
// Test: utility helpers
// -------------------------------------------------------------------------

func TestIsQueryBlocked(t *testing.T) {
	if !tracking.IsQueryBlocked("orbit_skill_tokens_saved_total") {
		t.Fatal("raw metric should be blocked")
	}
	if tracking.IsQueryBlocked("orbit:tokens_saved_total:prod") {
		t.Fatal("recording rule should not be blocked")
	}
}
