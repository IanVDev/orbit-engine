# Orbit Prompt Skill

<!-- SKILL_VERSION: 1.2.0 -->

**Arquivo:** `orbit-prompt.skill` (v1.2.0)

Skill universal para Claude Code com duas superfícies bem separadas:

1. **Diagnóstico automático** — silencioso por padrão, só responde quando detecta desperdício
2. **Comando manual** — `/orbit-prompt` para melhorar prompts antes de enviar

---

## Usage

Só existe uma forma recomendada:

```text
/orbit-prompt "sua tarefa aqui"
```

Retorna o prompt original analisado, uma versão melhorada com constraints claros, e um veredito `READY TO SEND`.

É isso.

---

## Como as duas superfícies se separam

### Superfície 1 — Diagnóstico automático (silencioso)

A skill observa a sessão e só emite output quando detecta um dos 8 padrões de desperdício. Em sessão saudável, permanece silenciosa. Você não faz nada.

### Superfície 2 — `/orbit-prompt` (manual, explícito)

Você aciona. Ela responde. Sempre.

```text
/orbit-prompt "Refactor auth module"
```

Nunca dispara automaticamente — só com o comando explícito.

---

## Advanced triggers (opcional)

O modo diagnóstico também reage a frases em linguagem natural. Use se preferir disparar sob demanda em vez de esperar detecção automática:

- `analyze cost`
- `is this optimal?`
- `how efficient is this?`
- `optimize this`
- `Before answering, apply orbit-engine`

São opcionais. O fluxo recomendado continua sendo `/orbit-prompt` para melhoria de prompt, e detecção automática para diagnóstico.

---

## Os 8 padrões detectados

1. **Unsolicited long responses** — output excede requisição
2. **Correction chains** — múltiplas follow-ups corrigindo
3. **Repeated edits** — mesmo arquivo editado 3+ vezes
4. **Exploratory searching** — leitura de muitos arquivos sem direção
5. **Weak prompts** — tarefas complexas sem constraints
6. **Large inline content** — código colado em vez de referenciado
7. **Validation theater** — artefatos criados mas não executados
8. **Context accumulation** — sessões longas com contexto irrelevante

---

## Instalação

### Claude Code (recomendado)

1. Distribua `orbit-prompt.skill` ao time
2. Instale via interface de skills local
3. Use `/orbit-prompt` em qualquer sessão

### LLM genérico (ChatGPT, Claude API, etc.)

1. `unzip orbit-prompt.skill`
2. Use `SKILL.md` como prompt de sistema
3. Simule `/orbit-prompt` manualmente enviando o comando no início da mensagem

---

## Documentação incluída (dentro do ZIP)

- `SKILL.md` — contrato de comportamento (fonte da verdade)
- `ONBOARDING.md` — orientação para novos usuários
- `QUICK-START.md` — primeiros passos em 3 minutos
- `EXAMPLES.md` — 6 cenários reais

---

## Customização

```bash
unzip orbit-prompt.skill
# editar SKILL.md
zip skill-custom.skill *.md
```

---

## Exemplo

**Sem `/orbit-prompt`:**

```text
"Refactor auth module"
→ 3 arquivos reescritos, schema alterado, rotas modificadas
→ "Não, só extrair o middleware"
→ rework, tokens gastos
```

**Com `/orbit-prompt`:**

```text
/orbit-prompt "Refactor auth module"
→ "Extract middleware from auth.ts to middleware/auth.ts.
   Keep function signatures. Don't touch routes or schema.
   Success = all tests pass."
→ enviado, entregue, sem correções
```

2 minutos investidos = 30 minutos de rework evitados.

---

## Características

- Silencioso por padrão em diagnóstico (só fala quando detecta desperdício)
- Determinístico (sem scoring, sem especulação, sem números inventados)
- Agnóstico de plataforma (funciona em qualquer LLM)
- Sem dependências externas
- Customizável (é um ZIP com markdown)
