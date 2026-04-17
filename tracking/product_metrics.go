// product_metrics.go — User-value counters for orbit-engine product layer.
//
// These counters answer product questions — "is orbit actually being used?"
// — which are distinct from existing technical metrics ("does the server
// respond, is auth working"). Each counter maps to a concrete, observable
// action the user takes.
//
// Counters exposed:
//
//   - orbit_proofs_generated_total       — one /track event accepted → one
//     proof minted (ComputeHash inside TrackSkillEvent).
//   - orbit_quickstart_completed_total   — `orbit quickstart` finished with
//     exit code 0 (full 3/3 flow, proof verified).
//   - orbit_verify_success_total         — a recomputed hash matched the
//     stored proof.
//   - orbit_verify_failure_total         — a recomputed hash did NOT match
//     the stored proof (tamper / drift signal).
//
// NOTE: orbit_hygiene_installations_total is NOT implemented in main.
// The `orbit hygiene install` command lives on branch
// chore/ci-regression-guards and has not been merged. When that branch
// lands, open a follow-up (G2.1) to add the counter and wire it into the
// install command — do NOT pre-register it here with no increment site,
// because an always-zero counter is worse than a missing one: it lies
// about being instrumented.
//
// Fail-closed semantics:
//   - Record* helpers only increment after the observable action has
//     already succeeded (the caller owns the success branch).
//   - No partial events: failure of the underlying action bumps the
//     failure counter, never both.
//   - Register once per registry; subsequent registrations on the same
//     registry will panic by design (duplicate collector).
package tracking

import "github.com/prometheus/client_golang/prometheus"

var (
	// orbit_proofs_generated_total — total sha256 proofs minted by a
	// successful TrackSkillEvent call.
	proofsGeneratedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_proofs_generated_total",
			Help: "Total cryptographic proofs (sha256) minted by successful TrackSkillEvent calls.",
		},
	)

	// orbit_quickstart_completed_total — `orbit quickstart` finished 3/3
	// with proof verification passing.
	quickstartCompletedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_quickstart_completed_total",
			Help: "Total successful completions of the `orbit quickstart` onboarding flow.",
		},
	)

	// orbit_verify_success_total — a proof verification matched.
	verifySuccessTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_verify_success_total",
			Help: "Total proof verifications where the recomputed hash matched the stored proof.",
		},
	)

	// orbit_verify_failure_total — a proof verification did NOT match.
	// Non-zero values indicate tampering, clock drift, or a bug.
	verifyFailureTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_verify_failure_total",
			Help: "Total proof verifications where the recomputed hash did NOT match the stored proof.",
		},
	)
)

// RegisterProductMetrics registers the product-layer counters on the given
// registerer. Must be called once per registry (calling twice panics).
func RegisterProductMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		proofsGeneratedTotal,
		quickstartCompletedTotal,
		verifySuccessTotal,
		verifyFailureTotal,
	)
}

// RecordProofGenerated bumps orbit_proofs_generated_total. Call exactly once
// per proof minted — typically from TrackSkillEvent after all validation
// and side effects have succeeded.
func RecordProofGenerated() { proofsGeneratedTotal.Inc() }

// RecordQuickstartCompleted bumps orbit_quickstart_completed_total. Call
// only at the very end of `orbit quickstart`, after proof verification has
// passed and the summary has been printed.
func RecordQuickstartCompleted() { quickstartCompletedTotal.Inc() }

// RecordVerifySuccess bumps orbit_verify_success_total. Call when a
// recomputed proof matches the stored proof.
func RecordVerifySuccess() { verifySuccessTotal.Inc() }

// RecordVerifyFailure bumps orbit_verify_failure_total. Call when a
// recomputed proof does NOT match the stored proof.
func RecordVerifyFailure() { verifyFailureTotal.Inc() }
