# Release Phase 15 — Guia de Uso do Sistema Completo

> **orbit-engine** — motor de auto-evolução para skills de IA.
> Versão: Phase 15 · 14 de abril de 2026

---

## Índice

1. [Visão geral](#1-visão-geral)
2. [Pré-requisitos](#2-pré-requisitos)
3. [evolve.py — Ciclo de evolução](#3-evolvepy--ciclo-de-evolução)
4. [trend_report.py — Análise de tendência](#4-trend_reportpy--análise-de-tendência)
5. [metrics_report.py — Métricas agregadas](#5-metrics_reportpy--métricas-agregadas)
6. [Interpretação das métricas](#6-interpretação-das-métricas)
7. [Sinais de risco](#7-sinais-de-risco)
8. [Workflow recomendado](#8-workflow-recomendado)
9. [Referência de exit codes](#9-referência-de-exit-codes)

---

## 1. Visão geral

O sistema é composto por três CLIs independentes:

| CLI | Função |
|-----|--------|
| `evolve.py` | Executa o ciclo completo: backup → validação → decisão (ACCEPT/REJECT/HOLD) |
| `trend_report.py` | Lê o `evidence_log.jsonl` e mostra a tendência temporal da skill |
| `metrics_report.py` | Lê o `evidence_log.jsonl` e calcula métricas agregadas de performance |

Todos dependem **apenas da stdlib do Python** — nenhuma dependência externa.

---

## 2. Pré-requisitos

- Python 3.10+
- Repositório clonado com a estrutura `tests/` intacta
- Baseline salvo (ver seção 3)

```
orbit-engine/
├── skill/SKILL.md          # arquivo da skill
├── tests/
│   ├── evolve.py           # orquestrador de evolução
│   ├── trend_report.py     # relatório de tendência
│   ├── metrics_report.py   # métricas agregadas
│   ├── decision_engine.py  # motor de decisão (3 gates)
│   ├── trend_analysis.py   # análise de tendência (standalone)
│   ├── evidence_log.jsonl  # log append-only (gerado automaticamente)
│   └── .baseline.json      # snapshot de referência
```

---

## 3. evolve.py — Ciclo de evolução

### Salvar baseline (primeira vez)

```bash
python3 tests/evolve.py --save-baseline
```

Grava o estado atual (testes, score, hash da skill) em `.baseline.json`.

### Ciclo completo

```bash
python3 tests/evolve.py skill/SKILL.md
```

Executa: backup → roda testes → compara com baseline → decide → aceita ou restaura.

### Dry run (sem commit)

```bash
python3 tests/evolve.py skill/SKILL.md --dry-run
```

Roda toda a validação mas **não aplica** a decisão — útil para preview.

### Com dados de impacto

```bash
python3 tests/evolve.py skill/SKILL.md \
  --impact tests/fixtures/impact_example.json \
  --origin skill-suggested
```

| Flag | Descrição |
|------|-----------|
| `--dry-run` | Valida sem aplicar decisão |
| `--feedback <path>` | Arquivo JSONL com dados de adoção |
| `--impact <path>` | Arquivo JSON com prova de impacto real |
| `--origin <valor>` | `manual` (default) ou `skill-suggested` |

### Exit codes

| Código | Significado |
|--------|-------------|
| `0` | **ACCEPT** — mudanças mantidas |
| `1` | **REJECT** — backup restaurado |
| `2` | **HOLD** — mantido, marcado para revisão |

---

## 4. trend_report.py — Análise de tendência

### Uso básico

```bash
python3 tests/trend_report.py
```

### Com log customizado

```bash
python3 tests/trend_report.py --log path/to/outro_log.jsonl
```

### Exemplo de output

```
┌──────────────────────────────────────────────────────────┐
│  TREND REPORT                                            │
│  algorithm v0.1.0  |  window 5                           │
│──────────────────────────────────────────────────────────│
│  status      📈  improving                                │
│  signal      🟢  safe                                     │
│  confidence  🟠  medium  (10 measured sessions)           │
│──────────────────────────────────────────────────────────│
│  mean composite   +55%                                   │
│  sigma (σ)        0.03                                   │
│  slope            +0.0300/session                        │
│──────────────────────────────────────────────────────────│
│  absorbed    ✅  yes                                      │
│  10/10 sessions within ±15pp of median (55%).            │
└──────────────────────────────────────────────────────────┘
```

### Campos do relatório

| Campo | Significado |
|-------|-------------|
| **status** | `improving` / `stable` / `regressing` / `intermittent` / `insufficient_data` |
| **signal** | `🟢 safe` / `🟡 caution` / `🔴 at_risk` — sinal acionável |
| **confidence** | `none` / `low` / `medium` / `high` — confiança baseada em volume + variância |
| **mean composite** | Média do impacto composto nas últimas 5 sessões |
| **sigma (σ)** | Desvio padrão — quanto menor, mais estável |
| **slope** | Inclinação por sessão — positivo = melhorando |
| **absorbed** | `yes` se o comportamento é consistente (8/10 sessões dentro de ±15pp da mediana) |

---

## 5. metrics_report.py — Métricas agregadas

### Uso básico

```bash
python3 tests/metrics_report.py
```

### Com log customizado

```bash
python3 tests/metrics_report.py --log path/to/outro_log.jsonl
```

### Exemplo de output

```
┌──────────────────────────────────────────────────────────┐
│  METRICS REPORT                                          │
│  v0.1.0                                                  │
│──────────────────────────────────────────────────────────│
│  sessions                                                │
│    total              6                                  │
│    measured            4                                 │
│    unmeasured          2                                 │
│    adoption rate       67%                               │
│──────────────────────────────────────────────────────────│
│  verdict distribution                                    │
│    ACCEPT               4                                │
│    HOLD                 2                                │
│──────────────────────────────────────────────────────────│
│  impact distribution (measured sessions)                 │
│    mean               +70.4%                             │
│    median             +70.0%                             │
│    min                +70.0%                             │
│    max                +71.7%                             │
│    p25                +70.0%                             │
│    p75                +70.4%                             │
│    positive rate       100%                              │
│──────────────────────────────────────────────────────────│
│  stability    ⏳  insufficient_data                       │
│  absorbed     ❌  no                                      │
│  hidden regressions   0/3 metrics  ✅                     │
│──────────────────────────────────────────────────────────│
│  confidence distribution                                 │
│    ⚪  none         1                                     │
│  signal distribution                                     │
│    🟡  caution      1                                     │
└──────────────────────────────────────────────────────────┘
```

### Campos do relatório

| Campo | Significado |
|-------|-------------|
| **adoption rate** | % de sessões com impacto medido vs total |
| **mean / median** | Impacto composto médio e mediano (%) |
| **p25 / p75** | Percentis — mostra a dispersão do impacto |
| **positive rate** | % de sessões medidas com impacto > 0 |
| **stability** | Status da tendência (delegado ao `trend_analysis`) |
| **absorbed** | Comportamento consistente? |
| **hidden regressions** | Quantas das 3 métricas (rework, efficiency, output) declinam silenciosamente |
| **confidence / signal** | Distribuição de confiança e sinal acionável |

---

## 6. Interpretação das métricas

### Decision signal — o que fazer?

| Signal | Ícone | Significado | Ação recomendada |
|--------|-------|-------------|------------------|
| `safe` | 🟢 | Nenhum sinal preocupante | Continuar normalmente |
| `caution` | 🟡 | Instabilidade ou dados insuficientes | Monitorar; evitar mudanças grandes |
| `at_risk` | 🔴 | Hidden regressions ou declínio ativo | **Investigar imediatamente** |

### Confidence — posso confiar?

| Tier | Ícone | Critério |
|------|-------|----------|
| `none` | ⚪ | < 5 sessões medidas |
| `low` | 🟡 | < 10 sessões ou σ > 0.25 |
| `medium` | 🟠 | 10+ sessões, σ razoável |
| `high` | 🟢 | 20+ sessões, σ < 0.10, sem hidden regressions |

> **Regra**: hidden regressions **sempre** limitam a confidence a no máximo `medium`.

### Sigma (σ) — estabilidade do impacto

| Faixa | Interpretação |
|-------|---------------|
| σ < 0.10 | Estável — comportamento absorvido |
| 0.10 ≤ σ ≤ 0.25 | Normal — variação aceitável |
| σ > 0.25 | Intermitente — a skill não tem efeito consistente |

### Slope — direção

| Faixa | Status |
|-------|--------|
| slope > +0.02 | `improving` — tendência positiva |
| -0.02 ≤ slope ≤ +0.02 | `stable` — platô |
| slope < -0.02 | `regressing` — **atenção** |

---

## 7. Sinais de risco

### 🔴 Hidden Regressions (CRITICAL)

Uma métrica individual (rework, efficiency ou output) está **declinando silenciosamente** enquanto o composto parece estável. O composto mascara o problema porque outra métrica compensa.

**Severidade**:

| Nível | Métricas afetadas | Interpretação |
|-------|-------------------|---------------|
| `minor` | 1/3 | Problema localizado |
| `moderate` | 2/3 | Padrão se espalhando |
| `critical` | 3/3 | Falha sistêmica — o composto está errado |

**O que fazer**: Rodar `trend_report.py` para ver qual métrica declina, investigar as últimas sessões e considerar rollback.

### 🟡 Intermittent (caution)

A skill funciona em algumas sessões e não em outras (σ > 0.25). Impacto não é reproduzível.

**O que fazer**: Coletar mais sessões, verificar se o contexto de uso varia muito.

### 📉 Regressing (at_risk)

O slope do composto é negativo (< -0.02). O impacto da skill está **diminuindo** ao longo do tempo.

**O que fazer**: Comparar sessões recentes com antigas, verificar se algo mudou no ambiente.

### ⏳ Insufficient Data (caution)

Menos de 5 sessões medidas. Todas as conclusões são preliminares.

**O que fazer**: Continuar usando a skill e medindo impacto até atingir 5+ sessões.

### ❌ Not Absorbed

Menos de 8/10 sessões recentes dentro de ±15pp da mediana. O comportamento ainda não estabilizou.

**O que fazer**: Normal nas primeiras sessões. Se persistir após 15+ sessões, a skill pode ser inconsistente.

---

## 8. Workflow recomendado

```
┌─────────────────────────────────────────┐
│  1. Editar skill/SKILL.md               │
│                                         │
│  2. python3 tests/evolve.py             │
│     skill/SKILL.md --dry-run            │
│     → Verificar se passa nos testes     │
│                                         │
│  3. python3 tests/evolve.py             │
│     skill/SKILL.md --origin manual      │
│     → Aplicar decisão (ACCEPT/REJECT)   │
│                                         │
│  4. python3 tests/trend_report.py       │
│     → Verificar tendência temporal      │
│                                         │
│  5. python3 tests/metrics_report.py     │
│     → Revisar métricas agregadas        │
│                                         │
│  6. Se signal = 🔴 at_risk:             │
│     → Investigar hidden regressions     │
│     → Considerar rollback               │
│                                         │
│  7. Se signal = 🟡 caution:             │
│     → Monitorar, coletar mais dados     │
│                                         │
│  8. Se signal = 🟢 safe:               │
│     → Próxima iteração                  │
└─────────────────────────────────────────┘
```

### Ciclo rápido de diagnóstico

```bash
# Tudo de uma vez
python3 tests/evolve.py skill/SKILL.md --dry-run && \
python3 tests/trend_report.py && \
python3 tests/metrics_report.py
```

---

## 9. Referência de exit codes

| CLI | Código | Significado |
|-----|--------|-------------|
| `evolve.py` | 0 | ACCEPT |
| `evolve.py` | 1 | REJECT |
| `evolve.py` | 2 | HOLD |
| `trend_report.py` | 0 | Sucesso |
| `trend_report.py` | 1 | Erro (log não encontrado ou vazio) |
| `metrics_report.py` | 0 | Sucesso |
| `metrics_report.py` | 1 | Erro (log não encontrado ou vazio) |

---

## Suíte de testes

```bash
# Rodar todos os testes
python3 tests/test_metrics_report.py   # 10 testes (MR-01 a MR-10)
python3 tests/test_trend_report.py     # 8 testes  (TR-01 a TR-08)
python3 tests/test_trend_analysis.py   # 5 testes  (TA-01 a TA-05)
python3 tests/test_session_result.py   # 6 testes  (SR-01 a SR-06)
python3 tests/run_tests.py            # 18 testes (validation)
```

**Total: 47 testes** — todos passando na data desta release.

---

*orbit-engine · Phase 15 · MIT License*
