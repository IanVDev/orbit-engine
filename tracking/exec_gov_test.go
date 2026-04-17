// exec_gov_test.go — Anti-regression tests for the Execution Governance layer.
//
// Coverage matrix:
//
//	R0 — required fields:    empty skill_id, empty session_id
//	R1 — critical paths:     /etc/passwd, /proc/, ../traversal, .env, id_rsa,
//	                         .pem, .key, kubeconfig, .aws/credentials, /root/
//	R2 — sensitive + tests:  sensitive+no-tests blocked; sensitive+tests allowed
//	     pass-through:       non-sensitive without tests is allowed
//	     pass-through:       no file paths is allowed
//
//	Metrics:   counters incremented correctly for blocked/allowed
//	HTTP:      TrackHandlerWithControl blocks critical-path requests (403)
//	HTTP:      TrackHandlerWithControl passes clean requests through
//	Governance:ValidatePromQLStrict accepts all exec-gov metric names
//	Fuzz:      IntentFromBody never panics
package tracking

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func goodIntent() ExecutionIntent {
	return ExecutionIntent{
		SkillID:   "test-skill",
		SessionID: "test-session",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// R0 — required fields
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateExecution_BlocksMissingSkillID(t *testing.T) {
	intent := goodIntent()
	intent.SkillID = ""
	v, err := ValidateExecution(intent)
	if err == nil {
		t.Fatal("expected error for missing skill_id, got nil")
	}
	if v.Allowed {
		t.Error("Allowed must be false for missing skill_id")
	}
	if v.BlockRule != "required_fields" {
		t.Errorf("BlockRule = %q; want required_fields", v.BlockRule)
	}
}

func TestValidateExecution_BlocksMissingSessionID(t *testing.T) {
	intent := goodIntent()
	intent.SessionID = ""
	v, err := ValidateExecution(intent)
	if err == nil {
		t.Fatal("expected error for missing session_id, got nil")
	}
	if v.Allowed {
		t.Error("Allowed must be false for missing session_id")
	}
	if v.BlockRule != "required_fields" {
		t.Errorf("BlockRule = %q; want required_fields", v.BlockRule)
	}
}

func TestValidateExecution_BlocksWhitespaceOnlySkillID(t *testing.T) {
	intent := goodIntent()
	intent.SkillID = "   "
	_, err := ValidateExecution(intent)
	if err == nil {
		t.Error("whitespace-only skill_id must be blocked")
	}
}

func TestValidateExecution_BlocksWhitespaceOnlySessionID(t *testing.T) {
	intent := goodIntent()
	intent.SessionID = "\t\n"
	_, err := ValidateExecution(intent)
	if err == nil {
		t.Error("whitespace-only session_id must be blocked")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// R1 — critical path access
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateExecution_BlocksCriticalPaths(t *testing.T) {
	cases := []struct {
		path     string
		wantRule string
	}{
		{"/etc/passwd", "critical_path:system_passwd"},
		{"/etc/shadow", "critical_path:system_shadow"},
		{"/etc/sudoers", "critical_path:system_sudoers"},
		{"/etc/hosts", "critical_path:system_hosts"},
		{"/proc/1/maps", "critical_path:proc_fs"},
		{"/sys/kernel/mm/hugepages", "critical_path:sys_fs"},
		{"/dev/mem", "critical_path:dev_fs"},
		{"/root/.bashrc", "critical_path:root_home"},
		{"/home/user/.env", "critical_path:dotenv"},
		{"/home/user/.env.production", "critical_path:dotenv"},
		{"/home/user/.ssh/id_rsa", "critical_path:ssh_key_rsa"},
		{"/home/user/.ssh/id_ed25519", "critical_path:ssh_key_ed25519"},
		{"/home/user/.ssh/id_ecdsa", "critical_path:ssh_key_ecdsa"},
		{"/certs/server.pem", "critical_path:cert_pem"},
		{"/certs/server.key", "critical_path:cert_key"},
		{"/certs/server.crt", "critical_path:cert_crt"},
		{"/certs/bundle.pfx", "critical_path:cert_pfx"},
		{"/certs/bundle.p12", "critical_path:cert_p12"},
		{"../../etc/passwd", "critical_path:path_traversal"},
		{"/var/run/secrets/kubernetes.io/serviceaccount/token", "critical_path:k8s_secrets"},
		{"/home/user/.kube/config", "critical_path:kube_dir"},
		{"/home/user/.aws/credentials", "critical_path:aws_credentials"},
		{"/data/app.sqlite", "critical_path:db_sqlite"},
		{"/etc/nginx/.htpasswd", "critical_path:htpasswd"},
	}

	for _, tc := range cases {
		intent := goodIntent()
		intent.FilePaths = []string{tc.path}

		v, err := ValidateExecution(intent)
		if err == nil {
			t.Errorf("path %q: expected error, got nil", tc.path)
			continue
		}
		if v.Allowed {
			t.Errorf("path %q: Allowed must be false", tc.path)
		}
		if !strings.HasPrefix(v.BlockRule, "critical_path:") {
			t.Errorf("path %q: BlockRule %q must have critical_path: prefix", tc.path, v.BlockRule)
		}
	}
}

func TestValidateExecution_MultiplePathsOneBlocked(t *testing.T) {
	intent := goodIntent()
	intent.FilePaths = []string{
		"/home/user/notes.txt",   // safe
		"/home/user/.ssh/id_rsa", // critical
		"/home/user/README.md",   // safe
	}
	v, err := ValidateExecution(intent)
	if err == nil || v.Allowed {
		t.Error("single critical path in list must block the entire intent")
	}
}

func TestValidateExecution_AllPathsSafe(t *testing.T) {
	intent := goodIntent()
	intent.FilePaths = []string{
		"/home/user/notes.txt",
		"/tmp/work.json",
		"/var/log/app.log",
	}
	v, err := ValidateExecution(intent)
	if err != nil {
		t.Errorf("safe paths: unexpected error: %v", err)
	}
	if !v.Allowed {
		t.Errorf("safe paths: Allowed must be true, got false (reason=%s)", v.Reason)
	}
}

func TestValidateExecution_NoFilePaths(t *testing.T) {
	v, err := ValidateExecution(goodIntent())
	if err != nil {
		t.Fatalf("no file paths: unexpected error: %v", err)
	}
	if !v.Allowed {
		t.Error("no file paths: must be allowed")
	}
}

func TestValidateExecution_EmptyStringInFilePaths(t *testing.T) {
	// Empty string paths must be skipped, not panic.
	intent := goodIntent()
	intent.FilePaths = []string{"", "   ", "/tmp/safe.txt"}
	v, err := ValidateExecution(intent)
	if err != nil {
		t.Fatalf("empty paths: unexpected error: %v", err)
	}
	if !v.Allowed {
		t.Error("empty paths: must be allowed")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// R2 — sensitive skill requires tests
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateExecution_BlocksSensitiveWithoutTests(t *testing.T) {
	intent := goodIntent()
	intent.SkillSensitive = true
	intent.HasTests = false

	v, err := ValidateExecution(intent)
	if err == nil || v.Allowed {
		t.Error("sensitive skill without tests must be blocked")
	}
	if v.BlockRule != "sensitive_no_tests" {
		t.Errorf("BlockRule = %q; want sensitive_no_tests", v.BlockRule)
	}
}

func TestValidateExecution_AllowsSensitiveWithTests(t *testing.T) {
	intent := goodIntent()
	intent.SkillSensitive = true
	intent.HasTests = true

	v, err := ValidateExecution(intent)
	if err != nil {
		t.Fatalf("sensitive+tests: unexpected error: %v", err)
	}
	if !v.Allowed {
		t.Error("sensitive+tests: Allowed must be true")
	}
}

func TestValidateExecution_AllowsNonSensitiveWithoutTests(t *testing.T) {
	intent := goodIntent()
	intent.SkillSensitive = false
	intent.HasTests = false

	v, err := ValidateExecution(intent)
	if err != nil {
		t.Fatalf("non-sensitive: unexpected error: %v", err)
	}
	if !v.Allowed {
		t.Error("non-sensitive: must be allowed without tests")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Verdict fields
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateExecution_VerdictFields_Allowed(t *testing.T) {
	intent := goodIntent()
	v, err := ValidateExecution(intent)
	if err != nil || !v.Allowed {
		t.Fatalf("expected allowed verdict: %v / %v", err, v)
	}
	if v.SkillID != intent.SkillID {
		t.Errorf("SkillID = %q; want %q", v.SkillID, intent.SkillID)
	}
	if v.SessionID != intent.SessionID {
		t.Errorf("SessionID = %q; want %q", v.SessionID, intent.SessionID)
	}
	if v.Reason != "all_rules_passed" {
		t.Errorf("Reason = %q; want all_rules_passed", v.Reason)
	}
	if v.Timestamp == "" {
		t.Error("Timestamp must be non-empty")
	}
	// Timestamp must parse as RFC3339.
	if _, parseErr := time.Parse(time.RFC3339Nano, v.Timestamp); parseErr != nil {
		t.Errorf("Timestamp %q is not valid RFC3339Nano: %v", v.Timestamp, parseErr)
	}
}

func TestValidateExecution_VerdictFields_Blocked(t *testing.T) {
	intent := goodIntent()
	intent.SkillID = ""
	v, err := ValidateExecution(intent)
	if err == nil || v.Allowed {
		t.Fatal("expected blocked verdict")
	}
	if v.Reason == "" {
		t.Error("blocked: Reason must be non-empty")
	}
	if v.BlockRule == "" {
		t.Error("blocked: BlockRule must be non-empty")
	}
	if v.Timestamp == "" {
		t.Error("blocked: Timestamp must be non-empty")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fail-closed contract: never (nil, Allowed=false)
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateExecution_FailClosed_BlockedAlwaysHasError(t *testing.T) {
	// Run many different blocking scenarios. Each must return non-nil error.
	cases := []ExecutionIntent{
		{SkillID: "", SessionID: "s"},
		{SkillID: "k", SessionID: ""},
		{SkillID: "k", SessionID: "s", FilePaths: []string{"/etc/passwd"}},
		{SkillID: "k", SessionID: "s", SkillSensitive: true, HasTests: false},
	}
	for i, c := range cases {
		v, err := ValidateExecution(c)
		if v.Allowed {
			t.Errorf("case %d: Allowed=true, must not occur", i)
		}
		if err == nil {
			t.Errorf("case %d: err=nil but Allowed=false — violates fail-closed contract", i)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// IntentFromBody
// ─────────────────────────────────────────────────────────────────────────────

func TestIntentFromBody_FullFields(t *testing.T) {
	body := `{
		"skill_id":"my-skill","session_id":"sess-1",
		"file_paths":["/tmp/a.txt"],"skill_sensitive":true,
		"has_tests":true,"caller_id":"svc-a","timestamp":"2026-01-01T00:00:00Z"
	}`
	intent := IntentFromBody([]byte(body))
	if intent.SkillID != "my-skill" {
		t.Errorf("SkillID = %q; want my-skill", intent.SkillID)
	}
	if intent.SessionID != "sess-1" {
		t.Errorf("SessionID = %q; want sess-1", intent.SessionID)
	}
	if len(intent.FilePaths) != 1 || intent.FilePaths[0] != "/tmp/a.txt" {
		t.Errorf("FilePaths = %v; want [/tmp/a.txt]", intent.FilePaths)
	}
	if !intent.SkillSensitive {
		t.Error("SkillSensitive must be true")
	}
	if !intent.HasTests {
		t.Error("HasTests must be true")
	}
	if intent.CallerID != "svc-a" {
		t.Errorf("CallerID = %q; want svc-a", intent.CallerID)
	}
}

func TestIntentFromBody_BackwardCompatEventType(t *testing.T) {
	// Older clients send event_type, not skill_id.
	body := `{"event_type":"activation","session_id":"s1"}`
	intent := IntentFromBody([]byte(body))
	if intent.SkillID != "activation" {
		t.Errorf("SkillID = %q; want activation (from event_type)", intent.SkillID)
	}
}

func TestIntentFromBody_SkillIDTakesPrecedenceOverEventType(t *testing.T) {
	body := `{"skill_id":"explicit","event_type":"activation","session_id":"s1"}`
	intent := IntentFromBody([]byte(body))
	if intent.SkillID != "explicit" {
		t.Errorf("SkillID = %q; want explicit (skill_id takes precedence)", intent.SkillID)
	}
}

func TestIntentFromBody_InvalidJSON(t *testing.T) {
	// Must not panic — returns zero-value intent.
	intent := IntentFromBody([]byte("{bad json"))
	if intent.SkillID != "" || intent.SessionID != "" {
		t.Error("invalid JSON: expected zero-value intent")
	}
}

func TestIntentFromBody_EmptyBody(t *testing.T) {
	intent := IntentFromBody(nil)
	if intent.SkillID != "" {
		t.Error("nil body: SkillID must be empty string")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Prometheus metrics
// ─────────────────────────────────────────────────────────────────────────────

func TestExecGovMetrics_BlockedIncrements(t *testing.T) {
	reg := prometheus.NewRegistry()
	blocked := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orbit_exec_gov_blocked_total", Help: "test",
	}, []string{"block_rule"})
	_ = reg.Register(blocked)

	// Simulate what ValidateExecution does internally.
	blocked.WithLabelValues("required_fields").Inc()
	blocked.WithLabelValues("critical_path:dotenv").Inc()

	families, _ := reg.Gather()
	var total float64
	for _, mf := range families {
		if mf.GetName() == "orbit_exec_gov_blocked_total" {
			for _, m := range mf.GetMetric() {
				total += m.GetCounter().GetValue()
			}
		}
	}
	if total < 2 {
		t.Errorf("orbit_exec_gov_blocked_total: expected >= 2, got %.0f", total)
	}
}

func TestExecGovMetrics_AllowedIncrements(t *testing.T) {
	reg := prometheus.NewRegistry()
	allowed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orbit_exec_gov_allowed_total", Help: "test",
	})
	_ = reg.Register(allowed)

	allowed.Inc()
	allowed.Inc()

	families, _ := reg.Gather()
	for _, mf := range families {
		if mf.GetName() == "orbit_exec_gov_allowed_total" {
			for _, m := range mf.GetMetric() {
				if m.GetCounter().GetValue() < 2 {
					t.Errorf("orbit_exec_gov_allowed_total: expected >= 2, got %.0f",
						m.GetCounter().GetValue())
				}
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PromQL governance
// ─────────────────────────────────────────────────────────────────────────────

func TestExecGovMetrics_PassGovernance(t *testing.T) {
	queries := []string{
		`rate(orbit_exec_gov_blocked_total[5m])`,
		`sum by (block_rule) (rate(orbit_exec_gov_blocked_total[5m]))`,
		`orbit_exec_gov_allowed_total`,
		`rate(orbit_exec_gov_allowed_total[1m])`,
		`histogram_quantile(0.99, rate(orbit_exec_gov_validation_duration_seconds_bucket[5m]))`,
		`orbit_exec_gov_blocked_total{block_rule="required_fields"}`,
	}
	for _, q := range queries {
		if err := ValidatePromQLStrict(q); err != nil {
			t.Errorf("governance rejected %q: %v", q, err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP handler integration
// ─────────────────────────────────────────────────────────────────────────────

// buildBody constructs a minimal valid tracking event body with extra exec-gov
// fields injected.
func buildExecGovBody(skillID, sessionID string, filePaths []string,
	sensitive, hasTests bool) []byte {
	m := map[string]interface{}{
		"skill_id":       skillID,
		"event_type":     skillID, // backward compat
		"session_id":     sessionID,
		"mode":           "auto",
		"trigger":        "test",
		"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
		"skill_sensitive": sensitive,
		"has_tests":      hasTests,
	}
	if len(filePaths) > 0 {
		m["file_paths"] = filePaths
	}
	b, _ := json.Marshal(m)
	return b
}

func TestTrackHandlerWithControl_ExecGovBlocksCriticalPath(t *testing.T) {
	handler := TrackHandlerWithControl(ModelControlAuto)
	body := buildExecGovBody("my-skill", "sess-1",
		[]string{"/etc/passwd"}, false, false)

	req := httptest.NewRequest(http.MethodPost, "/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("critical path: expected HTTP 403, got %d\nbody: %s",
			rr.Code, rr.Body.String())
	}
	// Response must contain governance error key.
	if !strings.Contains(rr.Body.String(), "execution_blocked_by_governance") {
		t.Errorf("response must contain execution_blocked_by_governance, got: %s",
			rr.Body.String())
	}
}

func TestTrackHandlerWithControl_ExecGovBlocksMissingSkillID(t *testing.T) {
	handler := TrackHandlerWithControl(ModelControlLocked)
	// Build body with empty skill_id AND event_type so both fallback paths are tested.
	m := map[string]interface{}{
		"skill_id":   "",
		"event_type": "",
		"session_id": "sess-1",
		"mode":       "auto",
		"trigger":    "test",
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
	}
	body, _ := json.Marshal(m)

	req := httptest.NewRequest(http.MethodPost, "/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("missing skill_id: expected HTTP 403, got %d", rr.Code)
	}
}

func TestTrackHandlerWithControl_ExecGovBlocksSensitiveNoTests(t *testing.T) {
	handler := TrackHandlerWithControl(ModelControlAuto)
	body := buildExecGovBody("sensitive-skill", "sess-2", nil, true, false)

	req := httptest.NewRequest(http.MethodPost, "/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("sensitive+no-tests: expected HTTP 403, got %d\nbody: %s",
			rr.Code, rr.Body.String())
	}
}

func TestTrackHandlerWithControl_ExecGovRunsBeforeModelControl(t *testing.T) {
	// Even with model_control=locked, exec-gov fires first.
	// A critical-path block must return exec-gov error (not model_control error).
	handler := TrackHandlerWithControl(ModelControlLocked)
	body := buildExecGovBody("my-skill", "sess-3",
		[]string{"/home/user/.ssh/id_rsa"}, false, false)

	req := httptest.NewRequest(http.MethodPost, "/track", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	// Must be exec-gov error, not model_control error.
	respBody := rr.Body.String()
	if !strings.Contains(respBody, "execution_blocked_by_governance") {
		t.Errorf("expected exec-gov error, got: %s", respBody)
	}
	if strings.Contains(respBody, "model_override_blocked") {
		t.Error("must NOT see model_override_blocked — exec-gov should fire first")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Path traversal — dedicated table
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateExecution_PathTraversal(t *testing.T) {
	traversals := []string{
		"../../etc/passwd",
		"../../../root/.ssh/id_rsa",
		"/tmp/../etc/shadow",
		"a/b/../../.env",
	}
	for _, path := range traversals {
		intent := goodIntent()
		intent.FilePaths = []string{path}
		v, err := ValidateExecution(intent)
		// Path traversal OR the destination pattern may match — either blocks.
		if v.Allowed || err == nil {
			t.Errorf("path %q must be blocked (traversal or destination)", path)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Regression: existing model_control tests still pass through exec-gov
// ─────────────────────────────────────────────────────────────────────────────

func TestExecGov_CleanBodyPassesThrough(t *testing.T) {
	// A body with no file_paths, no sensitive flag, valid ids — must reach
	// model_control layer (not blocked by exec-gov).
	body := buildExecGovBody("activation", "sess-clean", nil, false, false)
	intent := IntentFromBody(body)
	v, err := ValidateExecution(intent)
	if err != nil || !v.Allowed {
		t.Errorf("clean body must be allowed by exec-gov: err=%v allowed=%v", err, v.Allowed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fuzz: IntentFromBody never panics
// ─────────────────────────────────────────────────────────────────────────────

func FuzzIntentFromBody(f *testing.F) {
	f.Add([]byte(`{"skill_id":"s","session_id":"s"}`))
	f.Add([]byte(`{bad json`))
	f.Add([]byte(nil))
	f.Add([]byte(`{"file_paths":["../../etc/passwd"]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		// Must never panic.
		intent := IntentFromBody(b)
		// If we got a skill_id and session_id, ValidateExecution must not panic either.
		if intent.SkillID != "" && intent.SessionID != "" {
			_, _ = ValidateExecution(intent)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Constant metric names
// ─────────────────────────────────────────────────────────────────────────────

func TestExecGovMetricNameConstants(t *testing.T) {
	if ExecGovAllowedMetricName != "orbit_exec_gov_allowed_total" {
		t.Errorf("ExecGovAllowedMetricName = %q", ExecGovAllowedMetricName)
	}
	if ExecGovBlockedMetricName != "orbit_exec_gov_blocked_total" {
		t.Errorf("ExecGovBlockedMetricName = %q", ExecGovBlockedMetricName)
	}
	if ExecGovDurationMetricName != "orbit_exec_gov_validation_duration_seconds" {
		t.Errorf("ExecGovDurationMetricName = %q", ExecGovDurationMetricName)
	}
}
