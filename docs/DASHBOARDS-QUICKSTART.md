# 🚀 Importação de Dashboards Grafana — orbit-engine

## 📋 Resumo

Após criar o dashboard JSON `deploy/grafana-dashboard-security.json` e as alertas Prometheus em `deploy/prometheus-alerts-security.yml`, aqui estão as formas de subir tudo:

---

## ⚡ Opção 1: Início Rápido com Docker Compose (Recomendado)

**Requisitos:** Docker e docker-compose

```bash
# Inicia Prometheus + Grafana em containers
make obs-up

# Grafana fica disponível em: http://localhost:3000 (admin/admin)
# Prometheus em: http://localhost:9090
```

### Importar Dashboards

#### Via API automática (se souber o token):

```bash
GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<seu_token> make import-dashboards
```

#### Via UI manual (mais seguro):

1. Acesse http://localhost:3000 → **Configuration** → **API Tokens**
2. **Create token** → Nome: `orbit-import`, Role: **Admin** → **Generate**
3. Copie o token
4. Execute:
   ```bash
   GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<cole_aqui> make import-dashboards
   ```

#### Via UI Grafana (sem token):

1. Acesse http://localhost:3000 → clique **+ Create** → **Import**
2. **Upload dashboard JSON file**
3. Selecione:
   - `deploy/grafana-dashboard-security.json` (🔐 Security & Behavior)
   - `deploy/grafana-dashboard.json` (📊 Product Command Center)
4. **Load** → **Import**

### Parar stack

```bash
make obs-down
```

---

## 📊 Opção 2: Grafana & Prometheus Já Rodando (Instâncias Existentes)

Se você já tem Grafana e Prometheus rodando:

### 1️⃣ Configurar Datasource Prometheus no Grafana

```
Configuration → Data Sources → Add datasource → Prometheus
URL: http://localhost:9090 (ou seu Prometheus)
Save & Test
```

### 2️⃣ Importar Dashboards via API ou UI

**Via API:**

```bash
GRAFANA_URL=http://seu-grafana:3000 GRAFANA_TOKEN=<seu_token> make import-dashboards
```

**Via UI:**

1. Grafana → **+ Create** → **Import**
2. **Upload JSON file**
3. Selecione arquivo do deploy

### 3️⃣ Configurar Alertas no Prometheus

Edite seu `prometheus.yml` e adicione:

```yaml
rule_files:
  - "orbit_rules.yml"
  - "deploy/prometheus-alerts-security.yml"  # ← adicionar esta linha
```

Depois reinicie o Prometheus.

---

## 🔑 Gerar Token Grafana (Manual)

Sem script, via UI:

1. Grafana → ⚙️ **Configuration** (roda no canto)
2. → **API Tokens**
3. → **Create API Token**
   - Name: `orbit-import`
   - Role: **Admin**
   - TTL: (deixar em branco para sem expiração)
4. → **Generate**
5. **Copie** o token (único! será escondido)

---

## ✅ Validar Importação

### Dashboards carregados?

```bash
# Listar via curl
curl -H "Authorization: Bearer <token>" \
  http://localhost:3000/api/search?query=orbit
```

### Alertas definidos?

```bash
# Prometheus UI
http://localhost:9090/alerts

# Ou via API
curl http://localhost:9090/api/v1/rules
```

### Métricas sendo coletadas?

```bash
# Prometheus UI → Graph
# Teste query: orbit_security_mode

# Ou direct
curl 'http://localhost:9090/api/v1/query?query=orbit_security_mode'
```

---

## 📁 Arquivos

| Arquivo | Propósito | Quando usar |
| ------- | --------- | ----------- |
| `docker-compose.yml` | Stack Prometheus + Grafana | Dev/teste rápido |
| `deploy/grafana-dashboard-security.json` | Dashboard segurança | Importar no Grafana |
| `deploy/grafana-dashboard.json` | Dashboard negócio | Importar no Grafana |
| `deploy/prometheus-alerts-security.yml` | Alertas de segurança | Adicionar em prometheus.yml |
| `deploy/grafana-provisioning-prometheus.yml` | Datasource automático | Docker compose provisioning |
| `scripts/import-grafana-dashboards.sh` | Script import via API | Automatizar importação |

---

## 🐛 Troubleshooting

### "Grafana não está respondendo"

```bash
# Verificar se container está rodando
docker ps | grep grafana

# Se não, iniciar
make obs-up

# Logs
make obs-logs
```

### "Prometheus não tem métricas orbit_*"

Certifique-se de que:

1. Servidor de tracking está rodando: `curl http://localhost:9100/metrics | grep orbit`
2. Prometheus está scrapeando corretamente: `curl http://localhost:9090/targets`

### "Alerts não aparecem"

1. Verifique `prometheus.yml` inclui `deploy/prometheus-alerts-security.yml`
2. Reinicie Prometheus: `docker-compose restart prometheus`
3. Acesse http://localhost:9090/alerts e confirme 6 alertas carregados

### "Token inválido na importação"

1. Verifique que o token tem permissão **Admin**
2. Token pode ter expirado — gere um novo
3. Copie exatamente sem espaços extras

---

## 📖 Referências Rápidas

| Recurso | URL |
| ------- | --- |
| Grafana Dashboards | http://localhost:3000/dashboards |
| Prometheus Targets | http://localhost:9090/targets |
| Prometheus Alerts | http://localhost:9090/alerts |
| Prometheus Graph | http://localhost:9090/graph |
| Servidor Tracking | http://localhost:9100/metrics |

---

## 🎯 Próximos Passos

1. **Importar dashboards** (`make import-dashboards`)
2. **Gerar tráfego real** → enviar eventos ao `/track` do tracking server
3. **Observar métricas** → abrir dashboard em Grafana
4. **Testar alertas** → simular abuso comportamental e observar transições de security mode
5. **Documentar findings** → adicionar runbooks e procedures de resposta

---

Dúvidas? Verifique `docs/GRAFANA-IMPORT.md` para guia completo.
