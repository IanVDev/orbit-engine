// Command tracking-server exposes Prometheus metrics at /metrics,
// a health endpoint, and a /track endpoint for event ingestion.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Build-time variables injected via -ldflags.
// Example: go build -ldflags "-X main.CommitSHA=abc1234 -X main.Version=1.0.0"
var (
	CommitSHA = "dev"
	Version   = "0.0.0"
	BuildTime = "unknown"
)

func main() {
	// ── CLI flags ──────────────────────────────────────────────────────────
	// --model-control sets the override policy for model changes (Opus/Sonnet).
	// Default = "locked" (fail-closed): no override is ever applied unless the
	// operator explicitly passes --model-control=auto or --model-control=suggest.
	modelControlFlag := flag.String(
		"model-control",
		"locked",
		`Model override policy.
  locked  — never allow Opus/Sonnet override (default, fail-closed)
  auto    — allow override; apply existing routing heuristics
  suggest — return suggestion without applying the override`,
	)
	tokenBudgetSession := flag.Int64(
		"token-budget-session",
		100_000,
		"Maximum tokens allowed per session (cumulative). Default: 100000.",
	)
	tokenBudgetCall := flag.Int64(
		"token-budget-call",
		10_000,
		"Maximum tokens allowed per single call. Default: 10000.",
	)
	flag.Parse()

	// Parse and validate model-control before registering anything.
	// Fail-closed: invalid value → log.Fatalf stops the process immediately.
	modelControl, err := tracking.ParseModelControl(*modelControlFlag)
	if err != nil {
		log.Fatalf("[orbit-tracking] FATAL: %v", err)
	}
	log.Printf("[orbit-tracking] model-control mode: %s", modelControl)

	// Register core metrics on the default Prometheus registry.
	tracking.RegisterMetrics(prometheus.DefaultRegisterer)
	// Register security metrics (rejected_total, behavior_abuse_total, security_mode, etc.)
	// Must be called after RegisterMetrics so the registry is initialized.
	tracking.RegisterSecurityMetrics(prometheus.DefaultRegisterer)
	// Register value-observability metrics (perceived_value, returned, accepted, ignored).
	tracking.RegisterValueMetrics(prometheus.DefaultRegisterer)
	// Register product-layer counters (proofs_generated, quickstart_completed, verify_*).
	tracking.RegisterProductMetrics(prometheus.DefaultRegisterer)
	// Register model-control metrics (override_total, control_mode gauge).
	tracking.RegisterModelControlMetrics(prometheus.DefaultRegisterer)
	// Register token budget metrics (spent_total, per_call histogram, remaining gauge, blocked_total).
	tracking.RegisterTokenBudgetMetrics(prometheus.DefaultRegisterer)
	// Register token reconcile metrics (actual_total, estimation_error gauge).
	tracking.RegisterTokenReconcileMetrics(prometheus.DefaultRegisterer)
	// Register reconcile auth metrics (rejected_total{reason}).
	tracking.RegisterReconcileAuthMetrics(prometheus.DefaultRegisterer)

	tracking.SetSeedMode(false) // orbit_seed_mode = 0 → production

	// Publish active model-control mode as a gauge so dashboards can alert
	// when the mode deviates from the expected (locked) default.
	tracking.SetModelControlModeGauge(modelControl)

	// Heartbeat: increments orbit_heartbeat_total every 15s.
	// Alert fires when rate(orbit_heartbeat_total[1m]) == 0.
	tracking.StartHeartbeat(15 * time.Second)

	startedAt := time.Now().UTC()

	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// /v1/runtime — build identity for deploy validation.
	// Returns commit SHA, version, build time, and operational state.
	// The deploy script validates the commit field against the expected artifact SHA.
	http.HandleFunc("/v1/runtime", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"commit":        CommitSHA,
			"version":       Version,
			"build_time":    BuildTime,
			"started_at":    startedAt.Format(time.RFC3339),
			"model_control": string(modelControl),
		})
	})

	// Token budget governor — fail-fast on cost before governance and routing.
	// Limits: configurable via --token-budget-session and --token-budget-call.
	tokenBudget := tracking.NewTokenBudgetRegistry(*tokenBudgetSession, *tokenBudgetCall)
	log.Printf("[orbit-tracking] token-budget: per_session=%d per_call=%d",
		*tokenBudgetSession, *tokenBudgetCall)

	// /track — pipeline: [TokenBudget] → [ExecGov] → [ModelControl] → [TrackHandler]
	http.HandleFunc("/track", tracking.TrackHandlerWithBudget(
		tokenBudget,
		tracking.TrackHandlerWithControl(modelControl),
	))

	// /reconcile — post-execution budget adjustment, protected by HMAC auth.
	//
	// Secret: ORBIT_RECONCILE_SECRET env var (distinct from ORBIT_HMAC_SECRET
	// used by /track — different trust domain: internal execution layer vs clients).
	// In production (ORBIT_ENV=production) an empty secret is fatal.
	reconcileSecret := os.Getenv("ORBIT_RECONCILE_SECRET")
	if reconcileSecret == "" && tracking.IsProductionMode() {
		log.Fatalf("[orbit-tracking] FATAL: ORBIT_RECONCILE_SECRET is required when ORBIT_ENV=production")
	}
	reconcileAuth := tracking.NewReconcileAuth([]byte(reconcileSecret), 30*time.Second)
	http.HandleFunc("/reconcile", reconcileAuth.Middleware(tracking.ReconcileHandler(tokenBudget)))

	// Fail-closed bind: default to loopback unless ORBIT_BIND_ALL=1 opts in.
	// See tracking/bind.go for rationale.
	addr := tracking.ResolveListenAddr(":9100")
	log.Printf("[orbit-tracking] listening on %s (set %s=1 to bind all interfaces)", addr, tracking.BindAllEnv)
	log.Fatal(http.ListenAndServe(addr, nil))
}
