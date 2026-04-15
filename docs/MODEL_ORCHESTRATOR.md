# Model Orchestrator — Arquitetura

> **Versão**: 0.1.0
> **Data**: 2026-04-15
> **Regra**: Se o custo estimado excede o budget, a chamada NÃO acontece.

---

## Problema

Uso descontrolado de Opus consome budget rapidamente. Sonnet resolve ~85%
das tarefas com fração do custo. A decisão de qual modelo usar não deveria
ser humana — deveria ser um gate automático com override explícito.

## Princípio

**Sonnet é o default. Opus é uma exceção justificada.**

Qualquer chamada que não satisfaça os critérios de escalação roda em Sonnet.
Se o orquestrador não consegue decidir, roda em Sonnet (fail-closed = modelo
mais barato).

---

## Heurísticas de Roteamento

### Critérios de escalação para Opus

Uma tarefa só é roteada para Opus se **pelo menos 2** destes critérios forem
verdadeiros:

| # | Critério | Como detectar | Peso |
|---|----------|---------------|------|
| 1 | **Raciocínio multi-step** | Prompt contém "analise", "compare", "avalie trade-offs", "arquitete" | 1 |
| 2 | **Contexto > 8K tokens** | `len(prompt_tokens) > 8000` | 1 |
| 3 | **Histórico de falha em Sonnet** | Tarefa já falhou uma vez em Sonnet nesta sessão | 2 |
| 4 | **Override explícito** | Usuário marcou `[opus]` ou `--opus` | 3 (automático) |
| 5 | **Criticidade alta** | Tarefa afeta produção, segurança ou contrato | 1 |

Threshold: `soma_pesos >= 2` → Opus. Caso contrário → Sonnet.

### Critérios de bloqueio (fail-closed)

Mesmo que a tarefa peça Opus, ela é **bloqueada** se:

| Gate | Condição | Ação |
|------|----------|------|
| Budget excedido | `budget_remaining < estimated_cost` | BLOCK — não executa |
| Rate limit | `opus_calls_last_hour > MAX_OPUS_PER_HOUR` | DOWNGRADE → Sonnet |
| Prompt vazio | `len(prompt.strip()) == 0` | BLOCK — erro |
| Contexto explosivo | `estimated_total_tokens > MAX_CONTEXT` | BLOCK — pedir reformulação |

---

## Pipeline: Análise → Decisão → Execução → Validação

```
                    ┌─────────────┐
                    │   Prompt    │
                    │  (entrada)  │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  ANÁLISE    │
                    │             │
                    │ • Contar tokens estimados
                    │ • Classificar complexidade
                    │ • Verificar histórico sessão
                    │ • Checar budget restante
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  DECISÃO    │──── fail-closed gates ────┐
                    │             │                           │
                    │ • Calcular score de escalação            │
                    │ • Aplicar gates de bloqueio              ▼
                    │ • Escolher modelo               ┌──────────────┐
                    └──────┬──────┘               │  BLOQUEADO   │
                           │                      │  (não executa)│
                    ┌──────▼──────┐               └──────────────┘
                    │  EXECUÇÃO   │
                    │             │
                    │ • Chamar modelo escolhido
                    │ • Medir tokens reais (in/out)
                    │ • Registrar decisão no log
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ VALIDAÇÃO   │
                    │             │
                    │ • Comparar custo real vs estimado
                    │ • Atualizar budget restante
                    │ • Detectar drift de custo
                    │ • Sinalizar se Sonnet teria bastado
                    └─────────────┘
```

---

## Modelo de Custo

### Tabela de preço (referência, ajustar conforme Anthropic)

| Modelo | Input (por 1M tokens) | Output (por 1M tokens) |
|--------|----------------------|----------------------|
| Sonnet 4 | $3.00 | $15.00 |
| Opus 4 | $15.00 | $75.00 |

### Estimativa de custo

```
estimated_cost = (input_tokens / 1M × input_price) + (output_tokens / 1M × output_price)
```

Output estimado: `min(input_tokens × 0.8, MAX_OUTPUT_TOKENS)` como heurística
conservadora.

---

## Logging de Decisões

Cada chamada gera um registro JSONL:

```json
{
  "timestamp": "2026-04-15T18:30:00Z",
  "session_id": "sess-abc123",
  "task_id": "task-001",
  "prompt_tokens_estimated": 2400,
  "model_chosen": "sonnet",
  "escalation_score": 1,
  "escalation_reasons": [],
  "gates_passed": true,
  "blocked": false,
  "block_reason": null,
  "actual_input_tokens": 2380,
  "actual_output_tokens": 1200,
  "estimated_cost_usd": 0.025,
  "actual_cost_usd": 0.023,
  "cost_drift_pct": -8.0,
  "sonnet_would_suffice": null,
  "duration_ms": 3400
}
```

---

## Integração com orbit-engine

O orquestrador alimenta o tracking existente:

- `orbit_skill_activations_total{mode="auto"}` → chamada roteada automaticamente
- `orbit_skill_waste_estimated` → custo excedente quando Opus foi usado mas Sonnet bastava
- `orbit_skill_tokens_saved_total` → tokens economizados por usar Sonnet em vez de Opus
- Nova métrica candidata: `orbit_model_routing_total{model, reason}` (futuro)

---

## Estratégia de Testes

### Anti-regressão de custo

| Teste | O que valida | Fail condition |
|-------|-------------|----------------|
| `test_simple_prompt_routes_to_sonnet` | Prompt curto → Sonnet | Se roteado para Opus |
| `test_complex_prompt_routes_to_opus` | Prompt com 2+ critérios → Opus | Se roteado para Sonnet |
| `test_budget_exceeded_blocks` | Budget 0 → BLOCK | Se executar |
| `test_rate_limit_downgrades` | 10+ Opus/hora → downgrade | Se permitir Opus |
| `test_empty_prompt_blocked` | Prompt vazio → BLOCK | Se executar |
| `test_override_opus_always_works` | `[opus]` → Opus direto | Se não escalar |
| `test_cost_estimation_within_30pct` | Real vs estimado < 30% drift | Se drift > 30% |
| `test_sonnet_fallback_on_unknown` | Caso ambíguo → Sonnet | Se roteado para Opus |
| `test_logging_every_call` | Toda chamada gera log | Se log vazio |
| `test_budget_decrements` | Budget diminui após chamada | Se budget não mudar |

### Invariantes (fail-closed)

1. Nenhuma chamada de modelo acontece sem budget suficiente
2. Se análise de custo falha → BLOCK (não default para Opus)
3. Se rate limit não pode ser verificado → Sonnet (fail-closed)
4. Toda chamada gera log — sem exceção
5. Override explícito `[opus]` ignora heurísticas mas NÃO ignora budget
