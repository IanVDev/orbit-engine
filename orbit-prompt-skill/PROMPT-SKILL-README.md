# Orbit Prompt Skill — Exportação para Empresa

## 📦 O que você recebeu

**Arquivo:** `orbit-prompt.skill` (v1.1.0)

Um prompt universal que detecta 8 padrões de ineficiência em execução de tarefas.
**NOVO:** Inclui comando `/prompt` para melhorar prompts antes de enviar.

## ⚡ Começar em 2 minutos

### Opção 1: Usar como arquivo skill

Se sua empresa usa Claude Code:

```bash
# 1. Distribua o arquivo orbit-prompt.skill aos usuários
# 2. Instale na interface local de skills
# 3. O prompt ativa automaticamente em sessões com padrões detectados
# 4. Use /prompt para melhorar seus prompts antes de enviar
```

### Opção 2: Usar como prompt direto

Em qualquer LLM:

```bash
# 1. Extraia o conteúdo
unzip orbit-prompt.skill

# 2. Use SKILL.md como prompt de sistema
# 3. Pronto para usar em ChatGPT, Claude, ou qualquer IA
# 4. Você pode simular /prompt manualmente
```

### Opção 3: Integrar em processos

Para code review, automação, ou análise:

```bash
# 1. Extraia SKILL.md
# 2. Incorpore em seus bots/ferramentas
# 3. Use a seção "PROMPT IMPROVEMENT output" para melhorar prompts automaticamente
# 4. Customize conforme necessário
```

## 📚 Documentação incluída

Dentro do arquivo `orbit-prompt.skill` (ZIP):

- **SKILL.md** — Definição completa do prompt + comando `/prompt`
- **ONBOARDING.md** — Orientação para novos usuários
- **QUICK-START.md** — Guia de primeiros passos com `/prompt` (3 min)
- **EXAMPLES.md** — 6 cenários reais com diagnóstico E melhoria de prompt

## 🆕 Comando `/prompt`

**Novo na v1.1.0**

Use para melhorar seu prompt ANTES de enviar:

```bash
/prompt "Sua tarefa vaga aqui"
```

O prompt retorna:
- Análise do que está faltando
- Versão melhorada com constraints claros
- Explicação de cada melhoria
- Avaliação se está pronto para enviar

**Benefício:** 2 minutos para melhorar = 30 minutos economizados em rework

---

## 🎯 Os 8 padrões

O prompt detecta:

1. **Respostas muito longas** — Output > requisição
2. **Cadeias de correção** — Múltiplas follow-ups corrigindo
3. **Edições repetidas** — Mesmo arquivo editado 3+ vezes
4. **Exploração sem plano** — Leitura de muitos arquivos sem direção
5. **Prompts fracos** — Tarefas complexas sem constraints
6. **Conteúdo colado** — Código inline em vez de referência
7. **Teatro de validação** — Artefatos criados mas não executados
8. **Acúmulo de contexto** — Sessões longas com contexto irrelevante

## 🚀 Casos de uso imediatos

| Caso | Como usar |
|------|-----------|
| Code Review | Aplique os padrões como critério de eficiência |
| Pair Programming | Use em sessões com IA para melhorar foco |
| Melhorar prompts | Use `/prompt [tarefa]` antes de enviar |
| Requisitos complexos | Use `/prompt` para deixar claro e constrained |
| Mentoría | Ensine os 8 padrões + `/prompt` no onboarding |
| Automação | Integre em CI/CD para análise de PRs e prompts |
| Treinamento | Use exemplos em retrospectivas |

## ✅ Características

- ✓ Agnóstico de plataforma (funciona em qualquer LLM)
- ✓ Sem dependências externas
- ✓ Customizável (é um ZIP com markdown)
- ✓ Sem especulação (padrões observáveis apenas)
- ✓ Acionável (diagnósticos com ações específicas)
- ✓ Silencioso em sucesso (zero ruído)
- ✓ **Melhora prompts** (novo com `/prompt`)

## 🔄 Como customizar

```bash
# 1. Extraia
unzip orbit-prompt.skill

# 2. Edite SKILL.md (adicione seus padrões corporativos)

# 3. Recompacte
zip skill-empresa.skill *.md

# 4. Distribua a versão customizada
```

## 📞 Próximos passos

1. **Distribuição:** Compartilhe `orbit-prompt.skill` com seu time
2. **Onboarding:** Execute `QUICK-START.md` com os usuários
3. **Aplicação:** Use `/prompt` em tarefas complexas
4. **Feedback:** Coletar input sobre integração

## 📊 Adoção esperada

Após 1 mês:
- ⬇️ Menos correções em code review
- ⬇️ Menos rework de files
- ⬆️ Prompts mais claros e constrained
- ⬆️ Melhor qualidade de sessão

## 🎓 Exemplo com `/prompt`

**Fluxo tradicional (sem melhoria):**
```
Tarefa vaga: "Refactor auth module"
↓
Resultado: 3 files rewritten, schema changed, routes modified
↓
Feedback: "No, just extract middleware"
↓
Rework necessário, tokens gastos
```

**Fluxo com `/prompt`:**
```
Tarefa vaga: "Refactor auth module"
↓
/prompt "Refactor auth module"
↓
Tarefa melhorada: "Extract middleware from auth.ts to middleware/auth.ts,
keep function signatures, don't touch routes or schema.
Success = all tests pass."
↓
Enviado para Claude
↓
Resultado: Exatamente o que foi pedido
↓
Feedback: Nenhum (pronto na primeira vez)
```

**Tempo economizado:** 2 min com `/prompt` = 30 min de rework evitado

---

**Pronto para usar!** Comece com `QUICK-START.md` dentro do arquivo `.skill`.
