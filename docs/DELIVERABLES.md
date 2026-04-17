# 🎯 Entrega: Dashboard Segurança & Comportamento do orbit-engine

**Data:** 16 de abril de 2026  
**Versão:** 1.0  
**Status:** ✅ Completo

---

## 📊 Artefatos Entregues

### 1. Dashboard Grafana — Security & Behavior

📁 **`deploy/grafana-dashboard-security.json`**

**Descrição:** Dashboard completo de monitoramento de segurança com 12 painéis interativos.

**Painéis inclusos:**

**Row 1: Status do Sistema (5 stat panels + cores por severidade)**
- 🔒 Security Mode Atual → mapping 0=NORMAL (verde), 1=ELEVATED (amarelo), 2=LOCKDOWN (vermelho)
- 🧬 Abuse Ratio → percentunit com limiar visual (0.08/0.10/0.25/0.30)
- 🔴 Lockdown Ativo? → indicador crítico
- 🚫 Rejeições/5min → taxa agregada de rejeitadas
- 📡 Real Usage Alive → 1=ativo (verde), 0=inativo (vermelho)

**Row 2: Histórico Temporal (2 timeseries)**
- 🕐 Security Mode Timeline → evolução do modo ativo (stepAfter, cores fixas NORMAL/ELEVATED/LOCKDOWN)
- 🔄 Mode Transitions (rate 5m) → `rate(orbit_security_mode_transitions_total[5m])` com legenda `{{from}} → {{to}}`

**Row 3: Comportamento & Abuso (2 timeseries)**
- 🧬 Behavior Abuse Ratio → ratio com thresholdsStyle: line+area nos 4 limiares de histerese
- 💢 Behavior Abuse Events → taxa detectada + taxa rejeitada em dupla série

**Row 4: Rejeições (1 timeseries + 1 bargauge)**
- 📊 Taxa por Motivo (5m) → `rate(orbit_tracking_rejected_total[5m]) > 0` com legenda `{{reason}}`
- 🏆 Volume 1h → `sum by(reason)(increase(...[1h]))` em bar gauge horizontal

**Row 5: Sessões & Ativações (1 timeseries)**
- 📡 Real Usage vs Skill Activations (5m) → comparação: input × output do sistema

### 2. Alertas Prometheus — 6 Regras de Segurança

📁 **`deploy/prometheus-alerts-security.yml`**

| Alerta | Severidade | For | Condição | Descrição |
|--------|------------|-----|----------|-----------|
| `OrbitSecurityLockdown` | critical | 5m | `orbit_security_mode{mode="lockdown"} == 1` | LOCKDOWN ativo > 5 min = ataque sustentado |
| `OrbitBehaviorAbuseRatioHigh` | warning | 1m | `orbit_behavior_abuse_ratio > 0.3` | Ratio acima do limiar de escalada para LOCKDOWN |
| `OrbitCriticalMetricsMissing` | critical | 2m | `absent(orbit_tracking_rejected_total\|orbit_behavior_abuse_total\|orbit_security_mode)` | Métricas críticas não registradas (bug RegisterSecurityMetrics) |
| `OrbitRealUsageDown` | warning | 5m | `orbit_real_usage_alive == 0` | Sem eventos reais por 5 min |
| `OrbitHMACFailureRateHigh` | warning | 2m | `rate(orbit_tracking_hmac_failures_total[5m]) > 0.5` | Taxa de falhas HMAC > 0.5/s |
| `OrbitSecurityElevatedPersistent` | warning | 15m | `orbit_security_mode{mode="elevated"} == 1` | ELEVATED por 15 min |

### 3. Script de Importação Automática

📁 **`scripts/import-grafana-dashboards.sh`**

**Funcionalidade:** Importa dashboards JSON via API HTTP do Grafana.

**Features:**
- ✅ Autenticação Bearer token
- ✅ Validação de conectividade prévia
- ✅ Logs estruturados com timestamp
- ✅ Tratamento de erros com HTTP codes
- ✅ Mode verbose opcional
- ✅ Importação de múltiplos dashboards
- ✅ Resumo final com estatísticas

**Uso:**
```bash
GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<token> bash scripts/import-grafana-dashboards.sh
```

### 4. Stack Docker Compose (Completo)

📁 **`docker-compose.yml`**

**Serviços:** Prometheus + Grafana com provisioning automático

**Features:**
- Health checks para ambos serviços
- Volumes persistentes (prometheus-data, grafana-data)
- Network bridge (orbit-net)
- Configuração automática de datasource Prometheus

**Inicia com:**
```bash
make obs-up
```

### 5. Targets Make (4 novos)

```makefile
validate-dashboard-security   # Valida JSON do dashboard
validate-alerts-security      # Valida YAML dos alertas (requer pyyaml)
import-dashboards             # Importa via script (requer token)
obs-up / obs-down / obs-logs  # Docker compose stack
```

### 6. Documentação

📁 **`docs/GRAFANA-IMPORT.md`** → Guia completo (pré-requisitos, passos, troubleshooting)  
📁 **`docs/DASHBOARDS-QUICKSTART.md`** → Guia rápido (3 opções de importação)

---

## ✅ Checklist de Funcionalidade

- [x] Dashboard JSON válido (12 painéis, uid=orbit-security-v1)
- [x] Queries PromQL corretas (rate, aggregation, labels corretos)
- [x] Cores por severidade: NORMAL=verde, ELEVATED=amarelo, LOCKDOWN=vermelho
- [x] Limiares visuais de histerese no abuse ratio
- [x] Alertas Prometheus com for, labels severity, annotations runbook
- [x] Script importação automática via API HTTP
- [x] Docker compose para Prometheus + Grafana
- [x] Datasource Prometheus auto-provisioned
- [x] Documentação de deploy (2 guias)
- [x] Targets Makefile para validação e import
- [x] Compatibilidade com métricas existentes (RegisterSecurityMetrics)
- [x] Sem dependências externas (curl, jq opcionais, script compatível sem jq)

---

## 🚀 Instruções de Deploy

### Opção 1: Stack Docker (Recomendado)

```bash
# 1. Iniciar Prometheus + Grafana
make obs-up

# 2. Gerar token em Grafana: Configuration → API Tokens → Create
#    (ou usar admin/admin default)

# 3. Importar dashboards
GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<seu_token> make import-dashboards

# 4. Acessar
# Grafana: http://localhost:3000 (admin/admin)
# Prometheus: http://localhost:9090
```

### Opção 2: Instâncias Existentes

```bash
# 1. Adicionar alertas ao prometheus.yml:
rule_files:
  - "orbit_rules.yml"
  - "deploy/prometheus-alerts-security.yml"

# 2. Importar dashboards via script ou UI
GRAFANA_URL=http://seu-grafana:3000 GRAFANA_TOKEN=<seu_token> make import-dashboards

# 3. Ou manual: Grafana UI → Create → Import → Upload JSON
```

---

## 📈 Métricas Monitoradas

**Métricas primárias (do security.go):**

- `orbit_security_mode{mode}` → gauge 0/1 para cada modo
- `orbit_behavior_abuse_ratio` → gauge 0.0–1.0
- `orbit_behavior_abuse_total` → counter
- `orbit_tracking_rejected_total{reason}` → counter
- `orbit_security_mode_transitions_total{from,to}` → counter
- `orbit_real_usage_alive` → gauge 1/0

**Métricas secundárias (derivadas):**

- `rate(orbit_tracking_rejected_total[5m])` → rejeições/segundo
- `rate(orbit_security_mode_transitions_total[5m])` → transições/segundo
- `orbit_skill_activation_total` → (existente, para comparação)

---

## 🔒 Conformidade de Segurança

- ✅ **Fail-closed:** Alertas críticos disparam em modo LOCKDOWN/métricas ausentes
- ✅ **Hysteresis:** Painéis visualizam os 4 limiares assimétricos (0.08/0.10/0.25/0.30)
- ✅ **Cooldown:** Status gauge reflete modo atual (mesmo durante cooldown 30s)
- ✅ **Cardinality:** Labels fixos (mode, from, to, reason) — sem dinâmica
- ✅ **Governance:** Todas as queries passam em `ValidatePromQL` (allow-list)
- ✅ **No data exposure:** Nenhum ID/session/evento bruto em métricas/dashboards

---

## 📝 Arquivos Modificados/Criados

```
deploy/
  ├── grafana-dashboard-security.json          [NOVO] Dashboard 12 painéis
  ├── prometheus-alerts-security.yml           [NOVO] 6 alertas
  └── grafana-provisioning-prometheus.yml      [NOVO] Datasource auto
scripts/
  └── import-grafana-dashboards.sh             [NOVO] Importador via API
docs/
  ├── GRAFANA-IMPORT.md                        [NOVO] Guia completo
  └── DASHBOARDS-QUICKSTART.md                 [NOVO] Quickstart
docker-compose.yml                             [NOVO] Stack Prometheus+Grafana
prometheus.yml                                 [EDIT] Adicionado rule_files alertas
Makefile                                       [EDIT] 4 targets novos
```

---

## ✨ Diferencial de Implementação

1. **Hysteresis Visual:** Painéis de abuse ratio destacam os 4 limiares com thresholdsStyle
2. **Transições Rastreadas:** Counter com labels {from,to} permite análise de padrões
3. **Importação Automática:** Script robusto com retry, logs verbosos, suporta instâncias existentes
4. **Stack Completa:** Docker Compose provisiona Prometheus + Grafana + datasource em 1 comando
5. **Sem Dependências Externas:** Script funciona com curl apenas; jq é opcional
6. **Governança PromQL:** Todas as queries validadas contra allow-list
7. **Cores Semânticas:** Vermelho=LOCKDOWN (crítico), Amarelo=ELEVATED (aviso), Verde=NORMAL (saudável)

---

## 🧪 Validação

```bash
# Validar dashboard JSON
make validate-dashboard-security

# Validar alertas YAML (com pyyaml)
make validate-alerts-security

# Rodar testes Go (confirmam métricas registradas)
cd tracking && go test ./... -count=1

# Teste de importação (requer Grafana rodando)
GRAFANA_TOKEN=fake bash scripts/import-grafana-dashboards.sh
# → Detecta erro 401 Unauthorized (esperado)
```

---

## 📞 Próximos Passos

1. **Gerar token Grafana** (Configuration → API Tokens → Admin role)
2. **Importar dashboards** (`make import-dashboards`)
3. **Gerar tráfego** de teste no `/track`
4. **Observar métricas** ao vivo em Grafana
5. **Criar runbook** de resposta para alertas LOCKDOWN
6. **Integrar notificações** (Slack, PagerDuty, etc.) via Grafana Alerting

---

**Desenvolvido com ❤️ para orbit-engine v1**
