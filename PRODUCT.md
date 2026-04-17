# ORBIT — Product Definition

> **Orbit te deixa responder, dias depois, "a IA mexeu aqui?" — com prova.**

---

## Quando Orbit vira indispensável

Terça-feira, 15h. Quebrou algo em produção.

O time bisseciona e chega num PR de quinta passada. Alguém pergunta: *"esse PR foi escrito com ajuda de IA?"*

Sem Orbit, a resposta é memória — às vezes certa, às vezes não. Em auditoria, compliance ou post-mortem sério, memória não basta.

Com Orbit, a resposta é um comando:

```bash
orbit_explain.sh --list --since 2026-04-09T00:00:00Z
```

Isso mostra todas as sessões de IA que rodaram naquela janela, em qual repositório, e qual era o estado do código quando aconteceram. Para cada sessão suspeita:

```bash
orbit_explain.sh <session_id>
```

Entrega o HEAD ao iniciar, o HEAD ao encerrar, e os comandos git exatos para ver o que mudou durante ela. Não adivinha. Não interpreta. Apenas te coloca no lugar certo com a evidência na mão.

---

## O que Orbit faz, em uma frase

**Cada vez que você ativa uma skill de IA no seu código, Orbit grava quando, onde no repo e com qual versão do código isso aconteceu — e te dá os comandos para investigar depois.**

Isso é tudo. Não tenta ser mais do que isso.

---

## Para quem é

- **Engenheiros** que precisam reconstruir a história de uma mudança depois que a memória já se foi.
- **Times com política de uso de IA** (finanças, saúde, setores regulados) onde "a IA escreveu isso" precisa ser auditável, não opinião.
- **Incident responders** que fazem post-mortem e precisam correlacionar atividade de IA com o estado do repositório.
- **Fundadores/tech leads** que querem saber como a IA está sendo usada no time sem depender do dashboard do provedor.

Não é para: quem quer economizar tokens, quem quer que a IA seja mais rápida, quem quer dashboards bonitos. Use outra ferramenta.

---

## Como funciona (em 3 passos)

### 1. A skill registra

Toda vez que a skill `orbit-engine` é acionada, ela grava um evento em `~/.orbit/client_ledger.jsonl`. Cada evento carrega:

- quando aconteceu
- qual sessão
- qual repositório
- qual commit estava no HEAD naquele momento
- um hash que permite qualquer um recomputar e checar se a entrada foi mexida

Se a gravação falha, a skill aborta. Não existe "registrou parcial".

### 2. Você pergunta

```bash
orbit_explain.sh --list                              # todas as sessões
orbit_explain.sh --list --since 2026-04-17T00:00:00Z # só as de hoje
orbit_explain.sh <session_id>                        # detalhe de uma
```

A saída de uma sessão começa pelo que investigador quer primeiro:

- janela temporal da sessão
- integridade dos eventos (OK / divergência)
- repositório e HEAD inicial/final
- **o comando git exato para ver o que mudou**

### 3. Você investiga com git

Orbit não tenta te dizer o que a IA fez. Te entrega o `git log --oneline <first>..<last>` pronto. Você roda, você olha, você conclui.

Essa separação é deliberada: Orbit é evidência, `git` é verdade sobre o código, você é quem interpreta. Cada camada faz uma coisa.

---

## O que Orbit não faz (e por quê)

| Não faz | Por quê |
|---|---|
| Dizer "a IA causou este bug" | Correlação temporal ≠ causalidade. Interpretação é humana. |
| Contar tokens reais | Registra estimativa informada; reconciliação é passo separado. |
| Ler seu código ou prompts | Trabalha em metadados. Conteúdo fica na sua máquina. |
| Ser prova soberana | O ledger é um arquivo seu, mutável. Prova externa requer ancoragem (contrato `orbit anchor → AURYA`, ainda não implementado). |
| Recomendar ações | Não é recomendador nem linter. Só registra e correlaciona. |

Cada "não" é fronteira arquitetural. A honestidade sobre o que não prova é feature, não defeito.

---

## O que hoje é verificável (com comando concreto)

| Afirmação | Como verificar |
|---|---|
| Ledger é à prova de adulteração | `bash tests/test_orbit_explain_detects_tampering.sh` — 7 casos |
| Correlação git é honesta | `bash tests/test_orbit_explain_git_correlation.sh` — 3 casos |
| UX de investigação funciona | `bash tests/test_orbit_explain_ux_flags.sh` — 7 casos |
| Integridade é recomputável | rode `orbit_explain.sh <sid>` duas vezes — output idêntico |
| Fail-closed em todo ponto | backend desligado → skill aborta com diagnóstico e sai != 0 |

---

## Estado: Orbit v0 — Evidence Engine para forensics

Funciona hoje para:

- investigar uma sessão específica depois do fato
- listar sessões em janela temporal
- correlacionar com HEAD do repositório
- provar integridade do ledger local

Ainda não:

- ancora em sistema externo imutável (contrato definido em `docs/ORBIT_ANCHOR_CONTRACT.md`, implementação aguarda AURYA)
- tem UI de time / dashboard agregado
- detecta mudanças de working tree (só HEAD commitado)

---

## Uma linha

**Quando alguém perguntar "a IA estava nisso?", Orbit te entrega a resposta com os comandos git para confirmar.**
