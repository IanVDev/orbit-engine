# orbit-engine v1.0 — Launch Readiness

> ⚠️  **ESCOPO: Produto B (server stack — tracking-server + gateway + Prometheus/Grafana).**
> **NÃO aplica à CLI `orbit` (v0.1.x)**, que tem seu próprio contrato de release em
> `docs/CLI_RELEASE_GATE.md` e gate `make gate-cli`. Este documento descreve um
> milestone interno nunca tagueado (ver `CHANGELOG.md` — entradas `[1.0.x] *internal,
> never tagged*`). Preservado como histórico técnico do stack observacional.

> **Versão**: 1.0.0  
> **Data**: 2026-04-15  
> **Regra**: Se não pode ser verificado na prática, é considerado inválido.

---

## Resumo leigo

Construir software é metade do trabalho. A outra metade — e a mais perigosa — é
provar que funciona quando tudo dá errado. Este documento é o checklist final
antes de declarar que o Orbit v1.0 está pronto para uso real.

**Um único comando decide:**

```bash
./scripts/prelaunch_gate.sh --smoke   # versão rápida (1h)
./scripts/prelaunch_gate.sh           # gate completo (24h)
```

Se ele terminar com `🟢 VEREDITO: GO`, o sistema está pronto. Se terminar com
`🔴 VEREDITO: NO-GO`, não lança. Sem subjetividade. Sem "quase".

> **Se qualquer critério for FAIL, o script aborta e o veredito é NO-GO.**

---

## 1. Launch Readiness Checklist (10 critérios binários)

Cada item é **PASS** ou **FAIL**. Não existe "quase" ou "parcial".

| # | Critério | Como verificar | Status |
|---|----------|----------------|--------|
| 1 | **Testes Go passam (19+ subtests, 0 fail)** | `cd tracking && go test ./... -v -count=1` | ◻ |
| 2 | **Contract test inclui todas as 12 métricas de tracking** | `go test -run TestV1ContractComplete -v` verifica cada métrica | ◻ |
| 3 | **Governança PromQL rejeita raw `orbit_skill_*`** | `go test -run governance_rejects_raw -v` | ◻ |
| 4 | **Recording rules compilam sem erro** | `promtool check rules orbit_rules.yml` | ◻ |
| 5 | **Dashboard JSON é válido e tem 19+ painéis** | `python3 -c "import json; d=json.load(open('deploy/grafana-dashboard.json')); print(len(d['panels']))"` | ◻ |
| 6 | **Alertas obrigatórios existem (7 regras)** | `grep -c 'alert:' orbit_rules.yml` retorna ≥ 7 | ◻ |
| 7 | **Missão 24h completa sem erros** | `./scripts/mission_24h.sh` executa ciclo completo (ou `MISSION_HOURS=1` para smoke) | ◻ |
| 8 | **Fault injection: 6/6 cenários revertidos** | `./scripts/fault_injection.sh` — todos os gates detectam a falha | ◻ |
| 9 | **Activation latency é observado (histogram >0 samples)** | Enviar evento de ativação, verificar `orbit_skill_activation_latency_seconds_count > 0` | ◻ |
| 10 | **Zero seed contamination em env=prod** | `curl gateway:9091/api/v1/query -d 'query=orbit:seed_contamination'` retorna 0 ou vazio | ◻ |

### Como preencher

```bash
# Comando soberano — verifica todos os 10 critérios automaticamente:
./scripts/prelaunch_gate.sh --smoke    # smoke (1h) — para validação rápida
./scripts/prelaunch_gate.sh            # completo (24h) — para lançamento real

# Ou verificar etapas individualmente:
cd tracking && go test ./... -v -count=1          # Critérios 1-3
promtool check rules ../orbit_rules.yml            # Critério 4
python3 -c "import json; d=json.load(open('../deploy/grafana-dashboard.json')); print('panels:', len(d['panels']))"  # Critério 5
grep -c 'alert:' ../orbit_rules.yml                # Critério 6
```

---

## 2. Plano de Validação 24h (Produção Simulada)

### Objetivo

Provar que o sistema sobrevive a **uso contínuo e variado** durante 24 horas
sem degradação de métricas, acúmulo de erros, ou perda de dados.

### Script

`./scripts/mission_24h.sh`

### Cenários executados por ciclo (~5 min cada)

| Cenário | O que testa | Sinal esperado |
|---------|-------------|----------------|
| **long_session** | Sessão com 25+ eventos e ativação | `sessions_with_activation` incrementa, latency observado |
| **no_activation_session** | 21 eventos sem ativação (excede threshold) | `sessions_without_activation` incrementa, WARN no log |
| **late_activation_session** | Ativação só no evento 15 | Latency alto (>60s normalmente), histogram reflete |
| **high_waste_session** | waste_estimated > 2000 | `orbit:waste_estimated:prod` sobe, gauge reflete pico |
| **multi_mode_session** | Alterna entre auto/suggest/off | Labels `mode` têm todas as variantes populadas |

### Relatórios automáticos

A cada 5 minutos:
- Query das recording rules via gateway
- Verificação de governança (query bloqueada = saudável)
- Verificação de health endpoints

### Critério de sucesso

- **0 falhas de tracking** ao longo de 24h (`orbit:tracking_failures_total:prod == 0`)
- **Staleness nunca > 600s** (alerta OrbitSilence nunca dispara)
- **Activation rate > 0%** (pelo menos alguma sessão ativou)
- **Governance nunca permite raw query** (100% block rate para `orbit_skill_*`)

### Como rodar

```bash
# Produção simulada completa (24h)
TRACKING_HOST=localhost:9100 GATEWAY_HOST=localhost:9091 ./scripts/mission_24h.sh

# Smoke test (1h)
MISSION_HOURS=1 ./scripts/mission_24h.sh
```

---

## 3. Cenários de Falha (Forçar Manualmente)

### Script

`./scripts/fault_injection.sh`

### 6 cenários com auto-revert

| # | Cenário | O que quebra | Gate que deve detectar | Verificação |
|---|---------|-------------|----------------------|-------------|
| 1 | **Remover métrica** | Comenta `skillActivationsTotal` no código | Contract test FAIL | `go test -run TestV1ContractComplete` falha |
| 2 | **Renomear label** | Troca `"mode"` por `"moda"` | Contract test FAIL (mode_labels_coverage) | Label "auto" desaparece |
| 3 | **Matar tracking** | `SIGTERM` no processo tracking | Alert `OrbitTrackingDown` dispara | `orbit_tracking_up` some do scrape |
| 4 | **Seed contamination** | Setar `SetSeedMode(true)` em cmd/main.go | Alert `OrbitSeedContamination` dispara | `orbit:seed_contamination == 1` |
| 5 | **Desabilitar governança** | Bypass no gateway | Queries raw passam (detectável) | `orbit_skill_tokens_saved_total` retorna dados |
| 6 | **Flood (1000 eventos rápidos)** | Sobrecarga | Nenhum — deve absorver sem falhar | `tracking_failures_total` continua 0 |

### Princípio

> Se um gate não detecta a falha correspondente, o gate é inútil.
> Se nenhuma falha é testada, todos os gates são teóricos.

### Como rodar

```bash
cd tracking
../scripts/fault_injection.sh
# Cada cenário reverte automaticamente via git checkout
# Resultado: PASS/FAIL por cenário
```

---

## 4. Métricas Adicionais (Mínimas)

### Adicionada nesta fase

| Métrica | Tipo | Por que é necessária |
|---------|------|---------------------|
| `orbit_skill_activation_latency_seconds` | histogram | Mede **tempo até valor**: quanto tempo da sessão até a primeira ativação. Sem isso, sabemos SE o skill ativa, mas não o quão rápido. |

### Recording rules derivadas

| Regra | Expressão |
|-------|-----------|
| `orbit:activation_latency_p50:prod` | `histogram_quantile(0.50, rate(..._bucket{env="prod"}[5m]))` |
| `orbit:activation_latency_p95:prod` | `histogram_quantile(0.95, rate(..._bucket{env="prod"}[5m]))` |

### Decisão: não adicionar mais nada

Cada métrica nova é uma superfície de manutenção. As 12 métricas de tracking + 4 de gateway + 2 recording rules de latency cobrem:

- **Volume**: sessions, activations, tokens
- **Qualidade**: waste, activation rate, latency
- **Saúde**: tracking_up, staleness, failures, seed_mode
- **Governança**: gateway requests, blocked, errors, latency

> Adicionar mais sem validar o que existe é inflar complexidade sem valor.

---

## 5. Alertas Obrigatórios (Antes do Launch)

### 7 alertas configurados em `orbit_rules.yml`

| Alerta | Severidade | Condição | Por que é obrigatório |
|--------|-----------|----------|----------------------|
| `OrbitSilence` | warning | Staleness > 10min por 2min | Sistema morto parece saudável sem isso |
| `OrbitTrackingFailures` | critical | Qualquer falha em 5min | Fail-closed bloqueando ativações silenciosamente |
| `OrbitSeedContamination` | critical | seed_mode=1 em env=prod | Métricas prod corrompidas |
| `OrbitGatewayDown` | critical | up{gateway}==0 por 1min | Dashboard cego |
| `OrbitTrackingDown` | critical | up{tracking}==0 por 1min | Ingestão morta |
| `OrbitZeroSessions` | warning | 0 sessões novas em 30min | Sistema vivo mas sem valor |
| `OrbitHighBlockRate` | warning | >0.5 blocks/s por 5min | Query em loop ou Grafana misconfigured |

### Configuração necessária

```yaml
# alertmanager.yml (OBRIGATÓRIO antes do launch)
receivers:
  - name: orbit-critical
    # Configurar: email, Slack, PagerDuty, etc.
    
route:
  receiver: orbit-critical
  routes:
    - match:
        severity: critical
      receiver: orbit-critical
      repeat_interval: 5m
```

> **Se Alertmanager não está configurado, os alertas existem mas ninguém ouve.**
> Isso é equivalente a não ter alertas.

---

## 6. Critério Final: GO / NO-GO

### GO se e somente se:

| Gate | Condição |
|------|----------|
| **Tests** | `go test ./... -v -count=1` → 0 FAIL |
| **Contract** | Todas as 12 métricas de tracking presentes no contract test |
| **Recording Rules** | `promtool check rules orbit_rules.yml` → SUCCESS |
| **Dashboard** | JSON válido, 19+ painéis, activation latency presente |
| **Alertas** | 7 regras configuradas, Alertmanager confirmado |
| **Missão 24h** | Pelo menos 1 ciclo completo sem falhas (MISSION_HOURS≥1) |
| **Fault Injection** | 6/6 cenários detectados pelos gates corretos |
| **Seed Clean** | `orbit:seed_contamination` = 0 em prod |
| **Activation Latency** | Histogram tem >0 observações após 1h de uso |
| **Governança** | 100% block rate para queries raw `orbit_skill_*` |

### NO-GO se qualquer um for verdadeiro:

- ❌ Qualquer teste Go falha
- ❌ Contract test não inclui todas as métricas
- ❌ Alertas existem mas Alertmanager não está configurado
- ❌ Missão 24h não foi executada (nem smoke test de 1h)
- ❌ Fault injection não foi rodado
- ❌ Seed contamination detectada em prod
- ❌ Activation latency histogram com 0 observações após uso real

### Veredito atual

> **NO-GO** — O sistema está a **1 ciclo de validação real** de poder
> lançar com segurança. Os artefatos estão prontos. Os gates estão no
> lugar. Os alertas estão configurados. O que falta é **rodar a validação
> e provar que funciona sob estresse real**.

### Próximos passos concretos

1. Subir tracking + gateway + seed + Prometheus + Alertmanager
2. `./scripts/prelaunch_gate.sh --smoke` → confirmar 🟢 GO
3. `./scripts/prelaunch_gate.sh` → gate completo (24h) → confirmar 🟢 GO
4. Se tudo 🟢 → **Tag `v1.0.0`, push, deploy.**

---

## ESSÊNCIA CHECK

| Pergunta | Resposta |
|----------|---------|
| O que este documento adiciona que não existia? | Um critério **binário** e **verificável** para decidir se o v1.0 é real ou teatro |
| Alguém pode seguir isto sem contexto? | Sim — cada item tem comando exato para verificar |
| Existe algo aqui que não pode ser testado? | Não — se existisse, seria removido |
| O sistema está pronto? | Não. Está a 1 ciclo de validação de estar pronto |
| Qual é o risco de lançar sem isto? | Falso positivo de maturidade — parece pronto, mas nunca foi provado |
