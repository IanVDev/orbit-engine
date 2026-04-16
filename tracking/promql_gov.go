// promql_gov.go — Fail-closed PromQL governance for orbit-engine.
//
// All production dashboards and alerts MUST use recording-rule metrics
// (prefixed "orbit:"). Direct use of raw "orbit_skill_*" metrics is
// FORBIDDEN because those series exist in both prod and seed scrape
// targets and will silently mix environments without an {env=...} filter.
//
// ValidatePromQL enforces this policy at the string level — no external
// PromQL parser is required, keeping dependencies minimal.
//
// Cardinality protection: ValidatePromQLStrict also rejects queries that
// use high-cardinality label names (client_id, session_id, event_id, etc.)
// as label selectors, preventing unbounded series creation.
package tracking

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Governance policy constants
// ---------------------------------------------------------------------------

// _forbiddenPrefix is the raw metric prefix that MUST NOT appear in
// production PromQL queries. These metrics are only safe inside
// recording-rule definitions (orbit_rules.yml).
const _forbiddenPrefix = "orbit_skill_"

// _allowedPrefixes lists metric prefixes that ARE safe for production use.
// Queries referencing only these will pass validation.
var _allowedPrefixes = []string{
	"orbit:",                                     // recording rules (orbit:tokens_saved_total:prod, etc.)
	"orbit_seed_mode",                            // governance gauge
	"orbit_tracking_up",                          // liveness gauge
	"orbit_instance_id",                          // instance identity
	"orbit_last_event_timestamp",                 // freshness gauge
	"orbit_gateway_",                             // gateway self-observability (infra, not skill data)
	"orbit_heartbeat_total",                      // process liveness heartbeat counter
	"orbit_real_usage_total",                     // total valid events ingested (all real usage)
	"orbit_skill_activation_total",               // SkillRouter decision metric {reason, phase}
	"orbit_last_real_usage_timestamp",            // freshness gauge for real_usage_client events
	"orbit_tracking_dedup_blocked_total",         // security: dedup rejections
	"orbit_tracking_hmac_failures_total",         // security: HMAC auth failures
	"orbit_tracking_cleanup_total",               // security: cleanup evictions
	"orbit_real_usage_alive",                     // security: real usage liveness (1/0)
	"orbit_tracking_token_bucket_rejected_total", // security: token bucket rejections
	"orbit_tracking_rejected_total",              // security: unified rejection metric {reason}
	"orbit_behavior_abuse_total",                 // security: behavior abuse detection counter
	"orbit_behavior_abuse_ratio",                 // security: behavior abuse similarity ratio gauge
	"orbit_security_mode",                        // security: active security mode gauge {mode}
	"orbit_security_mode_reason",                 // security: reason for current mode transition
	"orbit_security_mode_transitions_total",      // security: mode transition counter {from, to}
	// Value-observability metrics (user-perceived value layer)
	"orbit_user_perceived_value_total",     // value: perceived value events {level}
	"orbit_user_returned_total",            // value: retention signal {fingerprint} (pseudonymous)
	"orbit_user_accepted_suggestion_total", // value: suggestion accepted counter
	"orbit_user_ignored_suggestion_total",  // value: suggestion ignored counter
	"orbit_user_ignore_reason_total",       // value: inferred ignore reason {reason}
	// Model-control metrics (override governance layer)
	"orbit_model_override_total", // model-ctrl: override attempts {from, to, control, allowed}
	"orbit_model_control_mode",   // model-ctrl: active control mode gauge {mode}
	// Execution-governance metrics (exec-gov layer — runs before model_control)
	"orbit_exec_gov_blocked_total",               // exec-gov: blocked verdicts {block_rule}
	"orbit_exec_gov_allowed_total",               // exec-gov: allowed verdicts
	"orbit_exec_gov_validation_duration_seconds", // exec-gov: validation latency histogram
	// Token-budget metrics (cost-governor layer)
	"orbit_token_spent_total",        // budget: tokens consumed {session_id}
	"orbit_token_per_call",           // budget: per-call token histogram (buckets, sum, count)
	"orbit_token_budget_remaining",   // budget: remaining budget gauge
	"orbit_token_allowed_total",      // budget: calls within budget {block_reason}
	"orbit_token_blocked_total",      // budget: calls blocked {block_reason}
	"orbit_token_usage_ratio",        // budget: used/max ratio gauge
	// Token-reconcile metrics (actual-vs-estimated layer)
	"orbit_token_actual_total",       // reconcile: actual tokens consumed {session_id}
	"orbit_token_estimation_error",   // reconcile: estimation error gauge (actual-estimated)
	// Reconcile auth metrics
	"orbit_reconcile_auth_rejected_total", // reconcile-auth: rejected requests {reason}
}

// ---------------------------------------------------------------------------
// PromQLViolation describes a single governance violation.
// ---------------------------------------------------------------------------

// PromQLViolation holds details about why a query was rejected.
type PromQLViolation struct {
	Query   string // the original query
	Reason  string // human-readable explanation
	Snippet string // the offending fragment
}

func (v PromQLViolation) Error() string {
	return fmt.Sprintf("promql-gov: REJECTED — %s (found %q in query %q)", v.Reason, v.Snippet, v.Query)
}

// ---------------------------------------------------------------------------
// ValidatePromQL — fail-closed query enforcement
// ---------------------------------------------------------------------------

// ValidatePromQL checks a PromQL expression against the orbit-engine
// governance policy. It returns nil if the query is safe, or a
// *PromQLViolation explaining why it was rejected.
//
// Policy (fail-closed):
//  1. Any occurrence of "orbit_skill_" → REJECT (raw metric, must use recording rule).
//  2. Empty query → REJECT (fail-closed: absence of intent is not safe).
//  3. All other queries → ALLOW (non-orbit metrics are outside our governance scope).
//
// This is intentionally a string-level check. A full PromQL parser would
// add a heavy dependency for minimal benefit — the forbidden prefix is
// unambiguous and cannot appear as a substring of a safe identifier.
func ValidatePromQL(query string) error {
	// Rule 0: empty/whitespace-only → fail-closed
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return &PromQLViolation{
			Query:   query,
			Reason:  "empty query is not allowed (fail-closed)",
			Snippet: "",
		}
	}

	// Rule 1: scan for forbidden raw metric prefix
	// We search case-sensitively — Prometheus metric names are case-sensitive.
	remaining := trimmed
	for {
		idx := strings.Index(remaining, _forbiddenPrefix)
		if idx < 0 {
			break
		}

		// Extract the full identifier starting at this match.
		end := idx
		for end < len(remaining) && isIdentChar(remaining[end]) {
			end++
		}
		ident := remaining[idx:end]

		// If the full identifier matches an explicitly allowed prefix, skip it.
		if isAllowedIdent(ident) {
			remaining = remaining[end:]
			continue
		}

		// Not in the allow-list — reject.
		snippet := ident
		return &PromQLViolation{
			Query:   query,
			Reason:  "raw metric prefix \"orbit_skill_\" is forbidden — use recording rules (orbit:*) instead",
			Snippet: snippet,
		}
	}

	// Rule 2: passes governance — query is allowed.
	return nil
}

// ---------------------------------------------------------------------------
// ValidatePromQLStrict — stricter mode: orbit-related queries MUST use
// allowed prefixes.
// ---------------------------------------------------------------------------

// ValidatePromQLStrict applies the same rules as ValidatePromQL, plus:
//   - If the query references any "orbit" identifier that is NOT in the
//     allowed list, it is rejected. This catches typos and future metrics
//     that haven't been added to governance yet.
//   - If the query uses any high-cardinality label name (client_id,
//     session_id, event_id, etc.) as a label selector, it is rejected.
//     This prevents unbounded series creation in Prometheus.
//
// Use this for CI/CD pipeline checks.
func ValidatePromQLStrict(query string) error {
	// First, apply base rules.
	if err := ValidatePromQL(query); err != nil {
		return err
	}

	trimmed := strings.TrimSpace(query)

	// Cardinality protection: reject queries using forbidden label names.
	for _, label := range HighCardinalityLabels() {
		// Look for patterns like: label_name= or label_name!= or label_name=~
		patterns := []string{
			label + "=",
			label + "!",
			label + "~",
		}
		for _, p := range patterns {
			if strings.Contains(trimmed, p) {
				return &PromQLViolation{
					Query:   query,
					Reason:  fmt.Sprintf("high-cardinality label %q is forbidden in queries — it would create unbounded series", label),
					Snippet: p,
				}
			}
		}
	}

	// Scan for any "orbit_" token that isn't in the allowed list.
	// We iterate through all occurrences of "orbit_" in the string.
	remaining := trimmed
	for {
		idx := strings.Index(remaining, "orbit_")
		if idx < 0 {
			break
		}

		// Extract the full identifier (word chars: a-z, A-Z, 0-9, _)
		start := idx
		end := idx
		for end < len(remaining) && isIdentChar(remaining[end]) {
			end++
		}
		ident := remaining[start:end]

		// Check against allowed prefixes
		if !isAllowedIdent(ident) {
			return &PromQLViolation{
				Query:   query,
				Reason:  fmt.Sprintf("metric %q is not in the governance allow-list — use recording rules (orbit:*)", ident),
				Snippet: ident,
			}
		}

		// Advance past this occurrence
		remaining = remaining[end:]
	}

	return nil
}

// isIdentChar returns true if c is a valid Prometheus metric name character.
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == ':'
}

// isAllowedIdent checks if an identifier matches any allowed prefix.
func isAllowedIdent(ident string) bool {
	for _, prefix := range _allowedPrefixes {
		if strings.HasPrefix(ident, prefix) {
			return true
		}
	}
	return false
}
