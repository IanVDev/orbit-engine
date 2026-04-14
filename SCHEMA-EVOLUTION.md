# SessionResultSchema — Plano de Evolução

**Versão atual:** v1.0  
**Data de vigência:** 2026-04-14  
**Escopo:** `SessionResultSchema`, `build_session_result()`, `format_session_result()`, `to_dict()`

---

## 1. Quando o v1 deve ser alterado

Três categorias de gatilho. Cada uma tem critério objetivo.

### 1.1 Gatilho estrutural (obrigatório evoluir)

Condição em que manter o v1 intacto causaria erro silencioso ou dado incorreto.

| Evento | Critério objetivo | Ação |
| --- | --- | --- |
| Novo canal no Gate 2 | `compute_real_impact()` retorna métrica não presente em `breakdown` | Incremento de minor (v1.1) |
| Nova origem válida | `--origin` aceita valor que `build_session_result()` não trata → `causality_message` incorreta | Incremento de minor (v1.1) |
| Mudança de peso | `_W_REWORK / _W_EFFICIENCY / _W_OUTPUT` alterados → `composite_formula` e `composite_weights` ficam stale | Incremento de minor (v1.1) |
| Mudança de semântica de veredicto | `Verdict` ganha membro novo → `verdict` str não cobre o caso | Incremento de minor (v1.1) |
| Remoção de campo existente | Qualquer campo dos 12 removido ou renomeado | Major (v2.0) — breaking |

### 1.2 Gatilho de produto (avaliar antes de evoluir)

Condição em que o v1 continua correto, mas limita uma funcionalidade nova.

- PRO quiser expor histórico de impacto por categoria ao longo de múltiplas sessões.
- Análise de tendência precisar de campo `session_date` no schema (ausente no v1).
- Comparação manual vs skill precisar de campo `baseline_origin` para saber a origem da sessão anterior.

Esses gatilhos **não justificam v2 imediatamente**. Justificam adicionar campos opcionais com `None` como default — incremento minor.

### 1.3 Gatilho de qualidade (não evoluir schema, corrigir implementação)

- Um campo existe mas é preenchido incorretamente (`tradeoff_detected` falso positivo).
- `composite_formula` string tem erro de formatação.
- `to_dict()` serializa tipo incorreto.

Esses são bugs no `build_session_result()` ou `format_session_result()` — **não justificam mudança de schema**. Corrigir a implementação. Os testes SR-01 a SR-06 devem detectar esses casos antes do merge.

---

## 2. Regras de versionamento

O `schema_version` no `SessionResultSchema` segue **semver restrito** (major.minor):

```text
major: qualquer mudança que quebra leitores do evidence_log existente
minor: adição de campo opcional, nenhum campo removido, nenhum campo renomeado
```

Não existe patch. Bug em campo → corrigir implementação, `schema_version` não muda.

### O que constitui breaking change (major)

1. Campo removido de `to_dict()`.
2. Campo renomeado (`impact_percent` → `composite_percent`).
3. Tipo de campo alterado (`float | None` → `str`).
4. Semântica de valor alterada ("`positive`" passa a significar outra coisa).
5. `composite_weights` deixa de ser `{"rework", "efficiency", "output"}`.

### O que não é breaking change (minor)

1. Campo novo adicionado com default `None` ou valor estável.
2. `causality_message` ganha novos templates internos (o campo continua sendo `str | None`).
3. `tradeoff_metrics` passa a incluir mais métricas (continua sendo `list[str]`).
4. `composite_formula` string muda de formato visual (não é contrato de parsing).
5. `impact_status` ganha novo valor possível além dos 4 existentes.

---

## 3. Processo de mudança

### Minor (v1.x)

```text
1. Abrir issue com: campo afetado, motivação, valor default proposto
2. Atualizar SessionResultSchema com novo campo (default=None ou valor estável)
3. Atualizar build_session_result() para preencher o campo
4. Atualizar to_dict() para incluir o campo no payload
5. Atualizar schema_version de "1.0" para "1.x"
6. Adicionar teste SR-0N cobrindo o novo campo
7. Verificar que testes SR-01..SR-06 continuam passando sem modificação
   → se precisar modificar teste existente, a mudança é breaking, não minor
```

Regra crítica do passo 7: **testes de contrato v1 não devem ser alterados para acomodar v1.x.** Se precisar, a mudança é major.

### Major (v2.0)

```text
1. Criar SessionResultSchemaV2 como nova classe (não modificar a existente)
2. Manter SessionResultSchema (v1) sem alteração durante o período de transição
3. Implementar migração explícita: migrate_v1_to_v2(entry: dict) -> dict
4. Definir data de deprecação do v1 (mínimo 90 dias após v2 disponível)
5. Testes v1 continuam passando durante todo o período de transição
6. Na data de deprecação: remover SessionResultSchema, mover testes v1 para arquivo de arquivo
```

---

## 4. Migração de dados no evidence_log

### Estrutura atual (v1)

Cada entrada no `evidence_log.jsonl` é uma linha JSON com o bloco `"session_result"` gerado por `to_dict()`:

```json
{
  "timestamp": "...",
  "verdict": "ACCEPT",
  "impact": { ... },
  "session_result": {
    "schema_version": "1.0",
    "origin": "skill-suggested",
    "verdict": "ACCEPT",
    "impact_percent": 0.70,
    "impact_status": "positive",
    "measured": true,
    "breakdown": { "rework": 0.76, "efficiency": 0.59, "output": 0.70 },
    "composite_weights": { "rework": 0.50, "efficiency": 0.30, "output": 0.20 },
    "composite_formula": "...",
    "tradeoff_detected": false,
    "tradeoff_metrics": [],
    "causality_message": "..."
  }
}
```

### Abordagem de migração

O `evidence_log.jsonl` é append-only. Entradas antigas nunca são reescritas.

Leitores do log devem usar `schema_version` para determinar o formato:

```python
def read_session_result(entry: dict) -> dict:
    sr = entry.get("session_result", {})
    version = sr.get("schema_version", "1.0")
    if version == "1.0":
        return sr  # leitura direta
    elif version == "2.0":
        return migrate_v1_to_v2(sr)
    else:
        raise ValueError(f"schema_version desconhecida: {version}")
```

### Regras de migração

1. **Nunca reescrever entradas antigas.** O log é auditável. Modificar entradas retroativamente destrói a rastreabilidade.
2. **`migrate_v1_to_v2()` é somente-leitura.** Ela produz um objeto v2 a partir de um objeto v1 sem tocar no arquivo.
3. **Campos ausentes no v1 recebem `None` no v2.** Nunca inventar valores retroativos.
4. **O campo `schema_version` é a chave de dispatch.** Sem ele, assumir `"1.0"` por compatibilidade.
5. **Análises de tendência sobre o histórico** devem normalizar pelo schema_version antes de agregar. Comparar `impact_percent` do v1 com `impact_percent` do v2 é válido somente se o campo tiver a mesma semântica nos dois — verificar nas notas de migração.

### Compatibilidade de runtime (regra permanente)

**Todo leitor deve suportar versões anteriores à sua.** Um leitor v2 lê v1 sem erro. Um leitor v3 lê v1 e v2 sem erro. A direção inversa não se aplica — um leitor v1 não precisa entender v2.

Consequência prática: `read_session_result()` é sempre atualizada junto com `SessionResultSchemaV_N`. Nunca há versão do leitor sem o handler da versão anterior.

Se um leitor encontra `schema_version` desconhecida (ex: `"3.0"` num leitor v2), o comportamento correto é **retornar o dict bruto sem transformação**, não lançar exceção. Exceção impede leitura de logs válidos em produção.

```python
def read_session_result(entry: dict) -> dict:
    sr = entry.get("session_result", {})
    version = sr.get("schema_version", "1.0")
    if version == "1.0":
        return sr
    elif version == "2.0":
        return migrate_v1_to_v2(sr)
    else:
        # versão futura desconhecida: retornar bruto, não travar
        return sr
```

### Idempotência de migrate_v1_to_v2()

A função de migração deve ser idempotente: aplicá-la duas vezes sobre o mesmo input produz o mesmo resultado que aplicá-la uma vez.

Requisitos para garantir isso:

1. A função nunca modifica o dict de entrada — sempre retorna um dict novo.
2. Se o input já tiver `schema_version == "2.0"`, retornar sem transformação.
3. Campos com `None` no v1 permanecem `None` no v2 — não substituir por valor padrão.
4. Nenhum campo é calculado retroativamente a partir de outros campos.

Estrutura mínima obrigatória:

```python
def migrate_v1_to_v2(sr: dict) -> dict:
    # idempotência: já é v2
    if sr.get("schema_version") == "2.0":
        return sr
    result = dict(sr)               # cópia, não mutação
    result["schema_version"] = "2.0"
    result["session_date"] = None   # campo novo, ausente no v1
    # ... demais campos novos com None
    return result
```

### Teste de compatibilidade entre versões

Junto com cada major, adicionar um teste de compatibilidade cruzada:

```python
def test_reader_handles_v1_entry_from_v2_reader():
    """Leitor v2 deve ler entrada v1 sem erro e sem perda de dados."""
    v1_entry = {"schema_version": "1.0", "verdict": "ACCEPT", "impact_percent": 0.70, ...}
    result = read_session_result({"session_result": v1_entry})
    assert result["verdict"] == "ACCEPT"
    assert result["impact_percent"] == 0.70

def test_migrate_is_idempotent():
    """migrate_v1_to_v2 aplicada duas vezes == aplicada uma vez."""
    v1 = {"schema_version": "1.0", "verdict": "HOLD", "impact_percent": None, ...}
    once = migrate_v1_to_v2(v1)
    twice = migrate_v1_to_v2(once)
    assert once == twice

def test_reader_does_not_raise_on_unknown_version():
    """Versão desconhecida retorna dict bruto, não lança exceção."""
    future_entry = {"schema_version": "9.9", "verdict": "ACCEPT"}
    result = read_session_result({"session_result": future_entry})
    assert result["verdict"] == "ACCEPT"
```

Esses testes entram em `tests/test_session_result.py` quando a v2 for implementada. Não criar antes — teste sem implementação é ruído.

### Quando escrever a função de migração

Apenas na hora em que o código precisar ler entradas v1 com um leitor v2. Não escrever migração antecipada.

---

## 5. Checklist antes de qualquer mudança no schema

```text
[ ] O gatilho é estrutural, de produto ou de qualidade?
    → Se qualidade: corrigir implementação, não schema.
[ ] A mudança adiciona ou remove campos?
    → Remoção/renomeação = major obrigatório.
[ ] Os testes SR-01..SR-06 passam sem modificação após a mudança?
    → Se não: a mudança é breaking, tratar como major.
[ ] schema_version foi atualizado?
[ ] to_dict() inclui o campo novo?
[ ] Há teste novo cobrindo o campo novo?
[ ] Se major: SessionResultSchemaV1 está preservada no código?
[ ] Se major: migrate_v1_to_v2() está implementada e testada?
[ ] Se major: teste de compatibilidade cruzada foi adicionado?
[ ] Se major: read_session_result() tem handler para a versão anterior?
```

---

## 6. O que o v2 provavelmente precisará

Sem comprometimento. Apenas o que os gatilhos atuais sugerem:

- `session_date: str | None` — necessário para análise de tendência PRO.
- `baseline_origin: str | None` — para comparação manual vs skill-suggested.
- `gate_scores: dict[str, float] | None` — scores por gate (validação, segurança) para contexto completo da decisão.
- `follow_up_sessions: int | None` — quantas sessões subsequentes seguiram a mesma recomendação.

Nenhum desses campos justifica v2 por si só. Todos podem entrar como minor se adicionados com `None` como default. Um v2.0 só se justifica se houver remoção ou renomeação de campo existente.
