# Orbit Prompt Skill — Exportação Técnica

**Arquivo:** `orbit-prompt.skill`
**Versão:** 1.1.0
**Data:** 23 de Abril de 2026

## O que é

A skill de prompt universal da Orbit Engine, empacotada para distribuição e uso em ambientes corporativos.

**Novidade na v1.1.0:** Comando `/orbit-prompt` para melhorar prompts antes de enviar.

## Conteúdo

- `SKILL.md` — Definição completa do prompt + comando `/orbit-prompt`
- `ONBOARDING.md` — Orientação inicial para novos usuários
- `QUICK-START.md` — Guia prático de primeiros passos (3 min)
- `EXAMPLES.md` — 6 cenários reais com exemplos de saída (diagnóstico + melhoria)

## Como usar

### Opção 1: Em ambientes Claude Code

Se sua empresa usa Claude Code com suporte a skills customizadas:

1. Distribua o arquivo `orbit-prompt.skill`
2. Os usuários podem instalar via interface local
3. A skill ativa automaticamente:
   - **Diagnóstico:** Detecta padrões de ineficiência em sessões
   - **Melhoria de prompt:** Use `/orbit-prompt [tarefa]` antes de enviar

### Opção 2: Como prompt standalone

Extraia o conteúdo do arquivo `.skill`:

```bash
unzip -q orbit-prompt.skill
cat SKILL.md
```

Use o conteúdo de `SKILL.md` como um prompt de sistema em qualquer aplicação de IA:
- ChatGPT
- Claude (via API)
- Qualquer outro LLM

**Para simular `/orbit-prompt` manualmente:**
- Siga a seção "PROMPT IMPROVEMENT output" do SKILL.md
- Siga o fluxo de análise e reescrita

### Opção 3: Como framework interno

Adapte os padrões detectados para:
- Code review automation
- Task analysis pipelines
- Efficiency metrics
- Quality gates
- Prompt refinement services

---

## O comando `/orbit-prompt` (NOVO na v1.1.0)

### Sintaxe

```
/orbit-prompt [tarefa vaga do usuário]
```

### Exemplo

**Entrada:**
```
/orbit-prompt "Refactor the authentication module"
```

**Saída:**
```
[Orbit Engine — Prompt Refinement]

ORIGINAL PROMPT:
"Refactor the authentication module"

ANALYSIS:
- Missing: which files? (auth.ts, login.tsx, both?)
- Missing: scope — are we changing password flow, OAuth, both?
- Missing: what stays the same? (function signatures, API contracts?)

IMPROVED PROMPT:
"Extract the password validation logic from auth.ts into validators/password.ts.
Keep the existing function signatures (validatePassword, validateStrength).
Don't touch routes.ts or database schema. Success = all existing tests pass,
validation logic is 100% covered by tests."

KEY IMPROVEMENTS:
- Specific file targets (password-validator.ts → validators.ts)
- Clear scope boundary (password only, not OAuth)
- Preserved contracts (function signatures stay the same)
- Acceptance criteria (tests pass, code coverage)

READY TO SEND: YES
Reason: Constraints are clear, rework risk is low, success is testable.
```

### O que `/orbit-prompt` analisa

1. **Targets missing** — Quais arquivos/funções/componentes?
2. **Scope undefined** — Qual é o escopo exato?
3. **Constraints absent** — O que pode mudar? O que não pode?
4. **Acceptance criteria unclear** — Como saber que está pronto?
5. **Ambiguity risk** — Pode ser interpretado de formas diferentes?

### O que `/orbit-prompt` retorna

1. **ANALYSIS** — O que está faltando (específico, não genérico)
2. **IMPROVED PROMPT** — Versão reescrita com todos os gaps preenchidos
3. **KEY IMPROVEMENTS** — Por que cada melhoria foi feita
4. **READY TO SEND** — Sim/Não e por quê

---

## Integração corporativa

### Para desenvolvimento

- Incorpore o prompt em pipelines de revisão de código
- Use `/orbit-prompt` para melhorar requisitos antes de implementar
- Customize os padrões para domínios específicos (arquitetura, dados, etc.)

### Para treinamento

- Eduque times sobre padrões de ineficiência com os exemplos
- Ensine como usar `/orbit-prompt` para melhorar comunicação
- Use o diagnóstico como base para retrospectivas

### Para automação

- Integre os padrões em bots de análise
- Crie um serviço `/orbit-prompt` como API interna
- Construa dashboards de saúde de sessão e qualidade de prompts

### Para requisitos

- Use `/orbit-prompt` ao receber requisitos dos stakeholders
- Melhore PRDs e especificações
- Padronize como requisitos são comunicados

---

## Características principais

✓ **Sem especulação** — Apenas padrões observáveis
✓ **Sem scoring** — Diagnóstico claro e direto
✓ **Silencioso em sucesso** — Não gera ruído em sessões saudáveis
✓ **Acionável** — Cada recomendação é específica e prática
✓ **Agnóstico de plataforma** — Funciona com qualquer LLM
✓ **Melhora prompts** — `/orbit-prompt` deixa tarefas claras e constrained

## Padrões detectados (diagnóstico automático)

1. Respostas muito longas (não solicitadas)
2. Cadeias de correção (múltiplas follow-ups)
3. Edições repetidas (mesmo arquivo 3+ vezes)
4. Exploração sem plano (muitos files, sem direção)
5. Prompts fracos (sem constraints)
6. Conteúdo grande colado (não referenciado)
7. Teatro de validação (sem execução)
8. Acúmulo de contexto (sessões longas)

---

## Customização

A skill é agnóstica de plataforma. Para customizar:

1. Extraia o conteúdo:
   ```bash
   unzip orbit-prompt.skill
   ```

2. Edite `SKILL.md` conforme necessário:
   - Adicione padrões específicos do domínio
   - Customize exemplos
   - Ajuste critérios de ativação

3. Recompacte:
   ```bash
   zip skill-customizada.skill *.md
   ```

4. Distribua a versão customizada

### Exemplos de customização

**Para times de data:**
- Adicione padrões sobre exploração de dados sem análise
- Customize exemplos para pipelines
- Focus em validation theater (modelos não testados)

**Para times de infraestrutura:**
- Aumente sensibilidade para contexto acumulado
- Customize exemplos para deployment
- Focus em rework de configs

**Para times de produto:**
- Customize para requisitos de produto
- Adicione padrões sobre feature creep
- Focus em scope management

---

## Versão

- **Versão:** 1.1.0
- **Data:** 23 de Abril de 2026
- **Compatibilidade:** Claude Code >=0.1.2
- **Novidades:** Comando `/orbit-prompt` para melhoria de prompts

---

## Suporte

**Questões técnicas?** Consulte:
- `SKILL.md` — Definição completa
- `EXAMPLES.md` — Cenários reais
- `QUICK-START.md` — Guia prático

**Origem:** Orbit Engine — Sistema de visibilidade operacional para IA
