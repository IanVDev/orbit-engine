# Changelog — orbit-engine

Todas as mudanças notáveis são documentadas aqui.
Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.0.0/).

---

## [1.0.1-rc] — 2026-04-15

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

## [1.0.0] — 2026-04-15

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
