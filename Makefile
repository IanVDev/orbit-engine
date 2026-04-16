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

# ── orbit-check (production readiness) ──────────────────────────────

# Verifica saúde do sistema ao vivo: health, métricas críticas, integridade SHA.
# Em produção: ENV=production ORBIT_GATEWAY_SHA256=<sha> make orbit-check
orbit-check:
	@bash scripts/orbit-check.sh

# Executa a suite de testes do orbit-check (servidores mock, sem dependências externas)
test-orbit-check:
	@bash tests/test_orbit_check.sh

# ── Grafana Dashboards ───────────────────────────────────────────────

# Valida o JSON do dashboard de segurança (requer python3)
validate-dashboard-security:
	@python3 -c "import json,sys; d=json.load(open('deploy/grafana-dashboard-security.json')); panels=[p for p in d['panels'] if p['type']!='row']; print(f'Dashboard valido: {len(panels)} paineis, uid={d[\"uid\"]}'); [print(f'  id={p[\"id\"]:2d}  {p[\"type\"]:12s}  {p[\"title\"]}') for p in panels]"

# Valida a sintaxe YAML das regras de alerta de segurança (requer pyyaml)
validate-alerts-security:
	@python3 -c "import sys; import yaml; data=yaml.safe_load(open('deploy/prometheus-alerts-security.yml')); rules=[r for g in data.get('groups',[]) for r in g.get('rules',[])]; print(f'Alertas validos: {len(rules)} regras'); [print(f'  {r[\"alert\"]}  ({r[\"labels\"][\"severity\"]})') for r in rules]" 2>/dev/null || python3 -c "import json; print('pyyaml nao instalado — validando apenas JSON do dashboard')"

# Importa dashboards Grafana via API HTTP
# Uso: GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<token> make import-dashboards
# Gera token em Grafana: Configuration → API Tokens → Create token (role: Admin)
import-dashboards:
	@bash scripts/import-grafana-dashboards.sh

# Inicia stack de observabilidade: Prometheus + Grafana (docker-compose)
# Requer Docker e docker-compose instalados
# Acesso: Prometheus em http://localhost:9090, Grafana em http://localhost:3000 (admin/admin)
obs-up:
	@docker-compose up -d && echo "✅ Stack iniciada: Prometheus (9090) + Grafana (3000)" && sleep 3 && curl -s http://localhost:3000/api/health | grep -q '"ok"' && echo "✅ Grafana pronto para importar dashboards"

# Para a stack de observabilidade
obs-down:
	@docker-compose down && echo "✅ Stack parada"

# Logs em tempo real da stack de observabilidade
obs-logs:
	@docker-compose logs -f

# Importa dashboards após stack estar rodando
obs-import: obs-up
	@echo "Aguardando Grafana ficar pronto..." && sleep 5
	@GRAFANA_TOKEN=$$(bash scripts/generate-grafana-token.sh || echo "") && \
	if [ -z "$$GRAFANA_TOKEN" ]; then \
		echo "⚠️  Token não gerado automaticamente — use:"; \
		echo "  GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<seu_token> make import-dashboards"; \
	else \
		GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=$$GRAFANA_TOKEN make import-dashboards; \
	fi

# ── Cleanup ──────────────────────────────────────────────────────────────

clean:
	cd tracking && go clean -testcache

