# orbit-engine v1.0 — Threat Model

> **Escopo**: Componentes da v1.0 (tracking server, gateway, prometheus)
> **Metodologia**: STRIDE simplificado, mapeado ao código existente
> **Regra**: Cada ameaça tem mitigação implementada OU gap documentado

---

## Superfície de Ataque

```
[Cliente/Grafana] → :9091 (Gateway) → :9090 (Prometheus)
[Script/Skill]    → :9100 (Tracking Server)
[Prometheus]      → :9100/metrics, :9091/metrics, :9101/metrics (scrape)
```

---

## Ameaças Mapeadas

### T1 — Injeção de evento malformado via /track

| Campo | Valor |
| ----- | ----- |
| **Tipo** | Tampering / Spoofing |
| **Vetor** | POST `/track` com JSON manipulado |
| **Impacto** | Métricas corrompidas, falso positivo de tokens economizados |
| **Mitigação** | ✅ IMPLEMENTADA (MITIGADA) |
| **Código** | `tracking.go:Validate()` — rejeita event_type vazio, session_id vazio, mode inválido |
| **Código** | `tracking.go:FlexTime.UnmarshalJSON()` — rejeita timestamps sem timezone, futuros >5min, passados >24h |
| **Código** | `security.go:ValidateHMAC()` — HMAC-SHA256 sobre payload via header `X-Orbit-Signature` |
| **Código** | `realusage.go:151` — gate HMAC fail-closed antes de qualquer decode/dedupe |
| **Código** | `security.go:SetHMACSecret()` — bootstrap fail-closed em `ORBIT_ENV=production` quando `ORBIT_HMAC_SECRET` está ausente |
| **Teste** | `tracking_test.go:TestTrackFailClosed`, `TestFlexTimeRejectsNoTimezone`, `TestFlexTimeRejectsInvalid` |
| **Teste** | `security_init_test.go:TestSecurity_FailClosedOnMissingHMACInProduction` — subprocess confirma abort em produção sem segredo |
| **Gap** | Nenhum — autenticação no /track exigida em produção, opcional em dev para backward-compat |

### T2 — Bypass de governança PromQL

| Campo | Valor |
| ----- | ----- |
| **Tipo** | Information Disclosure / Elevation |
| **Vetor** | Query direta ao Prometheus (:9090) sem passar pelo gateway |
| **Impacto** | Acesso a métricas raw sem filtro de ambiente (prod+seed misturados) |
| **Mitigação** | ✅ PARCIAL |
| **Código** | `promql_gov.go:ValidatePromQLStrict()` — bloqueia `orbit_skill_*` e identifiers desconhecidos |
| **Código** | `gateway.go:HandleQuery()` — toda query passa por validação antes do proxy |
| **Teste** | `promql_gov_test.go:TestValidatePromQLStrict`, `gateway_test.go:TestGatewayBlocksInvalidQueries` |
| **Gap** | ⚠️ Se alguém acessa Prometheus diretamente (:9090), bypass total |
| **Remediação** | Bind Prometheus a localhost + firewall. Documentar em SECURITY.md |

### T3 — Mistura de ambientes (seed contamination)

| Campo | Valor |
| ----- | ----- |
| **Tipo** | Tampering |
| **Vetor** | Prometheus scrapa prod e seed, query sem `{env=...}` retorna dados misturados |
| **Impacto** | Dashboards mostram dados incorretos |
| **Mitigação** | ✅ IMPLEMENTADA |
| **Código** | `orbit_rules.yml` — recording rules com `{env="prod"}` e `{env="seed"}` |
| **Código** | `promql_gov.go` — bloqueia queries raw que não usam recording rules |
| **Código** | `tracking.go:SetSeedMode()` — imutável após primeira chamada (panic se repetida) |
| **Código** | `prometheus.yml:honor_labels: false` — Prometheus sobrescreve labels |
| **Teste** | `tracking_test.go:TestSeedModeLock`, `promql_gov_test.go:TestValidatePromQLStrict` |
| **Gap** | Nenhum — cobertura completa |

### T4 — Denial of Service no gateway

| Campo | Valor |
| ----- | ----- |
| **Tipo** | Denial of Service |
| **Vetor** | Flood de queries no gateway (:9091) |
| **Impacto** | Gateway sobrecarregado, Grafana sem dados |
| **Mitigação** | ✅ PARCIAL |
| **Código** | `gateway.go:proxyToUpstream()` — timeout de 10s no client HTTP |
| **Código** | `orbit-gateway.service` — `MemoryMax=256M`, `Restart=always` |
| **Teste** | `gateway_test.go:TestGatewayUpstreamTimeout503` |
| **Gap** | ⚠️ Sem rate limiting |
| **Remediação** | P2 backlog: rate limiter no gateway (middleware) |

### T5 — Gateway down → Grafana cega

| Campo | Valor |
| ----- | ----- |
| **Tipo** | Availability |
| **Vetor** | Gateway crash, OOM, restart loop |
| **Impacto** | Grafana não mostra dados (503) |
| **Mitigação** | ✅ IMPLEMENTADA |
| **Código** | `orbit-gateway.service` — `Restart=always`, `RestartSec=3`, `StartLimitIntervalSec=0` |
| **Código** | `gateway.go:HandleHealth()` — endpoint de health para monitoramento |
| **Teste** | `gateway_test.go:TestGatewayHealth`, `TestGatewayUpstreamDown503` |
| **Gap** | Nenhum alerting automático (depende do Prometheus scrape do gateway) |

### T6 — Replay de evento (event_hash spoofing)

| Campo | Valor |
| ----- | ----- |
| **Tipo** | Spoofing / Repudiation |
| **Vetor** | Reenvio do mesmo evento para inflar métricas |
| **Impacto** | tokens_saved artificialmente alto |
| **Mitigação** | ✅ PARCIAL |
| **Código** | `tracking.go:ComputeHash()` — sha256(session_id + timestamp + tokens), hash chain via PrevHash |
| **Teste** | `tracking_test.go:TestEventHashIntegrity` |
| **Gap** | ⚠️ Hash é computado mas NÃO verificado contra duplicatas (sem dedup store) |
| **Remediação** | P3 backlog: in-memory set de hashes recentes com TTL |

### T7 — Upstream Prometheus manipulado

| Campo | Valor |
| ----- | ----- |
| **Tipo** | Tampering |
| **Vetor** | Prometheus upstream retorna dados falsos |
| **Impacto** | Dashboard mostra dados corrompidos |
| **Mitigação** | ✅ PARCIAL |
| **Código** | Gateway faz pass-through — não valida corpo do upstream |
| **Gap** | Aceito: Prometheus é componente trusted. Mitigação é bind a localhost |

---

## Matriz de Gaps e Prioridade

| Gap | Ameaça | Severidade | Prioridade | Remediação |
| --- | ------ | ---------- | ---------- | ---------- |
| Prometheus acessível diretamente | T2 | Média | P1 (doc) | Bind localhost + doc |
| Sem rate limiting | T4 | Baixa | P2 | Middleware |
| Sem dedup de eventos | T6 | Baixa | P3 | Hash set com TTL |

## Verificação

Cada mitigação marcada como ✅ tem pelo menos um teste automatizado associado.
Gaps marcados como ⚠️ têm remediação no backlog com prioridade explícita.

Para verificar que as mitigações estão ativas:

```bash
cd tracking && go test -run "TestTrackFailClosed|TestFlexTime|TestSeedModeLock|TestGatewayBlocksInvalidQueries|TestGatewayUpstreamDown503|TestEventHashIntegrity|TestV1ContractComplete" -v
```
