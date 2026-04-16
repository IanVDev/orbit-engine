# Importação de Dashboards Grafana — orbit-engine

## 📊 Dashboards Disponíveis

| Dashboard | File | UID | Painéis | Objetivo |
| --------- | ---- | --- | ------- | -------- |
| Product Command Center | `deploy/grafana-dashboard.json` | `orbit-engine-command-center` | 9 | Visão de negócio |
| Security & Behavior | `deploy/grafana-dashboard-security.json` | `orbit-security-v1` | 12 | Segurança e abuso |

---

## 🚀 Importação Automática via Script

### Pré-requisitos

- **Grafana** rodando e acessível (padrão: `http://localhost:3000`)
- **API Token** com permissão `Admin`
- **curl** e **jq** instalados

### Gerar API Token no Grafana

1. Grafana web UI → clique no ícone **⚙️ Configuration** (rodinha)
2. → **API Tokens**
3. → **Create API Token**
4. Nome: `orbit-import` (exemplo)
5. Role: **Admin**
6. → **Generate**
7. Copie o token gerado (será exibido apenas uma vez)

### Executar Importação

```bash
# Via environment variables
GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<seu_token> make import-dashboards

# Ou via argumentos
bash scripts/import-grafana-dashboards.sh http://localhost:3000 <seu_token>

# Com logs verbosos
VERBOSE=1 GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<seu_token> make import-dashboards
```

### Saída Esperada

```
[HH:MM:SS] Importador de Dashboards Grafana — orbit-engine

[HH:MM:SS] Verificando conectividade com Grafana em http://localhost:3000...
✅ Conectado ao Grafana v10.0.0

[HH:MM:SS] Importando dashboard: orbit-engine — Security & Behavior (uid=orbit-security-v1)
✅ Dashboard importado: ID=42 (orbit-security-v1)

[HH:MM:SS] Importando dashboard: orbit-engine — Product Command Center (uid=orbit-engine-command-center)
✅ Dashboard importado: ID=43 (orbit-engine-command-center)

────────────────────────────────────────────────────────────────────────────
[HH:MM:SS] Resumo: 2 importados, 0 falharam
✅ Todos os dashboards foram importados com sucesso!

Acesse em: http://localhost:3000/dashboards
```

---

## 📝 Importação Manual no Grafana UI

Se preferir importar via interface:

1. Grafana web UI → **+ Create** (canto superior esquerdo)
2. → **Import**
3. → **Upload dashboard JSON file**
4. Selecione um dos arquivos:
   - `deploy/grafana-dashboard-security.json`
   - `deploy/grafana-dashboard.json`
5. → **Load**
6. Configure datasource: **Prometheus** (deve estar pré-configurado como `Prometheus`)
7. → **Import**

---

## 🔍 Verificar Importação

### Via Grafana UI

- **Dashboards** → **Browse**
- Procure por `orbit-engine` ou `Security & Behavior`
- Clique para abrir

### Via API

```bash
curl -H "Authorization: Bearer <token>" \
  http://localhost:3000/api/search?query=orbit-engine
```

---

## 🔌 Integração com Prometheus

Os dashboards usam a datasource Grafana **Prometheus** com UID `${DS_PROMETHEUS}`.

Certifique-se de que:

1. **Prometheus** está rodando em `http://localhost:9090`
2. **Grafana** tem uma datasource Prometheus configurada
3. O arquivo `prometheus.yml` inclui as alertas de segurança:

```yaml
rule_files:
  - "orbit_rules.yml"
  - "deploy/prometheus-alerts-security.yml"
```

---

## 📋 Checklist de Implantação

- [ ] Grafana rodando (`http://localhost:3000`)
- [ ] API Token gerado e com permissão Admin
- [ ] Prometheus rodando (`http://localhost:9090`)
- [ ] `prometheus.yml` inclui `deploy/prometheus-alerts-security.yml`
- [ ] Servidor de tracking rodando (porta 9100)
- [ ] Dashboards importados (`make import-dashboards`)
- [ ] Validar: Grafana → Dashboards → `orbit-engine`

---

## 🐛 Troubleshooting

### ❌ "GRAFANA_TOKEN não definido"

```bash
# ✅ Solução
export GRAFANA_TOKEN=<seu_token>
make import-dashboards
```

### ❌ "Conectado ao Grafana v?"

jq não está instalado — instale via:
```bash
# macOS
brew install jq

# Ubuntu/Debian
apt-get install jq

# Ou a importação continua sem jq, apenas menos verbosa
```

### ❌ "Autenticação falhou — GRAFANA_TOKEN inválido"

- Verifique token em Grafana: Configuration → API Tokens
- Token pode ter expirado — regenere um novo
- Token deve ter role **Admin**

### ❌ "Não foi possível conectar a http://localhost:3000"

```bash
# Verifique se Grafana está rodando
curl http://localhost:3000/api/health

# Se falhar, inicie Grafana:
# Docker: docker run -d -p 3000:3000 grafana/grafana
# Ou local: grafana-server
```

### ❌ Datasource Prometheus não encontrada

No Grafana UI:
1. Configuration → Data Sources
2. Procure por "Prometheus"
3. Se não existir, crie uma:
   - URL: `http://localhost:9090`
   - Save & Test

---

## 📖 Referências

- [Grafana API — Import Dashboard](https://grafana.com/docs/grafana/latest/dashboards/manage-dashboards/#import-a-dashboard)
- [Prometheus — Recording Rules & Alerts](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/)
- [orbit-engine — Security Mode](../../docs/SECURITY.md)
