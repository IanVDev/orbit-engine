# orbit-engine v1.0 — Plano de Release (server stack)

> ⚠️  **ESCOPO: Produto B (tracking-server + gateway).** NÃO é o plano da CLI
> publicada em v0.1.x. Para a CLI, ver `docs/CLI_RELEASE_GATE.md`. Este plano
> corresponde a um milestone interno, nunca tagueado como release pública.

> **Data-base**: 2026-04-15  
> **Branch**: `main` (tag `v1.0.0` após gate pass)  
> **Regra de ouro**: nada entra na v1.0 sem teste + observabilidade  

---

## RESUMO LEIGO

O Orbit já tem um motor funcional: tracking de eventos, gateway PromQL
com governança, dashboard Grafana, hash chain, detecção de sessões sem
skill, e segurança de ambiente (seed vs prod). A v1.0 não é reconstruir
— é **congelar o subconjunto que funciona**, blindar com testes, e ter
um dashboard que mostre se está vivo ou morto em 10 segundos.

---

## ENTENDIMENTO

### O que existe hoje (inventário real)

| Camada | Artefato | Estado |
|--------|----------|--------|
| **Tracking Server** | `cmd/main.go` → `:9100` | ✅ Funcional: `/track`, `/metrics`, `/health` |
| **Gateway PromQL** | `cmd/gateway/main.go` → `:9091` | ✅ Funcional: fail-closed, governança strict |
| **Seed Server** | `cmd/seed/main.go` → `:9101` | ✅ Funcional: dados sintéticos |
| **PromQL Governance** | `promql_gov.go` | ✅ 2 modos (base + strict), allow-list |
| **Recording Rules** | `orbit_rules.yml` | ✅ 14 regras (prod + seed) |
| **Prometheus Config** | `prometheus.yml` | ✅ 3 jobs (tracking, seed, gateway) |
| **Dashboard Grafana** | `grafana-dashboard.json` | ⚠️ 9 painéis — faltam painéis operacionais |
| **Testes Go** | `tracking_test.go` + `gateway_test.go` + `promql_gov_test.go` | ✅ ~40 testes |
| **Testes Python** | `tests/` — 18 evals + session_result + trend + metrics | ✅ Cobrem contrato do skill |
| **Validadores CLI** | `cmd/validate*` (4 binários) | ✅ Smoke, env, gov, promql |
| **Scripts** | `simulate_usage.sh`, `validate_dashboard_queries.sh` | ✅ E2E operacional |
| **Systemd** | `orbit-gateway.service` | ✅ Hardened (fail-closed restart) |
| **Segurança** | `SECURITY.md` | ⚠️ Existe mas falta threat model operacional |

### O que falta para a v1.0

1. Dashboard Grafana incompleto (falta: sessions, failures, hash integrity, staleness alert)
2. Sem Makefile/script unificado que rode TODOS os gates
3. Sem threat model mínimo documentado como código
4. Sem contrato formal (quais endpoints/métricas são "suportados")
5. Sem tag de release e changelog
6. Sem teste anti-regressão que cubra o fluxo completo end-to-end com exit code

---

## AÇÕES CONSCIENTES

### Fase 1 — Grafana Completo (dia 1)
- [ ] Adicionar painéis: sessions_total, sessions_with/without_activation, tracking_failures
- [ ] Adicionar painel: staleness com alerta visual (>300s amarelo, >600s vermelho)
- [ ] Adicionar painel: hash integrity (orbit_instance_id presente)
- [ ] Adicionar painel: seed contamination indicator

### Fase 2 — Contrato v1.0 (dia 1)
- [ ] Criar `V1_CONTRACT.md` definindo a superfície suportada
- [ ] Criar `v1_contract_test.go` que valida todas as métricas do contrato

### Fase 3 — Gate Unificado (dia 2)
- [ ] Criar `Makefile` com targets: `test-go`, `test-python`, `validate-e2e`, `gate-v1`
- [ ] O target `gate-v1` é o gate obrigatório: tudo passa ou não tagueia

### Fase 4 — Threat Model (dia 2)
- [ ] Criar `THREAT_MODEL.md` com ameaças mapeadas ao código existente
- [ ] Cada ameaça aponta para a mitigação no código ou é marcada como gap

### Fase 5 — Tag + Release (dia 3)
- [ ] `git tag v1.0.0` somente após `make gate-v1` passar
- [ ] CHANGELOG.md com o estado congelado

---

## RECOMENDAÇÕES 11/10

### 1. O Grafana precisa de exatamente 4 painéis a mais

Painéis que faltam e que correspondem a métricas **já exportadas**:

| Painel | Métrica | Tipo |
|--------|---------|------|
| Sessions (prod) | `orbit:sessions_total:prod` | timeseries |
| Skill Activation Rate | `orbit:sessions_with_activation:prod / orbit:sessions_total:prod` | stat % |
| Tracking Failures | `orbit:tracking_failures_total:prod` | timeseries (cor vermelha) |
| Seed Contamination | `orbit:seed_contamination` | stat (0=ok, 1=ALERTA) |

### 2. O contrato v1.0 deve ser um subconjunto explícito

Ver `V1_CONTRACT.md` criado junto com este plano.

### 3. O teste anti-regressão obrigatório é: `TestV1ContractComplete`

Se esse teste falha, a v1.0 não pode ser tagueada. Ponto final.

### 4. Branch strategy: NÃO criar branch de release

| Decisão | PRÓ | CONTRA | RISCO | VEREDITO |
|---------|-----|--------|-------|----------|
| Taguear direto na main | Simples, sem merge hell | Sem isolation | Baixo: gates protegem | ✅ **APROVADO** |
| Criar branch release/v1.0 | Isolation | Overhead, divergência | Médio: sync manual | ❌ Rejeitado |

**Teste**: `git log --oneline v1.0.0..HEAD` mostra se houve drift pós-release.

### 5. Esteira de testes mínima

| Camada | O que testa | Como valida |
|--------|-------------|-------------|
| **Contrato** | Todas as métricas do v1 existem e têm tipo correto | `v1_contract_test.go` |
| **Comportamento** | Fail-closed, hash chain, session lifecycle | `tracking_test.go` (existente) |
| **Governança** | PromQL governance strict | `promql_gov_test.go` (existente) |
| **Gateway** | Proxy, bloqueio, 503 upstream, métricas | `gateway_test.go` (existente) |
| **E2E** | Servidor sobe, aceita evento, métricas aparecem | `cmd/validate` (existente) |
| **Dashboard** | Queries do Grafana passam pela governance | `validate_dashboard_queries.sh` |
| **Gate** | TODOS acima passam ou release bloqueado | `make gate-v1` |

### 6. Threat model → ver `THREAT_MODEL.md`

### 7. O teste anti-regressão sem o qual a v1.0 NÃO existe

`TestV1ContractComplete` em `v1_contract_test.go`: registra todas as métricas,
dispara um evento, e verifica que CADA métrica do contrato existe, tem o tipo
correto e tem valor > 0 (ou == 0 para failure counters zerados). Se esse teste
quebra, alguém removeu ou renomeou uma métrica da superfície pública.

### 8. Backlog priorizado

| # | Item | Critério de Aceite | Prioridade |
|---|------|-------------------|------------|
| 1 | Dashboard Grafana completo | 13 painéis, `validate_dashboard_queries.sh` passa | P0 — blocker |
| 2 | `V1_CONTRACT.md` + `v1_contract_test.go` | Teste passa, doc existe | P0 — blocker |
| 3 | `Makefile` com `gate-v1` | `make gate-v1` exit 0 | P0 — blocker |
| 4 | `THREAT_MODEL.md` | Cada ameaça tem mitigação ou gap marcado | P0 — blocker |
| 5 | `CHANGELOG.md` v1.0.0 | Arquivo existe com data | P1 |
| 6 | `git tag v1.0.0` | Somente após P0 completos | P1 |
| 7 | Alerting rules (Prometheus alerts) | Staleness > 10min, failure rate > 0 | P2 — pós-v1 |
| 8 | Task-level observability | Métricas por tipo de tarefa | P2 — pós-v1 |
| 9 | Rate limiting no gateway | Proteção contra flood | P2 — pós-v1 |
| 10 | Auth no /track endpoint | Bearer token ou mTLS | P3 — backlog |

---

## ESSÊNCIA CHECK

- [x] Cada item é verificável por código, teste, comando ou dashboard
- [x] Nenhum item é "melhorar X" — todos têm critério binário pass/fail
- [x] Segurança endereçada com threat model que aponta para código real
- [x] Zero overengineering: usa o que já existe, adiciona o mínimo necessário
- [x] O estado atual é o núcleo — nada foi descartado ou refeito
