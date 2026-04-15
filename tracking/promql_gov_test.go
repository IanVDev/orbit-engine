package tracking

import (
	"strings"
	"testing"
)

// -----------------------------------------------------------------------
// TestValidatePromQL — base governance: reject orbit_skill_*, allow orbit:
// -----------------------------------------------------------------------

func TestValidatePromQL(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr bool
		errSnip string // substring expected in error message
	}{
		// ── MUST REJECT ─────────────────────────────────────────────
		{
			name:    "raw tokens_saved_total is forbidden",
			query:   `orbit_skill_tokens_saved_total`,
			wantErr: true,
			errSnip: "orbit_skill_",
		},
		{
			name:    "raw metric with env filter still forbidden",
			query:   `orbit_skill_tokens_saved_total{env="prod"}`,
			wantErr: true,
			errSnip: "orbit_skill_",
		},
		{
			name:    "raw metric in complex expression",
			query:   `sum(rate(orbit_skill_activations_total[5m])) by (mode)`,
			wantErr: true,
			errSnip: "orbit_skill_",
		},
		{
			name:    "raw metric in binary operation",
			query:   `orbit_skill_waste_estimated / orbit_skill_tokens_saved_total`,
			wantErr: true,
			errSnip: "orbit_skill_",
		},
		{
			name:    "empty query is rejected (fail-closed)",
			query:   ``,
			wantErr: true,
			errSnip: "empty query",
		},
		{
			name:    "whitespace-only query is rejected",
			query:   `   `,
			wantErr: true,
			errSnip: "empty query",
		},

		// ── MUST ALLOW ──────────────────────────────────────────────
		{
			name:    "recording rule tokens_saved prod",
			query:   `orbit:tokens_saved_total:prod`,
			wantErr: false,
		},
		{
			name:    "recording rule activations prod",
			query:   `orbit:activations_total:prod`,
			wantErr: false,
		},
		{
			name:    "recording rule with function",
			query:   `rate(orbit:tokens_saved_total:prod[5m])`,
			wantErr: false,
		},
		{
			name:    "recording rule in expression",
			query:   `orbit:waste_estimated:prod / orbit:tokens_saved_total:prod`,
			wantErr: false,
		},
		{
			name:    "freshness query",
			query:   `orbit:event_staleness_seconds:prod > 300`,
			wantErr: false,
		},
		{
			name:    "seed contamination alert",
			query:   `orbit:seed_contamination == 1`,
			wantErr: false,
		},
		{
			name:    "governance gauge seed_mode",
			query:   `orbit_seed_mode{env="prod"}`,
			wantErr: false,
		},
		{
			name:    "governance gauge tracking_up",
			query:   `orbit_tracking_up`,
			wantErr: false,
		},
		{
			name:    "instance_id is allowed",
			query:   `orbit_instance_id`,
			wantErr: false,
		},
		{
			name:    "last_event_timestamp is allowed",
			query:   `time() - orbit_last_event_timestamp`,
			wantErr: false,
		},
		{
			name:    "non-orbit metric passes (out of scope)",
			query:   `up{job="orbit-engine-tracking"}`,
			wantErr: false,
		},
		{
			name:    "pure math expression passes",
			query:   `1 + 1`,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePromQL(tc.query)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for query %q, got nil", tc.query)
				}
				if tc.errSnip != "" && !strings.Contains(err.Error(), tc.errSnip) {
					t.Fatalf("error %q should contain %q", err.Error(), tc.errSnip)
				}
				// Verify it's a *PromQLViolation
				if _, ok := err.(*PromQLViolation); !ok {
					t.Fatalf("expected *PromQLViolation, got %T", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for query %q: %v", tc.query, err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestValidatePromQLStrict — stricter mode catches unknown orbit_ metrics
// -----------------------------------------------------------------------

func TestValidatePromQLStrict(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr bool
		errSnip string
	}{
		// ── MUST REJECT (strict) ────────────────────────────────────
		{
			name:    "raw metric rejected same as base",
			query:   `orbit_skill_tokens_saved_total`,
			wantErr: true,
			errSnip: "orbit_skill_",
		},
		{
			name:    "unknown orbit_ metric rejected",
			query:   `orbit_unknown_future_metric`,
			wantErr: true,
			errSnip: "allow-list",
		},
		{
			name:    "typo in metric name caught",
			query:   `orbit_seeed_mode`,
			wantErr: true,
			errSnip: "allow-list",
		},

		// ── MUST ALLOW (strict) ─────────────────────────────────────
		{
			name:    "recording rule still passes",
			query:   `orbit:tokens_saved_total:prod`,
			wantErr: false,
		},
		{
			name:    "governance gauge passes strict",
			query:   `orbit_seed_mode`,
			wantErr: false,
		},
		{
			name:    "tracking_up passes strict",
			query:   `orbit_tracking_up`,
			wantErr: false,
		},
		{
			name:    "instance_id passes strict",
			query:   `orbit_instance_id{instance_id="abc"}`,
			wantErr: false,
		},
		{
			name:    "last_event_timestamp passes strict",
			query:   `orbit_last_event_timestamp > 0`,
			wantErr: false,
		},
		{
			name:    "non-orbit metric passes strict",
			query:   `node_cpu_seconds_total`,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePromQLStrict(tc.query)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for query %q, got nil", tc.query)
				}
				if tc.errSnip != "" && !strings.Contains(err.Error(), tc.errSnip) {
					t.Fatalf("error %q should contain %q", err.Error(), tc.errSnip)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for query %q: %v", tc.query, err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestPromQLViolationError — the Error() method formats correctly
// -----------------------------------------------------------------------

func TestPromQLViolationError(t *testing.T) {
	v := &PromQLViolation{
		Query:   "orbit_skill_foo",
		Reason:  "test reason",
		Snippet: "orbit_skill_foo",
	}
	msg := v.Error()
	if !strings.Contains(msg, "REJECTED") {
		t.Fatalf("error should contain REJECTED: %s", msg)
	}
	if !strings.Contains(msg, "test reason") {
		t.Fatalf("error should contain reason: %s", msg)
	}
	if !strings.Contains(msg, "orbit_skill_foo") {
		t.Fatalf("error should contain snippet: %s", msg)
	}
}
