# Orbit — Monetização

**Status:** modelo observacional em 3 fases · hoje em Fase 1

---

## Resumo leigo

O Orbit cobra como mede: devagar, com evidência, sem surpresa. Nunca há um preço antes de um dado — e o dado vive no seu dashboard antes de virar linha na fatura. Três fases, cada uma com gatilho observável, cada uma com fallback gratuito.

---

## Princípio central

> **Nenhum preço nasce de expectativa comercial. Todo preço nasce de custo observado, e você vê o custo antes de pagá-lo.**

---

## As três fases

### Fase 1 — Gratuita, local, sem cadastro

Hoje.

- Você instala o binário `orbit`.
- Você roda comandos, gera logs, usa o dashboard localmente.
- Não há conta, não há login, não há limite.
- Nada sobe para nenhum servidor nosso a menos que você deliberadamente ative telemetria anônima (opt-in, com cada campo enviado descrito no código).

**O que você vê:** o custo computacional do próprio Orbit — quanto tempo cada ação levou, quantos bytes de log gerou, qual foi o hash de prova. Transparência começa no primeiro `orbit run`.

### Fase 2 — Hospedagem opcional, preço fixo

Futuro, ativada somente após evidência sustentada de demanda (não calendário).

- Mesma CLI gratuita.
- Opcionalmente: dashboard e tracking-server hospedados por nós, por um valor fixo mensal.
- **Paridade obrigatória:** cada feature no hospedado já existe no local. Você paga por hospedagem, nunca por acesso.
- Campo novo nos logs: `resource_cost` gravado em cada ação (ainda não cobrado — apenas observado).

**O que você vê:** o mesmo dashboard, agora com a coluna de custo real de cada ação servidor-side. Você observa por 30+ dias antes de qualquer coisa virar cobrável.

### Fase 3 — Cobrança por atividade em ações de alto custo servidor

Futuro distante, ativada somente após 30 dias contínuos de dados de custo por ação.

- Mesma CLI gratuita para sempre.
- Mesmo hospedado flat da Fase 2 como tier base.
- Ações específicas (e somente essas) passam a ser medidas por uso — tipicamente verify em lote, diagnose hospedado, retenção estendida.
- **Preview de custo obrigatório:** cada ação paga mostra o valor estimado antes de executar. Sem exceção.
- **Fallback livre:** toda ação paga tem equivalente local gratuito. A cobrança é por conveniência, nunca por acesso.

**O que você vê:** antes de cada ação paga, uma linha clara — "esta operação vai custar R$ 0,0XX". Nenhuma fatura surge de algo que você não confirmou.

---

## Garantias ao usuário

1. **A CLI local é gratuita para sempre.** Licença aberta, código auditável. Não podemos revogar isso retroativamente — o commit existe.
2. **Paridade de feature.** Tudo que existe no hospedado existe no local. Nenhum "Pro only" artificial.
3. **Preview antes de pagar.** Toda ação cobrável mostra o preço antes de executar, sem cliques escondidos.
4. **Seus logs são seus.** A qualquer momento você pode baixar tudo e voltar para self-hosted, sem perda.
5. **Janela de reajuste.** Qualquer mudança de preço exige 30 dias de novos dados de custo observado. Preço não sobe por decisão de mercado — sobe por evidência de custo.
6. **Teto previsível.** Contas têm limite mensal default. Surpresa de fatura não é um mecanismo de crescimento.

Cada garantia acima corresponde a um artefato verificável: licença, código, campo no log, teste de política, parâmetro de conta.

---

## O que nunca faremos

- Cobrar por uma ação cujo custo não foi **observado por pelo menos 30 dias** em dados reais.
- Criar um tier pago artificialmente (paywall em feature que já era grátis).
- Limitar uso via rate limit não declarado.
- Enviar telemetria sem opt-in explícito, com cada campo enviado descrito no código.
- Alterar preço sem aviso de 30 dias E sem commit público citando o dataset de evidência.
- Descontinuar a versão local da CLI.
- Migrar um cliente de tier sem o clique explícito dele.
- Usar desconto por volume como mecanismo de trava (lock-in por desconto).
- Cobrar pela ausência — nenhum preço por "manter conta viva" ou "manter dados acessíveis".

---

## Como decisões de preço são tomadas

O mesmo protocolo que rege a expansão de parser (`EXPANSION_DECISION_PROTOCOL` em `scripts/parse_orbit_events.py`) rege o nascimento de qualquer item precificado:

1. **T₀ — observação.** Uma categoria de ação passa a ser medida (`resource_cost` no log). Nenhuma cobrança, 30 dias de acumulação.
2. **T₁ — confirmação.** Após 30 dias, o custo médio e P95 são estáveis? A categoria persiste ou foi um pico? Se foi pico, reinicia o ciclo.
3. **Evidência pública.** Só então um commit adiciona a categoria a `BILLABLE_ACTIONS`, citando o dataset observado como referência (`evidence_dataset_ref`).
4. **Teste automatizado trava o rigor.** Um teste (`PricingEvidenceGateTest`, análogo ao `test_expansion_candidate_contract` já presente no repo) verifica que toda ação cobrada tem referência ao dataset e janela mínima de observação. Sem isso, o commit não passa.
5. **Reajuste segue mesma regra.** Nenhum preço muda sem 30 dias de novos dados e nova referência pública.

Isso significa que decisões de preço no Orbit são **auditáveis no git log**. Você pode ver exatamente quando um preço nasceu, sob qual evidência, e comparar com o dashboard público daquela janela.

---

## Veredito

Este modelo é deliberadamente lento em receita. A troca é firmeza em confiança: nenhuma fatura surpreende, nenhuma cobrança aparece antes da observação, nenhum tier é inventado para parecer "Pro".

O Orbit observa antes de agir — e observa antes de cobrar. O preço é uma consequência tardia da realidade, não uma decisão antecipada de negócio. Se um dia o modelo parecer rígido demais, a pergunta correta não é *"como flexibilizar o preço?"* — é *"o que mudou no uso real que justifica revisão?"*. A resposta sempre vem do log, nunca do pitch.

---

*Estamos em Fase 1 até que dados observados autorizem transição. Este documento é versionado; alterações exigem commit público referenciando a evidência que as motivou.*
