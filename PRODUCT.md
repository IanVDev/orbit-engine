# ORBIT — Product Definition

> **Orbit registra, prova e permite verificar qualquer ação executada por IA.**

---

## Posicionamento

Orbit é um **motor de evidência operacional** para workflows de codificação assistida por IA (Claude Code, GPT, Gemini). Não otimiza tokens, não reescreve prompts, não substitui o assistente. O que faz:

- **Registra** cada ativação da IA em um ledger append-only local, com hash reproduzível.
- **Correlaciona** cada evento com o estado do repositório (git HEAD) no instante da ativação.
- **Prova integridade** local via `orbit explain <session_id>` — recomputa hashes, valida schema, compara com backend vivo.
- **Delimita o próprio alcance**: diz explicitamente o que verificou e o que não verificou.
- **Prepara ancoragem externa** via contrato `orbit anchor → AURYA` (camada soberana, separada por design).

O moat não é economia. É **trilha auditável por construção** para decisões tomadas com IA.

---

## Para quem

- **Engenheiros** que precisam responder "foi a IA ou fui eu?" dias depois de uma mudança.
- **Times em compliance/auditoria** que precisam de evidência do que foi feito com assistência de IA.
- **Incident responders** que precisam correlacionar sessões de IA a estados do repositório em post-mortems.
- **Fundadores/PMs** que querem medir uso real de IA no time sem depender de dashboards agregados de um provedor.

Não é para: quem quer reduzir custo de tokens. Use outra coisa.

---

## O problema que resolve

Hoje, quando algo quebra em produção depois de uma sessão de IA, três perguntas ficam sem resposta:

1. **Houve sessão de IA na janela em que o bug foi introduzido?**
2. **Se houve, qual o estado do repositório durante essa sessão?**
3. **A sessão existe em algum registro imutável verificável por terceiros?**

Ferramentas existentes (dashboards de provedor, telemetria de IDE) respondem parcialmente a (1), não tocam (2), e nunca chegam em (3).

Orbit responde (1) e (2) hoje, com evidência reproduzível. Para (3) existe contrato definido, não implementação prematura — prova soberana mora na camada de ancoragem (AURYA), não dentro do Orbit.

---

## Como funciona

### 1. Registro (skill `orbit-engine`)

Cada ativação da skill dispara:

- POST `/track` ao backend HTTP local
- Append no ledger local (`~/.orbit/client_ledger.jsonl`) com:
  - `session_id`, `timestamp`, `event_type`, `mode`
  - `skill_event_hash` = sha256(session_id | timestamp | tokens)
  - `server_event_id` (receipt do backend)
  - `git_head` e `git_repo` (HEAD capturado no instante da ativação)

**Fail-closed:** backend inacessível → skill aborta com diagnóstico. Escrita do ledger falha → operação aborta. Não há "modo offline silencioso".

### 2. Verificação (`scripts/orbit_explain.sh`)

```
orbit_explain.sh <session_id>
```

Executa 3 fases verificáveis:

- **Fase 1 — ledger local**: filtra eventos pelo session_id, recomputa cada hash, compara com o armazenado. Diverge? Exit != 0.
- **Fase 2 — backend**: `/health` deve responder 200.
- **Fase 3 — contexto global**: consulta recording rule `orbit:activations_total:prod` no gateway. Valor é de contexto, não de auditoria da sessão.

Sempre imprime um bloco **ESCOPO DESTA VERIFICAÇÃO** separando VERIFICADO de NÃO VERIFICADO. A honestidade sobre limites é feature, não aviso.

`orbit_explain.sh --list` lista todas as sessões presentes no ledger com contagem, tokens totais e janela temporal.

### 3. Correlação com repositório

O bloco `ARTEFATO CORRELACIONADO (git)` mostra:

- `git_repo` da sessão
- HEAD inicial e final
- Se HEAD avançou, sugere `git -C <repo> log --oneline <first>..<last>` para inspeção externa

Orbit registra correlação temporal entre a sessão e o repositório. **Não infere causalidade**: o `git log` é quem mostra o que mudou. Essa separação é explícita no output.

### 4. Ancoragem (contrato, não implementação)

`docs/ORBIT_ANCHOR_CONTRACT.md` fixa o protocolo pelo qual `orbit anchor` publicará um compromisso imutável (merkle_root + batch_hash) em AURYA. Enquanto AURYA não expõe o endpoint real, o contrato vive em documento — não em código especulativo.

---

## O que é verificável hoje

Tudo que este README descreve tem arquivo executável ou estrutural que prova:

| Afirmação | Verificação |
|---|---|
| Ledger é append-only | `tests/test_orbit_explain_detects_tampering.sh` — 7 casos fail-closed |
| Integridade é reproduzível | cada linha do ledger carrega hash recomputável |
| Correlação git é honesta | `tests/test_orbit_explain_git_correlation.sh` — 3 casos incl. HEAD ausente |
| Fail-closed em todo ponto | diagnóstico em stderr, exit != 0 em qualquer divergência |
| Contrato anchor bem definido | `docs/ORBIT_ANCHOR_CONTRACT.md` — input, endpoint, verificação |

---

## O que Orbit NÃO faz (por design)

- **Não classifica intenção do usuário.** Não distingue "ativação útil" de "ativação desperdiçada".
- **Não conta tokens reais.** Registra `impact_estimated_tokens` informado pelo chamador. Reconciliação é via `/reconcile`, pós-execução.
- **Não intercepta conteúdo do prompt.** Trabalha em metadados.
- **Não é prova soberana.** O ledger local é mutável pelo usuário. Prova externa requer ancoragem em AURYA.
- **Não infere causalidade.** Orbit só correlaciona tempo e estado. Interpretação é humana.
- **Não sugere ações.** Não é sistema de recomendação nem de gating.

Cada um desses "não" é uma fronteira arquitetural, não ausência de feature.

---

## Estado de maturidade

Orbit v0 — **Evidence Engine validado**. Significa:

- Backend HTTP fail-closed, funcional, testado
- Skill client-side com ledger append-only
- `orbit_explain` com 3 fases verificáveis + correlação git
- Contrato de ancoragem definido
- 10+ cenários de adulteração cobertos por testes bash

Não significa:

- Pronto para produto pago
- Ancoragem externa disponível
- UI de time ou dashboard consolidado

Quem precisa de produto completo, este ainda não é o momento. Quem precisa de trilha auditável reproduzível em projeto próprio, já entrega.

---

## Uma linha

**Orbit registra toda ativação de IA no seu workflow com evidência reproduzível — e diz com clareza o que ainda não pode provar.**
