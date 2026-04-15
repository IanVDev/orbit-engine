# orbit-engine v1.0 — Contrato Público

> **Versão**: 1.0.0  
> **Data**: 2026-04-15  
> **Regra**: Se não está nesta lista, não é suportado na v1.0.  

## Superfície Suportada

### Endpoints HTTP

| Endpoint | Porta | Método | Descrição | Contrato |
| -------- | ----- | ------ | --------- | -------- |
| `/track` | 9100 | POST | Ingestão de SkillEvent | 200 + `{"status":"ok"}` ou 400 + erro |
| `/metrics` | 9100 | GET | Prometheus scrape (tracking) | Formato OpenMetrics |
| `/health` | 9100 | GET | Liveness | 200 |
| `/api/v1/query` | 9091 | GET/POST | PromQL via gateway | Governança fail-closed |
| `/api/v1/query_range` | 9091 | GET/POST | PromQL range via gateway | Governança fail-closed |
| `/health` | 9091 | GET | Gateway liveness | 200 |
| `/metrics` | 9091 | GET | Gateway self-metrics | Formato OpenMetrics |

### Métricas Prometheus (contrato v1.0)

#### Tracking Server (:9100)

| Métrica | Tipo | Labels | Descrição |
| ------- | ---- | ------ | --------- |
| `orbit_skill_activations_total` | counter | `mode` | Ativações por modo (auto/suggest/off) |
| `orbit_skill_tokens_saved_total` | counter | — | Tokens economizados acumulados |
| `orbit_skill_waste_estimated` | gauge | — | Último waste estimado |
| `orbit_skill_tracking_failures_total` | counter | — | Falhas de tracking (crítico) |
| `orbit_skill_sessions_total` | counter | — | Total de sessões rastreadas |
| `orbit_skill_sessions_with_activation_total` | counter | — | Sessões com skill ativado |
| `orbit_skill_sessions_without_activation_total` | counter | — | Sessões sem ativação (threshold) |
| `orbit_seed_mode` | gauge | — | 0=prod, 1=seed (imutável) |
| `orbit_tracking_up` | gauge | — | 1=vivo, ausência=morto |
| `orbit_instance_id` | gauge | `instance_id` | Identidade única do processo |
| `orbit_last_event_timestamp` | gauge | — | Unix epoch do último evento |

#### Gateway (:9091)

| Métrica | Tipo | Labels | Descrição |
| ------- | ---- | ------ | --------- |
| `orbit_gateway_requests_total` | counter | `path`, `method` | Requests recebidas |
| `orbit_gateway_blocked_total` | counter | `reason` | Requests bloqueadas |
| `orbit_gateway_errors_total` | counter | `type` | Erros de upstream |
| `orbit_gateway_latency_ms` | histogram | — | Latência p50/p95/p99 |

#### Recording Rules (orbit_rules.yml)

| Regra | Expressão base |
| ----- | -------------- |
| `orbit:tokens_saved_total:prod` | `orbit_skill_tokens_saved_total{env="prod"}` |
| `orbit:activations_total:prod` | `sum by (mode) (orbit_skill_activations_total{env="prod"})` |
| `orbit:waste_estimated:prod` | `orbit_skill_waste_estimated{env="prod"}` |
| `orbit:sessions_total:prod` | `orbit_skill_sessions_total{env="prod"}` |
| `orbit:sessions_with_activation:prod` | `orbit_skill_sessions_with_activation_total{env="prod"}` |
| `orbit:sessions_without_activation:prod` | `orbit_skill_sessions_without_activation_total{env="prod"}` |
| `orbit:tracking_failures_total:prod` | `orbit_skill_tracking_failures_total{env="prod"}` |
| `orbit:event_staleness_seconds:prod` | `time() - orbit_last_event_timestamp{env="prod"}` |
| `orbit:seed_contamination` | `orbit_seed_mode{env="prod"} == 1` |
| `orbit:env_series_count` | `count(orbit_skill_tokens_saved_total) by (__name__)` |

### Governança PromQL

| Regra | Comportamento |
| ----- | ------------- |
| Query vazia | REJEITADA (fail-closed) |
| `orbit_skill_*` em query | REJEITADA (usar recording rules) |
| `orbit_` fora da allow-list | REJEITADA (strict mode) |
| Recording rules `orbit:*` | PERMITIDA |
| Métricas de governança (`orbit_seed_mode`, `orbit_tracking_up`, etc.) | PERMITIDA |
| Métricas não-orbit | PERMITIDA (fora do escopo) |

### SkillEvent Schema (JSON)

```json
{
  "event_type": "string (required)",
  "timestamp": "string RFC3339 com timezone (required)",
  "session_id": "string (required)",
  "mode": "auto | suggest | off (required)",
  "trigger": "string",
  "estimated_waste": "float",
  "actions_suggested": "int",
  "actions_applied": "int",
  "impact_estimated_tokens": "int64",
  "event_hash": "string (computed)",
  "prev_hash": "string (computed)"
}
```

### Invariantes (fail-closed)

1. Nenhuma ativação ocorre sem evento rastreado
2. Se tracking falha → erro retornado → caller deve abortar
3. Gateway: upstream inacessível → 503 (nunca fallback)
4. `orbit_seed_mode` é imutável após primeira chamada
5. Timestamps sem timezone são rejeitados
6. Timestamps > 5min no futuro ou > 24h no passado são rejeitados

## Capacidade Interna (NÃO suportada na v1.0)

> Existe no código mas não faz parte do contrato público.

- Decision engine completo (3 gates, baselines, evidence log)
- Trend analysis (TA-01 a TA-05)
- Metrics report aggregate
- Session result schema v1 (impact feedback)
- Gaming detector
- Feedback collector

## Backlog de Exposição Futura

> Candidatos a contrato em versões futuras.

- Task-level observability (métricas por tipo de tarefa)
- Alerting rules (Prometheus alerts via orbit_rules.yml)
- Rate limiting no gateway
- Auth no endpoint /track
- WebSocket streaming de eventos
- Multi-tenant support (labels por tenant)
