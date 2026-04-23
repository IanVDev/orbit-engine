# Orbit Prompt Skill — Exportação

**Arquivo:** `orbit-prompt.skill`

## O que é

A skill de prompt universal da Orbit Engine, empacotada para distribuição e uso em ambientes corporativos.

## Conteúdo

- `SKILL.md` — Definição completa do prompt e regras de ativação
- `ONBOARDING.md` — Orientação inicial para novos usuários
- `QUICK-START.md` — Guia prático de primeiros passos
- `EXAMPLES.md` — 5 cenários reais com exemplos de saída

## Como usar

### Opção 1: Em ambientes Claude Code

Se sua empresa usa Claude Code com suporte a skills customizadas:

1. Distribua o arquivo `orbit-prompt.skill`
2. Os usuários podem instalar via interface local
3. A skill ativará automaticamente em sessões que detectem padrões de ineficiência

### Opção 2: Como prompt standalone

Extraia o conteúdo do arquivo `.skill`:

```bash
unzip -q orbit-prompt.skill
cat SKILL.md
```

Use o conteúdo de `SKILL.md` como um prompt de sistema em qualquer aplicação de IA.

### Opção 3: Como framework interno

Adapte os padrões detectados para:
- Code review automation
- Task analysis pipelines
- Efficiency metrics
- Quality gates

## Integração corporativa

### Para desenvolvimento

- Incorpore o prompt em pipelines de revisão de código
- Use as métricas de padrão para medir eficiência de sessão
- Adapte os padrões para domínios específicos (arquitetura, dados, etc.)

### Para treinamento

- Eduque times sobre padrões de ineficiência com os exemplos
- Use o diagnóstico como base para retrospectivas
- Cite os padrões em code review para normalizar feedback

### Para automação

- Integre os padrões em bots de análise
- Crie alertas para padrões críticos
- Construa dashboards de saúde de sessão

## Características principais

✓ **Sem especulação** — Apenas padrões observáveis
✓ **Sem scoring** — Diagnóstico claro e direto
✓ **Silencioso em sucesso** — Não gera ruído em sessões saudáveis
✓ **Acionável** — Cada recomendação é específica e prática
✓ **Idioma agnóstico** — Funciona com qualquer linguagem de programação

## Padrões detectados

1. Respostas muito longas (não solicitadas)
2. Cadeias de correção
3. Edições repetidas ao mesmo alvo
4. Exploração sem plano
5. Prompts fracos (sem restrições)
6. Conteúdo grande colado (não referenciado)
7. Teatro de validação (sem execução)
8. Acúmulo de contexto (sessões longas)

## Suporte e customização

A skill é agnóstica de plataforma. Para customizar:

1. Extraia o conteúdo: `unzip orbit-prompt.skill`
2. Edite `SKILL.md` conforme necessário
3. Recompacte: `zip skill-customizada.skill *.md`
4. Distribua a versão customizada

## Versão

- **Versão:** 1.0.0
- **Data:** 23 de Abril de 2026
- **Compatibilidade:** Claude Code >=0.1.2

---

**Origem:** Orbit Engine — Sistema de visibilidade operacional para IA
