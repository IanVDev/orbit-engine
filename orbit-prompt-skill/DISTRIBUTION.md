# Distribuição da Skill de Prompt — Guia Corporativo

## Resumo executivo

Arquivo: `orbit-prompt.skill` (4.3 KB)

Uma skill compacta e agnóstica de plataforma que detecta padrões de ineficiência em execução de tarefas.

## Para distribuir internamente

### 1. Documentação mínima

Compartilhe os seguintes arquivos:
- `orbit-prompt.skill` — O pacote da skill
- `EXPORT-PROMPT-SKILL.md` — Guia de uso

### 2. Formas de distribuição

**Via repositório corporativo:**
```bash
# Colocar em /company/skills/orbit-prompt/
orbit-prompt.skill
EXPORT-PROMPT-SKILL.md
```

**Via documentação interna:**
1. Link para download do arquivo `.skill`
2. Instruções de descompactação
3. Exemplos de aplicação

**Via integração direta:**
1. Extraia o conteúdo do `SKILL.md`
2. Use como prompt de sistema em suas ferramentas
3. Customize conforme necessário

### 3. Casos de uso recomendados

#### Para Code Review
- Adicione como critério de eficiência nas reviews
- Use os padrões como feedback padronizado
- Normalize o diagnóstico entre times

#### Para Pair Programming
- Integre em sessões de pair com IA
- Use para detect rework e correções
- Melhore foco e escopo

#### Para Mentoría
- Ensine os 8 padrões em onboarding
- Use exemplos em retrospectivas
- Cite em feedback de desempenho

#### Para Automação
- Integre em CI/CD para análise de PRs
- Crie alertas para padrões críticos
- Meça tendências ao longo do tempo

## Customização para sua empresa

O arquivo é um ZIP padrão. Para customizar:

```bash
# 1. Extraia
unzip orbit-prompt.skill -d meu-prompt

# 2. Edite SKILL.md com seus padrões corporativos
# (adicione regras, remova as desnecessárias, etc)

# 3. Recompacte
cd meu-prompt
zip -q meu-prompt-customizado.skill *.md

# 4. Distribua
```

### Exemplos de customização

**Para times de data:**
- Adicione padrões sobre exploração sem análise
- Customize exemplos para pipelines de dados
- Focus em validation theater (modelos não testados)

**Para times de infraestrutura:**
- Aumente sensibilidade para contexto acumulado
- Customize exemplos para deployment
- Focus em rework de configs

**Para times de produto:**
- Customize para requisitos de produto
- Adicione padrões sobre feature creep
- Focus em scope management

## Integração com processos

### Antes do desenvolvimento
- Compartilhe a skill com o time
- Explique os 8 padrões em uma sessão de 30 min
- Use como baseline para expectativa de qualidade

### Durante o desenvolvimento
- Referencie padrões em code review
- Use em pair programming sessions
- Aplique em retrospectivas

### Após completion
- Analise PRs usando os padrões
- Meça quantas correções foram necessárias
- Identifique padrões recorrentes por time

## Métricas de eficácia

Após 1 mês de adoção, observe:

- **Redução em correções:** Menos follow-ups em code review
- **Melhoria em escopo:** Prompts mais claros e constrained
- **Menos rework:** Edições repetidas ao mesmo arquivo
- **Contexto mais limpo:** Melhor foco em tarefas grandes

## Suporte interno

**Perguntas frequentes:**

*P: Posso modificar a skill?*
R: Sim. Extraia, edite SKILL.md, recompacte com `zip`.

*P: Funciona em qualquer LLM?*
R: Sim, o formato é agnóstico. Use o conteúdo como prompt em qualquer IA.

*P: Preciso de permissões especiais?*
R: Não. É um arquivo ZIP com markdown. Nenhuma dependência externa.

*P: Como integro em nossa CI/CD?*
R: Extraia o `SKILL.md` e use em linters customizados ou bots de review.

## Roadmap de adoção

**Semana 1:** Distribua, explique os padrões, compartilhe exemplos
**Semana 2-3:** Use em code review e retrospectivas
**Mês 1:** Meça adoção e feedback inicial
**Mês 2:** Customize para especialidades do time
**Mês 3+:** Integre com ferramentas internas, meça tendências

## Recursos

- Arquivo: `orbit-prompt.skill` (4.3 KB)
- Documentação: `EXPORT-PROMPT-SKILL.md`
- Exemplos: Extraia `EXAMPLES.md` do arquivo `.skill`

---

**Questões?** Consulte `EXPORT-PROMPT-SKILL.md` para detalhes técnicos.
