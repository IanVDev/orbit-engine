// exec_gov.go — Execution Governance layer for orbit-engine.
//
// This layer sits BEFORE the model_control layer in the request pipeline.
// Its purpose is to enforce a mandatory validation gate on every execution
// attempt before any routing or model-selection logic is evaluated.
//
// # Design invariants
//
//   - Fail-closed: ValidateExecution returns a non-nil error whenever an
//     execution must be blocked. There is no path that allows execution on
//     a validation error.
//   - No bypass: TrackHandlerWithControl calls ValidateExecution before any
//     other check. A blocked verdict causes an immediate HTTP 403 with a
//     structured JSON body; the event is NEVER forwarded to TrackHandler.
//   - Every verdict (allowed AND blocked) is logged as JSONL and counted in
//     Prometheus so that governance gaps are observable.
//   - Critical paths and sensitive-skill rules are evaluated even when the
//     incoming request has no model_from/model_to fields — the governance
//     check is unconditional.
//
// # Pipeline order
//
//	HTTP request
//	     │
//	     ▼
//	[1] ExecGov.ValidateExecution   ← this file
//	     │ blocked → HTTP 403
//	     ▼
//	[2] ModelControl enforcement    ← model_control.go
//	     │ locked+override → HTTP 403
//	     ▼
//	[3] TrackHandler                ← tracking.go
//
// # Threat model
//
// The following classes of requests are unconditionally blocked:
//
//	A. Missing identity (empty skill_id or session_id).
//	B. Access to critical file system paths (credentials, kernel, secrets).
//	C. Sensitive skill execution without accompanying test coverage signal.
//	D. Path traversal attempts ("../").
//	E. Suspicious path patterns (environment files, private keys, certs).
package tracking

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// ExecutionIntent — what is about to be executed
// ---------------------------------------------------------------------------

// ExecutionIntent captures every attribute of an incoming execution request
// that is relevant for governance evaluation.  It is built from the HTTP
// request body before any routing logic runs.
//
// Zero-value fields are safe — missing optional fields never grant extra
// permissions; they are treated as "not provided" and checked accordingly.
type ExecutionIntent struct {
	// SkillID is the identifier of the skill being invoked. Required.
	SkillID string `json:"skill_id"`
	// SessionID is the caller's session. Required.
	SessionID string `json:"session_id"`
	// FilePaths lists every file the execution will read or write.
	// May be empty for skills that operate on in-memory data only.
	FilePaths []string `json:"file_paths,omitempty"`
	// SkillSensitive marks the skill as requiring test coverage before execution.
	// Must be paired with HasTests=true; otherwise execution is blocked.
	SkillSensitive bool `json:"skill_sensitive"`
	// HasTests signals that the caller has verified test coverage exists.
	// Effective only when SkillSensitive=true.
	HasTests bool `json:"has_tests"`
	// CallerID is an optional identity for the calling process or user.
	CallerID string `json:"caller_id,omitempty"`
	// RequestedAt is the ISO-8601/RFC3339 timestamp of the request.
	// Used only for logging; not validated (FlexTime handles that upstream).
	RequestedAt string `json:"requested_at,omitempty"`
}

// ---------------------------------------------------------------------------
// ExecutionVerdict — result of ValidateExecution
// ---------------------------------------------------------------------------

// ExecutionVerdict describes the governance outcome for a single intent.
// It is embedded in HTTP error responses and JSONL log lines.
type ExecutionVerdict struct {
	// Allowed is true only when every governance rule is satisfied.
	// Fail-closed: when in doubt, Allowed is false.
	Allowed bool `json:"allowed"`
	// Reason is a machine-readable string describing the outcome.
	// For allowed verdicts: "all_rules_passed".
	// For blocked verdicts: a concise description of the violated rule.
	Reason string `json:"reason"`
	// BlockRule names the specific rule that caused a block, empty when Allowed.
	// Format: "rule_name" or "rule_name:detail".
	BlockRule string `json:"block_rule,omitempty"`
	// SkillID echoes the intent field for correlation.
	SkillID string `json:"skill_id"`
	// SessionID echoes the intent field for correlation.
	SessionID string `json:"session_id"`
	// Timestamp is when the verdict was produced (UTC RFC3339Nano).
	Timestamp string `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Critical path catalogue
// ---------------------------------------------------------------------------

// criticalPathPatterns is the closed set of path substrings that are NEVER
// allowed in FilePaths.  Matching is case-insensitive and uses contains
// semantics after filepath.Clean normalisation.
//
// Adding a new pattern here automatically enforces it everywhere — this list
// is the single source of truth for the critical-path block rule.
var criticalPathPatterns = []struct {
	pattern string // lower-case substring to match
	name    string // label used in metrics and logs
}{
	// ── System files ─────────────────────────────────────────────
	{"/etc/passwd", "system_passwd"},
	{"/etc/shadow", "system_shadow"},
	{"/etc/sudoers", "system_sudoers"},
	{"/etc/hosts", "system_hosts"},
	{"/proc/", "proc_fs"},
	{"/sys/", "sys_fs"},
	{"/dev/", "dev_fs"},
	{"/root/", "root_home"},
	// ── Credentials & private keys ────────────────────────────────
	{".env", "dotenv"},
	{"id_rsa", "ssh_key_rsa"},
	{"id_ed25519", "ssh_key_ed25519"},
	{"id_ecdsa", "ssh_key_ecdsa"},
	{"id_dsa", "ssh_key_dsa"},
	{".pem", "cert_pem"},
	{".key", "cert_key"},
	{".crt", "cert_crt"},
	{".pfx", "cert_pfx"},
	{".p12", "cert_p12"},
	{".p8", "cert_p8"},
	// ── Path traversal ────────────────────────────────────────────
	{"../", "path_traversal"},
	// ── Cloud / container secrets ─────────────────────────────────
	{"/var/run/secrets/", "k8s_secrets"},
	{"kubeconfig", "kubeconfig"},
	{".kube/", "kube_dir"},
	{".aws/credentials", "aws_credentials"},
	// ── Database artefacts ────────────────────────────────────────
	{".sqlite", "db_sqlite"},
	// ── Authorisation stores ──────────────────────────────────────
	{".htpasswd", "htpasswd"},
	{"shadow", "shadow_file"},
}

// ---------------------------------------------------------------------------
// ValidateExecution — the governance gate
// ---------------------------------------------------------------------------

// ValidateExecution evaluates all governance rules against the supplied intent.
//
// Return contract (fail-closed):
//   - (verdict{Allowed:true},  nil)   → execution is permitted.
//   - (verdict{Allowed:false}, error) → execution MUST be blocked; error message
//     is safe to surface to the caller.
//
// There is intentionally no (verdict{Allowed:false}, nil) return path —
// a blocked verdict always carries an error so callers cannot silently ignore it.
//
// Rules evaluated in order (first match wins):
//
//	R0: skill_id is required.
//	R1: session_id is required.
//	R2: no critical file path may be accessed.
//	R3: a sensitive skill must have test coverage (HasTests=true).
func ValidateExecution(intent ExecutionIntent) (ExecutionVerdict, error) {
	start := time.Now()
	ts := start.UTC().Format(time.RFC3339Nano)

	block := func(reason, rule string) (ExecutionVerdict, error) {
		v := ExecutionVerdict{
			Allowed:   false,
			Reason:    reason,
			BlockRule: rule,
			SkillID:   intent.SkillID,
			SessionID: intent.SessionID,
			Timestamp: ts,
		}
		emitExecGovLog(intent, v)
		execGovBlockedTotal.WithLabelValues(rule).Inc()
		execGovValidationDuration.Observe(time.Since(start).Seconds())
		return v, fmt.Errorf("exec-gov: %s", reason)
	}

	// ── R0: required fields ───────────────────────────────────────────
	if strings.TrimSpace(intent.SkillID) == "" {
		return block(
			"skill_id is required — execution blocked (fail-closed)",
			"required_fields",
		)
	}
	if strings.TrimSpace(intent.SessionID) == "" {
		return block(
			"session_id is required — execution blocked (fail-closed)",
			"required_fields",
		)
	}

	// ── R1: critical file path access ─────────────────────────────────
	for _, fp := range intent.FilePaths {
		if fp == "" {
			continue
		}
		// Normalise: resolve relative and clean.
		cleaned := filepath.Clean(fp)
		lower := strings.ToLower(cleaned)

		for _, entry := range criticalPathPatterns {
			if strings.Contains(lower, entry.pattern) {
				return block(
					fmt.Sprintf("access to path %q is forbidden (matches critical rule %q)", fp, entry.name),
					"critical_path:"+entry.name,
				)
			}
		}
	}

	// ── R2: sensitive skill requires test coverage ─────────────────────
	if intent.SkillSensitive && !intent.HasTests {
		return block(
			fmt.Sprintf(
				"skill %q is marked sensitive but no test coverage was declared — execution blocked",
				intent.SkillID,
			),
			"sensitive_no_tests",
		)
	}

	// ── All rules passed ──────────────────────────────────────────────
	v := ExecutionVerdict{
		Allowed:   true,
		Reason:    "all_rules_passed",
		SkillID:   intent.SkillID,
		SessionID: intent.SessionID,
		Timestamp: ts,
	}
	emitExecGovLog(intent, v)
	execGovAllowedTotal.Inc()
	execGovValidationDuration.Observe(time.Since(start).Seconds())
	return v, nil
}

// ---------------------------------------------------------------------------
// IntentFromBody — extract ExecutionIntent from a raw JSON request body
// ---------------------------------------------------------------------------

// execGovBodyFields mirrors the fields extracted from the request body for
// exec-gov evaluation.  All fields are optional at the JSON level; missing
// required fields are caught by ValidateExecution (fail-closed).
type execGovBodyFields struct {
	SkillID        string   `json:"skill_id"`
	SessionID      string   `json:"session_id"`
	FilePaths      []string `json:"file_paths"`
	SkillSensitive bool     `json:"skill_sensitive"`
	HasTests       bool     `json:"has_tests"`
	CallerID       string   `json:"caller_id"`
	// EventType is also accepted as skill_id when skill_id is absent
	// (backward-compat with older clients that send event_type only).
	EventType string `json:"event_type"`
	Timestamp string `json:"timestamp"`
}

// IntentFromBody builds an ExecutionIntent from a raw JSON body.
// It never fails — missing or unparseable fields produce zero values, and
// ValidateExecution will reject those as required-field violations.
func IntentFromBody(body []byte) ExecutionIntent {
	var f execGovBodyFields
	_ = json.Unmarshal(body, &f)

	skillID := f.SkillID
	if skillID == "" {
		skillID = f.EventType // backward-compat
	}

	return ExecutionIntent{
		SkillID:        skillID,
		SessionID:      f.SessionID,
		FilePaths:      f.FilePaths,
		SkillSensitive: f.SkillSensitive,
		HasTests:       f.HasTests,
		CallerID:       f.CallerID,
		RequestedAt:    f.Timestamp,
	}
}

// ---------------------------------------------------------------------------
// JSONL logging
// ---------------------------------------------------------------------------

// emitExecGovLog writes a structured JSONL line for every verdict.
// The prefix [EXEC_GOV] is used by log shippers to route these entries.
func emitExecGovLog(intent ExecutionIntent, verdict ExecutionVerdict) {
	entry := map[string]interface{}{
		"event":      "exec_gov",
		"allowed":    verdict.Allowed,
		"reason":     verdict.Reason,
		"block_rule": verdict.BlockRule,
		"skill_id":   intent.SkillID,
		"session_id": intent.SessionID,
		"sensitive":  intent.SkillSensitive,
		"has_tests":  intent.HasTests,
		"timestamp":  verdict.Timestamp,
	}
	if len(intent.FilePaths) > 0 {
		entry["file_paths"] = intent.FilePaths
	}
	if intent.CallerID != "" {
		entry["caller_id"] = intent.CallerID
	}

	b, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[EXEC_GOV][ERROR] failed to marshal verdict log: %v", err)
		return
	}
	log.Printf("[EXEC_GOV] %s", b)
}

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var (
	// orbit_exec_gov_blocked_total{block_rule} — one increment per blocked verdict.
	// Label block_rule uses the compact rule name (e.g. "critical_path:dotenv",
	// "sensitive_no_tests", "required_fields").
	execGovBlockedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "orbit_exec_gov_blocked_total",
			Help: "Total execution attempts blocked by the governance layer. Label: block_rule.",
		},
		[]string{"block_rule"},
	)

	// orbit_exec_gov_allowed_total — one increment per allowed verdict.
	// No skill_id label to avoid high cardinality.
	execGovAllowedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_exec_gov_allowed_total",
			Help: "Total execution attempts allowed by the governance layer.",
		},
	)

	// orbit_exec_gov_validation_duration_seconds — latency of ValidateExecution.
	// Used to detect governance layer overhead and pathological path lists.
	execGovValidationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "orbit_exec_gov_validation_duration_seconds",
			Help:    "Duration of ValidateExecution calls in seconds.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
		},
	)
)

// execGovMetricsOnce protects RegisterExecGovMetrics from double-registration.
var execGovMetricsOnce sync.Once

// RegisterExecGovMetrics registers all exec-gov Prometheus collectors.
// Call once at process startup, passing prometheus.DefaultRegisterer for
// production or an isolated registry for tests.
func RegisterExecGovMetrics(reg prometheus.Registerer) {
	execGovMetricsOnce.Do(func() {
		reg.MustRegister(
			execGovBlockedTotal,
			execGovAllowedTotal,
			execGovValidationDuration,
		)
	})
}

// ---------------------------------------------------------------------------
// Exported helper for governance queries
// ---------------------------------------------------------------------------

// ExecGovAllowedMetricName is the canonical name of the allowed counter.
// Exported so governance tests can reference it without string literals.
const ExecGovAllowedMetricName = "orbit_exec_gov_allowed_total"

// ExecGovBlockedMetricName is the canonical name of the blocked counter.
const ExecGovBlockedMetricName = "orbit_exec_gov_blocked_total"

// ExecGovDurationMetricName is the canonical name of the duration histogram.
const ExecGovDurationMetricName = "orbit_exec_gov_validation_duration_seconds"
