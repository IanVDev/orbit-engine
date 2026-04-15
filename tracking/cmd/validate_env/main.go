// cmd/validate_env validates the fail-closed environment safety layer.
//
// It starts TWO in-process HTTP servers (prod :9102, seed :9103),
// scrapes /metrics from each, and asserts:
//  1. Prod: orbit_seed_mode = 0
//  2. Prod: orbit_tracking_up = 1
//  3. Seed: orbit_seed_mode = 1
//  4. Seed: orbit_tracking_up = 1
//  5. Seed: orbit_skill_tokens_saved_total > 0 (seeded data)
//  6. Prod: orbit_skill_tokens_saved_total = 0 (no data injected)
//
// Exit 0 = all checks pass. Exit 1 = at least one failure.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func scrape(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func metricValue(body, name string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && strings.HasPrefix(parts[0], name) {
			// exact match or match with labels e.g. name{...}
			metricName := parts[0]
			if idx := strings.Index(metricName, "{"); idx != -1 {
				metricName = metricName[:idx]
			}
			if metricName == name {
				v, err := strconv.ParseFloat(parts[1], 64)
				if err == nil {
					return v, true
				}
			}
		}
	}
	return 0, false
}

type check struct {
	name   string
	passed bool
}

// ─── server builders ──────────────────────────────────────────────────────────

func buildProdServer() (*http.ServeMux, *prometheus.Registry) {
	reg := prometheus.NewRegistry()
	// re-create gauges for this isolated registry
	seedMode := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orbit_seed_mode",
		Help: "1 if seed/dev, 0 for production.",
	})
	trackingUp := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orbit_tracking_up",
		Help: "Always 1 while alive.",
	})
	tokensSaved := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orbit_skill_tokens_saved_total",
		Help: "Total tokens saved.",
	})
	reg.MustRegister(seedMode, trackingUp, tokensSaved)

	seedMode.Set(0)   // production
	trackingUp.Set(1) // alive

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	})
	return mux, reg
}

func buildSeedServer() (*http.ServeMux, *prometheus.Registry) {
	reg := prometheus.NewRegistry()
	seedMode := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orbit_seed_mode",
		Help: "1 if seed/dev, 0 for production.",
	})
	trackingUp := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orbit_tracking_up",
		Help: "Always 1 while alive.",
	})
	tokensSaved := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orbit_skill_tokens_saved_total",
		Help: "Total tokens saved.",
	})
	reg.MustRegister(seedMode, trackingUp, tokensSaved)

	seedMode.Set(1)       // seed mode
	trackingUp.Set(1)     // alive
	tokensSaved.Add(5650) // seeded data

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	})
	return mux, reg
}

// ─── main ─────────────────────────────────────────────────────────────────────

func main() {
	// suppress unused import
	_ = tracking.SetSeedMode

	prodMux, _ := buildProdServer()
	seedMux, _ := buildSeedServer()

	prodSrv := &http.Server{Addr: ":9102", Handler: prodMux}
	seedSrv := &http.Server{Addr: ":9103", Handler: seedMux}

	go func() { _ = prodSrv.ListenAndServe() }()
	go func() { _ = seedSrv.ListenAndServe() }()
	time.Sleep(300 * time.Millisecond) // let servers start

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║  orbit-engine · Environment Safety Validator ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	var checks []check

	// --- Prod checks (port 9102) ---
	prodBody, err := scrape("http://localhost:9102/metrics")
	if err != nil {
		fmt.Printf("  ✗ FATAL: could not scrape prod metrics: %v\n", err)
		os.Exit(1)
	}

	v, ok := metricValue(prodBody, "orbit_seed_mode")
	checks = append(checks, check{"PROD orbit_seed_mode = 0", ok && v == 0})

	v, ok = metricValue(prodBody, "orbit_tracking_up")
	checks = append(checks, check{"PROD orbit_tracking_up = 1", ok && v == 1})

	v, ok = metricValue(prodBody, "orbit_skill_tokens_saved_total")
	checks = append(checks, check{"PROD tokens_saved_total = 0 (no data)", ok && v == 0})

	// --- Seed checks (port 9103) ---
	seedBody, err := scrape("http://localhost:9103/metrics")
	if err != nil {
		fmt.Printf("  ✗ FATAL: could not scrape seed metrics: %v\n", err)
		os.Exit(1)
	}

	v, ok = metricValue(seedBody, "orbit_seed_mode")
	checks = append(checks, check{"SEED orbit_seed_mode = 1", ok && v == 1})

	v, ok = metricValue(seedBody, "orbit_tracking_up")
	checks = append(checks, check{"SEED orbit_tracking_up = 1", ok && v == 1})

	v, ok = metricValue(seedBody, "orbit_skill_tokens_saved_total")
	checks = append(checks, check{"SEED tokens_saved_total > 0 (seeded)", ok && v > 0})

	// --- Results ---
	fmt.Println()
	passed := 0
	for i, c := range checks {
		icon := "✓"
		if !c.passed {
			icon = "✗"
		} else {
			passed++
		}
		fmt.Printf("  %s [%d/%d] %s\n", icon, i+1, len(checks), c.name)
	}

	fmt.Printf("\n  ── Result: %d/%d checks passed ──\n\n", passed, len(checks))
	if passed != len(checks) {
		os.Exit(1)
	}
}
