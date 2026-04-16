# orbit-engine v1.0 — Makefile
#
# Usage:
#   make test-go          — run all Go tests
#   make test-python      — run all Python tests
#   make validate-e2e     — run CLI validators (no external deps)
#   make validate-promql  — validate governance rules
#   make gate-v1          — ALL checks must pass before tagging v1.0
#   make tag-v1           — git tag v1.0.0 (only after gate-v1)
#
# The gate-v1 target is the release gate. If it fails, v1.0 is blocked.

.PHONY: test-go test-go-contract test-python validate-e2e validate-promql gate-v1 tag-v1 clean

# ── Go tests ──────────────────────────────────────────────────────────

test-go:
	@echo "══ Go tests (all) ══"
	cd tracking && go test ./... -v -count=1
	@echo "✅ Go tests passed"

test-go-contract:
	@echo "══ v1.0 contract test ══"
	cd tracking && go test -run "TestV1ContractComplete|TestV1GatewayMetricsContract" -v -count=1
	@echo "✅ v1.0 contract test passed"

# ── Python tests ──────────────────────────────────────────────────────

test-python:
	@echo "══ Python tests ══"
	cd tests && python3 run_tests.py
	@echo "✅ Python tests passed"

# ── E2E validators (in-process, no external services) ─────────────────

validate-e2e:
	@echo "══ E2E validate ══"
	cd tracking && go run ./cmd/validate
	@echo "✅ E2E validate passed"

validate-env:
	@echo "══ Environment safety validate ══"
	cd tracking && go run ./cmd/validate_env
	@echo "✅ Environment safety passed"

validate-gov:
	@echo "══ Governance validate ══"
	cd tracking && go run ./cmd/validate_gov
	@echo "✅ Governance validate passed"

# ── PromQL governance (quick check) ──────────────────────────────────

validate-promql:
	@echo "══ PromQL governance ══"
	@echo "-- Recording rules (must PASS) --"
	cd tracking && go run ./cmd/validate_promql "orbit:tokens_saved_total:prod"
	cd tracking && go run ./cmd/validate_promql "orbit:activations_total:prod"
	cd tracking && go run ./cmd/validate_promql "orbit:sessions_total:prod"
	cd tracking && go run ./cmd/validate_promql "orbit:event_staleness_seconds:prod"
	cd tracking && go run ./cmd/validate_promql "orbit_seed_mode"
	cd tracking && go run ./cmd/validate_promql "orbit_tracking_up"
	cd tracking && go run ./cmd/validate_promql "orbit_gateway_requests_total"
	@echo "-- Raw metrics (must FAIL) --"
	cd tracking && go run ./cmd/validate_promql "orbit_skill_tokens_saved_total" && exit 1 || true
	cd tracking && go run ./cmd/validate_promql "orbit_skill_activations_total" && exit 1 || true
	cd tracking && go run ./cmd/validate_promql --strict "orbit_unknown_metric" && exit 1 || true
	@echo "✅ PromQL governance passed"

# ── v1.0 RELEASE GATE ────────────────────────────────────────────────
# ALL targets below must pass. If ANY fails, the release is blocked.

gate-v1: test-go-contract test-go test-python validate-e2e validate-promql
	@echo ""
	@echo "════════════════════════════════════════════════════════════"
	@echo "  🟢  v1.0 RELEASE GATE PASSED"
	@echo ""
	@echo "  All checks:"
	@echo "    ✅ Go contract tests"
	@echo "    ✅ Go full test suite"
	@echo "    ✅ Python validation tests"
	@echo "    ✅ E2E in-process validator"
	@echo "    ✅ PromQL governance"
	@echo ""
	@echo "  Ready to tag: make tag-v1"
	@echo "════════════════════════════════════════════════════════════"

# ── Tag (only after gate passes) ─────────────────────────────────────

tag-v1:
	@echo "Checking gate status..."
	$(MAKE) gate-v1
	@echo ""
	@echo "Tagging v1.0.0..."
	git tag -a v1.0.0 -m "orbit-engine v1.0.0 — validated release"
	@echo "✅ Tagged v1.0.0. Push with: git push origin v1.0.0"

# ── Cleanup ──────────────────────────────────────────────────────────

clean:
	cd tracking && go clean -testcache

# ── SSH / Remote access ───────────────────────────────────────────────────────
#
# Required env vars (never commit values):
#   ORBIT_EC2_HOST    — EC2 public DNS or IP
#   ORBIT_EC2_USER    — non-root SSH user (e.g. ubuntu, ec2-user)
#   ORBIT_SSH_KEY     — path to .pem key file
#
# Optional:
#   ORBIT_SSH_PORT    — SSH port (default: 22)
#   ORBIT_PROJECT_DIR — remote project path (default: /opt/orbit-engine)

.PHONY: ssh-validate ssh-remote-validate ssh-config

ssh-validate:
	@echo "══ SSH connection + local key validation ══"
	@bash scripts/ssh_setup.sh
	@echo "✅ SSH validation passed"

ssh-remote-validate:
	@echo "══ Remote environment validation ══"
	@[ -n "$(ORBIT_EC2_HOST)" ]  || (echo "ERROR: ORBIT_EC2_HOST not set" && exit 1)
	@[ -n "$(ORBIT_EC2_USER)" ]  || (echo "ERROR: ORBIT_EC2_USER not set" && exit 1)
	@[ -n "$(ORBIT_SSH_KEY)" ]   || (echo "ERROR: ORBIT_SSH_KEY not set" && exit 1)
	@ssh \
		-i "$(ORBIT_SSH_KEY)" \
		-p "$${ORBIT_SSH_PORT:-22}" \
		-o IdentitiesOnly=yes \
		-o BatchMode=yes \
		-o ConnectTimeout=10 \
		-o StrictHostKeyChecking=accept-new \
		-o ForwardAgent=no \
		-o ForwardX11=no \
		"$(ORBIT_EC2_USER)@$(ORBIT_EC2_HOST)" \
		'bash -s' < scripts/ssh_remote_validate.sh
	@echo "✅ Remote environment validation passed"

ssh-config:
	@echo "══ Rendering SSH config (requires envsubst) ══"
	@[ -n "$(ORBIT_EC2_HOST)" ]  || (echo "ERROR: ORBIT_EC2_HOST not set" && exit 1)
	@[ -n "$(ORBIT_EC2_USER)" ]  || (echo "ERROR: ORBIT_EC2_USER not set" && exit 1)
	@[ -n "$(ORBIT_SSH_KEY)" ]   || (echo "ERROR: ORBIT_SSH_KEY not set" && exit 1)
	@mkdir -p ~/.ssh/config.d
	@envsubst < deploy/ssh_config.template > ~/.ssh/config.d/orbit-engine
	@chmod 600 ~/.ssh/config.d/orbit-engine
	@echo "✅ SSH config written to ~/.ssh/config.d/orbit-engine"
	@echo "   Add 'Include ~/.ssh/config.d/*' to the top of ~/.ssh/config if needed"
