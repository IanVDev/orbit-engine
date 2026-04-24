# orbit-engine — Makefile
#
# Usage:
#   make build     — compila e instala orbit em ~/.orbit/bin/orbit
#   make install   — instala em /usr/local/bin/orbit (pode exigir sudo)
#   make test-go   — roda todos os testes Go
#   make gate-cli  — gate de release offline (< 120s, fail-closed)

.PHONY: build install test-go gate-cli release-gate tag-release publish-skill clean

# ── Build + install local ────────────────────────────────────────────────────
# scripts/install.sh faz: go build com -ldflags (commit/version) →
# instala em ~/.orbit/bin/orbit → smoke-test (orbit version) → PATH hint.
build:
	@bash scripts/install.sh

# Instala em /usr/local/bin/orbit (caminho global).
install:
	@bash scripts/install.sh --prefix /usr/local/bin

# ── Go tests ─────────────────────────────────────────────────────────────────
test-go:
	@echo "══ Go tests ══"
	cd tracking && go test ./... -count=1
	@echo "✅ Go tests passed"

# ── CLI release gate ─────────────────────────────────────────────────────────
#
# Gate offline para tag de release da CLI. Roda em < 120s sem rede.
# Saída: gate_report.json com {gate, status, duration_ms, tail} por gate.
gate-cli:
	@bash scripts/gate_cli.sh

# ── Release gate (pós-release, requer rede) ───────────────────────────────────
# Valida que o binário publicado é consumível (download + sha256 + version).
# Uso: make release-gate VERSION=v0.1.2
release-gate:
	@if [ -z "$(VERSION)" ]; then \
		echo "ERROR: VERSION required. Usage: make release-gate VERSION=v0.1.2"; \
		exit 1; \
	fi
	@bash scripts/release_gate.sh --version $(VERSION) --platform $(or $(PLATFORM),linux-amd64)

# ── Tag (só após gate passar) ────────────────────────────────────────────────
# Uso: make tag-release VERSION=v0.1.2
tag-release:
	@if [ -z "$(VERSION)" ]; then \
		echo "ERROR: VERSION required. Usage: make tag-release VERSION=v0.1.2"; \
		exit 1; \
	fi
	@echo "Re-running gate-cli before tagging..."
	$(MAKE) gate-cli
	git tag -a $(VERSION) -m "orbit-engine $(VERSION)"
	@echo "✅ Tagged $(VERSION). Push with: git push origin $(VERSION)"

# ── Publish skill (orbit-engine SSOT → orbit-prompt distribuição) ────────────
# Coordena release entre os dois repos. Fail-closed em qualquer inconsistência.
#
# Uso:
#   make publish-skill REPO_VERSION=v0.3.0
#   make publish-skill REPO_VERSION=v0.3.0 DRY_RUN=1
#
# Pré-requisito humano: entrada `## [X.Y.Z]` no topo de orbit-prompt/CHANGELOG.md.
# Sem isso o script falha e mostra o template a preencher.
publish-skill:
	@REPO_VERSION=$(REPO_VERSION) DRY_RUN=$(or $(DRY_RUN),0) \
		ORBIT_PROMPT_REPO=$(ORBIT_PROMPT_REPO) \
		bash scripts/publish_skill.sh

# ── Cleanup ───────────────────────────────────────────────────────────────────
clean:
	cd tracking && go clean -testcache
