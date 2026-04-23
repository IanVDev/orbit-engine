# Orbit Prompt Skill

## 📦 O que você recebeu

**Arquivo:** `orbit-prompt.skill`

Um prompt universal que detecta 8 padrões de ineficiência em execução de tarefas.

## ⚡ Começar em 2 minutos

### Opção 1: Usar como arquivo skill

Se sua empresa usa Claude Code:

```bash
# 1. Distribua o arquivo orbit-prompt.skill aos usuários
# 2. Instale na interface local de skills
# 3. O prompt ativa automaticamente em sessões com padrões detectados
```

### Opção 2: Usar como prompt direto

Em qualquer LLM:

```bash
# 1. Extraia o conteúdo
unzip orbit-prompt.skill

# 2. Use SKILL.md como prompt de sistema
# 3. Pronto para usar em ChatGPT, Claude, ou qualquer IA
```

### Opção 3: Integrar em processos

Para code review, automação, ou análise:

```bash
# 1. Extraia SKILL.md
# 2. Incorpore em seus bots/ferramentas
# 3. Customize conforme necessário
```

## 📚 Documentação incluída

Dentro do arquivo `orbit-prompt.skill` (ZIP):

- **SKILL.md** — Definição completa do prompt
- **ONBOARDING.md** — Orientação para novos usuários
- **QUICK-START.md** — Guia de primeiros passos (3 min)
- **EXAMPLES.md** — 5 cenários reais com saída esperada

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
| Mentoría | Ensine os 8 padrões no onboarding |
| Automação | Integre em CI/CD para análise de PRs |
| Treinamento | Use exemplos em retrospectivas |

## 📋 Guias de referência

**Quer mais detalhes?**

- `EXPORT-PROMPT-SKILL.md` — Uso técnico e integração
- `DISTRIBUTION.md` — Guia de adoção corporativa
- `PROMPT-SKILL-README.md` — Este arquivo

## ✅ Características

- ✓ Sem dependências externas
- ✓ Agnóstico de plataforma (funciona em qualquer LLM)
- ✓ Customizável (é um ZIP com markdown)
- ✓ Sem especulação (apenas padrões observáveis)
- ✓ Acionável (cada diagnóstico inclui ações específicas)
- ✓ Silencioso em sucesso (não gera ruído)

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
3. **Aplicação:** Use os padrões em code review durante 1 semana
4. **Feedback:** Ajuste conforme necessário

## 📊 Adoção esperada

Após 1 mês:
- ⬇️ Menos correções em code review
- ⬇️ Menos rework de files
- ⬆️ Prompts mais claros e constrained
- ⬆️ Melhor qualidade de sessão

## 🎓 Exemplo rápido

**Antes (sem o prompt):**
- Tarefa: "Refactor auth module"
- Resultado: 3 files rewritten, schema changed, routes modified
- Feedback: "No, just extract middleware"
- Resultado: Rework necessário, tokens gastos

**Depois (com o prompt):**
- Tarefa: "Extract auth middleware from auth.ts into middleware/auth.ts, don't touch routes or schema"
- Resultado: Exatamente o que foi pedido
- Feedback: Nenhum

---

**Pronto para usar!** Comece com `QUICK-START.md` dentro do arquivo `.skill`.
