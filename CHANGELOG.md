# Changelog — orbit-engine

Todas as mudanças notáveis são documentadas aqui.
Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.0.0/).

---

## [0.1.1] — 2026-04-21

### 🎯 Marco

**Prod Gate v1** — release gate oficial da CLI, offline, fail-closed, `< 120 s`.
Skill `orbit-engine` (`skill/`) com contrato estrutural travado e versioning
no frontmatter.

### Adicionado

- **`make gate-cli`** (`scripts/gate_cli.sh`) — 11 gates sequenciais com JSON
  report em `gate_report.json`. Cada gate emite `{gate, status, duration_ms, tail}`.
- **Smoke E2E** (`tests/smoke_e2e.sh`) — exercita o binário real (`version`,
  `run` ok/fail, `verify` ok/tamper, `doctor --json`).
- **Contrato de log v1** (`tests/test_log_contract.sh`) — 11 campos obrigatórios
  travados, paridade `run --json` × log persistido.
- **Rollback como comando** (`scripts/orbit_rollback.sh` + `tests/test_rollback.sh`) —
  restaura `.bak` com validação fail-closed.
- **Makefile guard** (`tests/test_makefile_no_dup.sh`) — bloqueia *"overriding recipe"*.
- **Docs scope guard** (`tests/test_docs_dont_claim_v1.sh`) — docs públicos não
  apontam gate do Produto B.
- **Skill contract guard** (`tests/test_skill_contract.sh`, G10) — trava
  frontmatter YAML (name, version, cli_compat) + seções invariantes
  (Observable Patterns, Output Format, Rules, Gating Rules, Silence Rule,
  MANDATORY PRE-RESPONSE RULE) + tokens do output (DIAGNOSIS, ACTIONS, DO NOT
  DO NOW) em `skill/SKILL.md`.
- **Gate doc parity guard** (`tests/test_gate_doc_parity.sh`, G11) — exige que
  a contagem de gates em `scripts/gate_cli.sh` bata com a tabela em
  `docs/CLI_RELEASE_GATE.md`. Previne divergência silenciosa ao adicionar
  novos gates.
- **Skill frontmatter** (`skill/SKILL.md`) — campos `version: 0.1.1` e
  `cli_compat: ">=0.1.1"` para sinalizar breaking changes da skill vs CLI.
- **Install one-liner** (`scripts/install_remote.sh`) — `curl -fsSL ... | bash`
  baixa binário pré-compilado do GitHub Releases, valida `.sha256`, instala em
  `~/.orbit/bin/orbit` (sem sudo), faz smoke test. Diferente de
  `scripts/install.sh` (que exige Go local para compilar), este é o caminho
  para usuários finais que só querem usar a CLI. Fluxo fail-closed em 5 passos:
  detect OS/ARCH → resolve versão (latest ou pinned) → download bin+sha256 →
  `sha256sum -c` → install + `orbit version` smoke. Toda falha imprime
  **CAUSA + AÇÃO** estruturados (não só stacktrace). Anti-regressão em
  `tests/test_install_remote.sh`: 6 cenários (happy path + 5 fail-closed:
  binário 404, sha256 404, sha256 adulterado, smoke version mismatch,
  prefix sem permissão) via mock HTTP local.
- **`orbit release <version>`** — subcomando que automatiza a última milha
  do release (antes manual, único passo com risco de erro humano). Fluxo
  fail-closed em 6 passos: [1/6] valida formato `vX.Y.Z[-suffix]`; [2/6]
  valida estado do repo (na main, clean, sync com `origin/main`); [3/6]
  valida que tag não existe (local nem remoto); [4/6] roda `make gate-cli`
  (pulável via `--skip-gate`, não recomendado); [5/6] cria tag anotada e
  faz push — detecta HTTP 403/forbidden/permission denied e loga
  `CRITICAL: environment has no write permission`, com rollback automático
  da tag local; [6/6] (opt-in via `--wait-ci`) aguarda o release.yml
  publicar e roda `release_gate.sh` automaticamente. Uso:
  `orbit release v0.1.2`, `orbit release --wait-ci v0.1.2`. Anti-regressão:
  `tests/test_orbit_release.sh` cobre 7 cenários (VERSION malformada,
  branch != main, tree sujo, HEAD ahead, tag duplicada local/remote, happy
  path) em repo sintético local.
- **Release Gate Soberano** (`scripts/release_gate.sh` + `make release-gate`) —
  valida distribuição pública pós-build, fail-closed em 5 passos: tag existe
  no remoto, binário no GitHub Releases (HTTP 200), `.sha256` no Releases,
  download + `sha256sum -c`, binário reporta exatamente a versão esperada.
  Integrado em `.github/workflows/release.yml` como job `release-gate`
  (roda após `publish release`). Par operacional: `gate-cli` é pré-build
  offline determinístico; `release-gate` é pós-build e valida que o release
  é consumível do jeito que o README documenta. Anti-regressão:
  `tests/test_release_gate.sh` cobre 6 cenários (1 happy path + 5 fail-closed:
  tag ausente, binário 404, sha256 404, sha256 adulterado, version mismatch)
  via mock HTTP local, sem dependência de rede externa.
- **`orbit hygiene install|check`** — subcomando que instala/valida o pre-commit
  hook em `orbit-hygiene/` (block >5MB, warn >1MB).
- **CI regression guards** (`.github/workflows/regression-guards.yml`) — job de
  tamanho + `TestNoLargeBinariesTracked` (bloqueia qualquer tracked >5MB).
- **CI job "Prod Gate v1 (CLI)"** — `make gate-cli` rodando a cada push/PR,
  com upload de `gate_report.json` como artefato.
- **`docs/CLI_RELEASE_GATE.md`** — contrato oficial da CLI (9 gates detalhados).
- **Shadow mode do SkillRouter** (Produto B, backward-compat) — `Source` +
  `ActivationPossible` em `SkillEvent`; `observe_decision()` no
  `orchestrator/client.py`.

### Alterado

- **`Makefile`**: `gate-release` → `gate-server` (Produto B); aliases
  retrocompat preservados; alvo `orbit-check` duplicado renomeado para
  `orbit-check-remote`.
- **`tracking/go.mod`**: `go 1.25.4` → `1.24` (permite `make gate-cli` rodar
  offline sem fetch de toolchain).
- **Docs do Produto B** movidos para `docs/server-stack/` com banner de escopo:
  `LAUNCH_READINESS.md`, `V1_CONTRACT.md`, `V1_RELEASE_PLAN.md`, `RELEASE_PHASE_15.md`.
- **CLAUDE.md**: fundido — diretiva PT + Core Rules técnicas.

### Removido (higiene do repo)

- `DONE_100.md` (documento de status fora de escopo).
- `product-management.plugin` (binário 59 KB fora de escopo).
- `scripts/hooks/` (duplicava `orbit-hygiene/` — violação DRY; código Go aponta
  para `orbit-hygiene/` como fonte).
- 6 binários Go >5 MB removidos do índice (`tracking/main`, `orbit-gateway`,
  `tracking-server`, `tracking-server-4f58bda-darwin-arm64`, `validate_env`,
  `validate_gov`) + adicionados ao `.gitignore`.
- `.claude/skills/orbit-engine.skill` (75 linhas de PromQL governance do
  Produto B) — conteúdo órfão contraditório ao `skill/SKILL.md` real. Preservação
  explícita em `.gitignore` removida. Comentário em `tracking/repo_hygiene_test.go`
  atualizado.

### Corrigido

- `TestMain` duplicado em `tracking/cmd/orbit` (Go rejeita 2 no mesmo package):
  `quickstart_test.go` agora reutiliza `uxAuditBin` do `ux_audit_test.go`.

### Verificação

```bash
make gate-cli        # → 9/9 PASS em < 30s (offline)
```

---

## [0.1.0] — 2026-04-19

### 🎯 Marco

Primeira release pública. Posicionamento de **Fase 1** conforme
`MONETIZATION.md`: ferramenta CLI local, gratuita, sem cadastro, sem
servidor obrigatório, sem cobrança. Verifiable end-to-end.

### O que esta release entrega

- **Binário `orbit`** para `linux/amd64`, `linux/arm64`, `darwin/amd64`,
  `darwin/arm64` (publicados via GitHub Releases com SHA256 ao lado).
- **Loop fechado de execução:**
  `run → event → decision → snapshot → log[+diagnosis inline] → verify → diagnose`.
- **Subcomandos do CLI:**
  - `orbit run <cmd>` — executa com proof SHA256 e log append-only em `~/.orbit/logs/`
  - `orbit verify <log>` — re-valida proof de um log persistido
  - `orbit diagnose [log]` — analisa o último log (parsers de `go test` e `go build`)
  - `orbit doctor [--alert-only]` — diagnóstico de ambiente (PATH, commit stamp, conectividade)
  - `orbit context-pack` — gera pacote de contexto para transição entre conversas
  - `orbit stats` — estatísticas locais via tracking-server opcional
  - `orbit update` — atualização via GitHub Releases
  - `orbit quickstart` — jornada init → run → proof → verify
- **Dashboard Next.js local** lendo `~/.orbit/logs/*.json` direto, sem
  re-parse: surfacea `recent_diagnoses` com badge `error_type` (TEST/BUILD)
  e métrica `silenced_events` por comando.
- **Contrato de expansão de parser** materializado em código:
  `expansion_policy` + `expansion_candidates` + `EXPANSION_DECISION_PROTOCOL`.
- **Template de PR** para novo parser (`.github/PULL_REQUEST_TEMPLATE/new_parser.md`).

### O que esta release explicitamente NÃO entrega

- Sem dashboard hospedado (apenas local).
- Sem login, conta, sessão ou billing.
- Sem cobrança de qualquer espécie (ver `MONETIZATION.md` Fase 1).
- Sem suporte a Windows (apenas Linux + macOS).
- Sem parsers além de `go test` e `go build` — outros nascem só sob
  sinal real (ver `EXPANSION_DECISION_PROTOCOL`).
- Sem telemetria não-opt-in.

### Garantias verificáveis

- **Proof SHA256** por execução, re-validável via `orbit verify`.
- **Não escreve fora de `$ORBIT_HOME`** — travado por
  `tests/test_no_user_writes.sh`.
- **Léxico do README hero** travado por `tests/test_readme_claims.sh`.
- **Skill orbit-engine** silencia (não ativa) em estado saudável —
  validado por `tests/test_discourse_coherence.py`.
- **Suite verde:** `go test ./...`, 11 Python dashboard tests,
  3 coherence tests, 4 shell guards.

### Para quem serve

Desenvolvedores Go (parsers cobrem `go test` + `go build`) que querem
registro auditável de execuções locais com diagnóstico determinístico
e fail-closed. Sem requerer infra — instala um binário, roda, vê log.

### Onde está depende de sinal real

- Parsers para `cargo`, `tsc`, `rustc`, etc.: só sob `expansion_candidates`
  persistente conforme `EXPANSION_DECISION_PROTOCOL`.
- Hospedagem opcional: só na Fase 2 (ver `MONETIZATION.md`).
- Cobrança por atividade: só na Fase 3, após 30 dias de `resource_cost`
  observado.

### Verificação de instalação

```bash
orbit version          # imprime: orbit version v0.1.0 (commit=... build=...)
orbit run echo hello   # gera log em ~/.orbit/logs/
orbit verify ~/.orbit/logs/*.json | head -1   # confirma proof
```

---

## Entradas anteriores — milestones internos não publicados

> As entradas abaixo documentam fases de validação operacional internas
> conduzidas antes da repivotagem para o posicionamento de Fase 1. Nenhuma
> chegou a ser publicada como tag git pública. Estão preservadas como
> histórico técnico, não como contrato com usuário.

## [1.0.1-rc] — 2026-04-15 *(internal milestone, never tagged)*

### 🎯 Marco

Fase de **validação operacional**. Todos os artefatos para provar que a v1.0
funciona sob estresse real, não apenas em testes unitários.

### Adicionado

- **`orbit_skill_activation_latency_seconds`** — nova métrica histogram
  que mede o tempo (em segundos) da sessão até a primeira ativação.
  Buckets: 1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600.

- **Recording rules de latência** em `orbit_rules.yml`:
  - `orbit:activation_latency_p50:prod`
  - `orbit:activation_latency_p95:prod`

- **7 alertas obrigatórios** em `orbit_rules.yml`:
  - `OrbitSilence` — nenhum evento há 10min (warning)
  - `OrbitTrackingFailures` — falhas de tracking (critical)
  - `OrbitSeedContamination` — seed em prod (critical)
  - `OrbitGatewayDown` — gateway não responde (critical)
  - `OrbitTrackingDown` — tracking server morto (critical)
  - `OrbitZeroSessions` — zero sessões em 30min (warning)
  - `OrbitHighBlockRate` — taxa alta de bloqueios no gateway (warning)

- **LAUNCH_READINESS.md** — checklist de launch com:
  - 10 critérios binários PASS/FAIL
  - Plano de validação 24h
  - 6 cenários de falha documentados
  - Critério GO/NO-GO

- **scripts/mission_24h.sh** — validação contínua 24h com 5 cenários:
  long_session, no_activation, late_activation, high_waste, multi_mode

- **scripts/fault_injection.sh** — 6 testes de injeção de falha com auto-revert

- **Painel "Activation Latency (p50 / p95)"** no dashboard Grafana (id 25)

### Alterado

- **V1_CONTRACT.md** — adicionada `orbit_skill_activation_latency_seconds` +
  recording rules de latência ao contrato público
- **v1_contract_test.go** — 21 subtests (era 19): novo `activation_latency_observed`
  e `governance_allows_activation_latency_rule`
- **Dashboard Grafana** — 19 painéis (era 18), versão 3

### Validado

- ✅ `go test ./... -v -count=1` — todos os testes passam
- ✅ Dashboard JSON válido com 19 painéis
- ✅ 7 alertas configurados em orbit_rules.yml
- ✅ Contract test: 12 métricas de tracking verificadas

---

## [1.0.0] — 2026-04-15 *(internal milestone, never tagged)*

### 🎯 Marco

Primeira release verificável do orbit-engine. Define a **linha de base** para
métricas, governança, observabilidade e segurança.

### Adicionado

- **V1_CONTRACT.md** — contrato público da superfície v1.0:
  - 7 endpoints HTTP (tracking :9100, gateway :9091, seed :9101)
  - 15 métricas Prometheus (11 tracking + 4 gateway)
  - 10 recording rules (prod + seed)
  - PromQL governance fail-closed (base + strict)
  - Esquema JSON do SkillEvent
  - 6 invariantes fail-closed documentados

- **V1_RELEASE_PLAN.md** — plano de execução em 5 fases:
  1. Grafana completo
  2. Separação de contrato
  3. Gate de release
  4. Modelo de ameaça
  5. Tag + release

- **THREAT_MODEL.md** — modelo de ameaça STRIDE com 7 vetores:
  - T1 Malformed Event Injection
  - T2 Governance Bypass
  - T3 Seed Contamination
  - T4 Resource Exhaustion
  - T5 Gateway Down
  - T6 Event Replay
  - T7 Upstream Manipulation

- **tracking/v1_contract_test.go** — teste anti-regressão (19 subtestes):
  - Verifica existência de todas as 11 métricas tracking + 4 gateway
  - Valida labels `mode` e `instance_id`
  - Testa governança (rejeita raw, aceita recording rules, rejeita vazio)
  - Testa FlexTime (rejeita sem timezone)
  - Testa SkillEvent validation fail-closed

- **Makefile** — gate unificado de release:
  - `make test-go` / `make test-go-contract` / `make test-python`
  - `make validate-e2e` / `validate-env` / `validate-gov` / `validate-promql`
  - `make gate-v1` — todos os gates devem passar
  - `make tag-v1` — executa gate-v1 + cria tag git

- **deploy/grafana-dashboard.json** — 4 novos painéis (13 total):
  - Sessions (prod) — timeseries com 3 séries
  - Skill Activation Rate (%) — stat com thresholds
  - Tracking Failures (prod) ⚠️ — timeseries vermelho
  - Seed Contamination — stat com mapping CLEAN/CONTAMINATED

### Alterado

- **scripts/validate_dashboard_queries.sh** — 6 novas queries de validação:
  - `orbit:sessions_total:prod`
  - `orbit:sessions_with_activation:prod`
  - `orbit:sessions_without_activation:prod`
  - `orbit:tracking_failures_total:prod`
  - `orbit:seed_contamination`
  - Activation rate range query

### Inventário v1.0

| Componente | Contagem |
| --- | --- |
| Go source files | 10 |
| Go test files | 4 |
| Go tests (subtests) | ~65 |
| CLI validators | 4 |
| Python test suite | 6 arquivos, ~40 evals |
| Recording rules | 14 |
| Grafana panels | 13 |
| Prometheus scrape jobs | 3 |
| Threats modeladas | 7 |

### Verificação

```bash
# Gate completo (todos devem ser PASS):
make gate-v1

# Teste de contrato isolado:
cd tracking && go test -run "TestV1" -v

# Validação de queries Grafana:
bash scripts/validate_dashboard_queries.sh
```

---

## [0.x] — Pré-release

Todo o desenvolvimento anterior à v1.0. Sem contrato formal de estabilidade.
