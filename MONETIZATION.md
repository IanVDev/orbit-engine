# ORBIT — Monetização

**Data:** 2026-04-14  
**Status:** v1.0 — pronto para uso

---

## Premissa

O produto detecta desperdício real e o mede com dados reais. A monetização reflete isso: FREE entrega o diagnóstico, PRO entrega o histórico e a tendência. Nenhuma funcionalidade de detecção fica atrás de paywall.

---

## 1. FREE vs PRO

### FREE — sem conta, sem login, sem limite de uso

O que está incluído:

- Detecção completa dos 6 padrões de desperdício (correction chains, repeated edits, unsolicited output, exploratory reading, weak prompts, large pastes)
- Diagnóstico por sessão com nível de risco (low / medium / high / critical)
- Ações específicas — comandos exatos, não conselhos genéricos
- Silêncio quando não há problema
- SESSION RESULT: composite de impacto da sessão atual, breakdown por métrica, flag de tradeoff
- Funciona com qualquer AI assistant (Claude Code, GPT, Gemini, outros)
- Código aberto, auditável

O FREE resolve o problema principal: o usuário sabe o que está errado e o que fazer. Não é uma versão limitada — é um produto completo para uso individual por sessão.

### PRO — para quem quer medir e melhorar ao longo do tempo

O que é adicionado ao FREE:

| Funcionalidade | O que faz | Por que é PRO |
| --- | --- | --- |
| Histórico de impacto | Armazena SESSION RESULT de todas as sessões; permite ver evolução ao longo do tempo | Requer persistência e backend |
| Comparação manual vs skill-suggested | Mostra diferença de composite entre sessões com e sem a skill ativa | Requer histórico com campo `origin` |
| Detecção de regressão | Alerta quando uma métrica que estava melhorando começa a piorar | Requer série temporal mínima de 5 sessões |
| Análise de tendência | Gráfico simples de composite ao longo das últimas N sessões | Requer histórico |
| Dashboard de time | Agrega padrões de desperdício por membro do time | Requer conta de time e múltiplos usuários |
| Evidence log exportável | Export do audit trail completo em JSON ou CSV | Convenência, não funcionalidade core |

Nada do acima afeta a detecção. Um usuário FREE tem diagnósticos tão precisos quanto um usuário PRO — ele simplesmente não tem histórico persistido nem análise de tendência.

---

## 2. Pricing

### Individual

| Plano | Preço | Para quem |
| --- | --- | --- |
| FREE | $0 / sempre | Uso pessoal, sessão a sessão |
| PRO Individual | $12 / mês ou $99 / ano | Dev solo que quer medir progresso ao longo do tempo |

O anual equivale a 8,25/mês — desconto de ~31%. Sem trial com cartão. O FREE já é o trial.

### Teams

| Plano | Preço | Para quem |
| --- | --- | --- |
| PRO Team (até 5) | $49 / mês | Time pequeno, um responsável pelo billing |
| PRO Team (até 20) | $149 / mês | Time médio, tech lead quer visibilidade agregada |
| Enterprise | Contato | Mais de 20 devs, SSO, SLA, audit log dedicado |

Teams inclui tudo do PRO Individual para cada membro, mais o dashboard de time.

### Princípio de pricing

O custo do PRO Individual ($99/ano) se paga se o produto evitar 5–6 sessões desperdiçadas por ano. Com o baseline de $18 por sessão ruim vs $2 por sessão boa, a diferença por sessão é $16. Cinco sessões = $80 economizados. O produto não precisa prometer mais do que isso.

---

## 3. O moat

O produto tem três camadas de defesa contra cópia:

**Camada 1 — dados próprios**  
O evidence_log é append-only e acumula histórico do usuário específico. Nenhum concorrente tem acesso a esse histórico. Com o tempo, o produto melhora com os dados do próprio usuário — o que é matematicamente impossível de replicar sem o histórico.

**Camada 2 — o loop de auto-evolução**  
A skill melhora através do decision engine com 3 gates (validação, impacto, segurança). Cada versão da skill é validada contra 18 testes antes de ser aceita. Um concorrente pode copiar o código mas não copia os dados de calibração nem o histórico de evidências que sustenta cada decisão de evolução.

**Camada 3 — detecção calibrada**  
Os padrões de desperdício foram calibrados contra sessões reais, não documentação. A precisão da detecção (0 falsos positivos nos 18 testes canônicos) vem de iteração sobre casos reais. Clonar o código gera um produto com detecção descalibrada — que vai alertar demais ou de menos.

O que não é moat: o conceito em si ("detectar desperdício em sessões de AI") pode ser copiado. O posicionamento, a calibração e o histórico acumulado não podem.

---

## 4. O que não tem paywall

Regra não negociável: nenhuma funcionalidade de detecção fica bloqueada no FREE.

Razão: o produto vive de credibilidade. Se um usuário FREE recebe diagnóstico incompleto, ele não tem como saber que a versão PRO é melhor — e desconfia que o FREE é deliberadamente degradado. Isso destrói confiança.

O FREE detecta tudo, diagnostica tudo, recomenda tudo. O PRO adiciona dimensão temporal (histórico) e colaboração (times). Essas são funcionalidades genuinamente diferentes, não restrições artificiais.

---

## 5. Caminho de conversão FREE → PRO

### Gatilho baseado em uso real

O prompt de upgrade aparece **uma única vez**, depois que o usuário acumula **5 sessões com SESSION RESULT positivo** (composite > 20%). Esse número é o mínimo para que o usuário tenha evidência própria de que o produto funciona.

Condições que devem ser verdadeiras simultaneamente:

- Sessões com impact medido: ≥ 5
- Pelo menos 3 dessas com `impact_status == "positive"`
- Nenhum prompt de upgrade exibido antes para este usuário

Se qualquer condição falhar, o produto permanece silencioso sobre PRO.

### Exemplo de mensagem de upgrade

A mensagem é gerada a partir dos dados reais do usuário, não de texto genérico:

```text
Você tem 7 sessões com impacto medido.
Composite médio: +61% | Melhor sessão: +76% (rework)

Para ver se esse padrão está se mantendo ao longo do tempo
ou comparar sessões com e sem a skill ativa:
→ orbit.dev/pro

(isso não vai aparecer de novo)
```

Regras da mensagem:

- Usa números reais do evidence_log do usuário — nunca estimativas.
- Menciona especificamente o que o usuário ganharia com PRO, baseado no que ele já tem (ex: se há tradeoff recorrente, mencionar detecção de regressão; se há sessões com origin misto, mencionar comparação).
- A última linha confirma que não vai repetir. Isso reduz a percepção de pressão.
- Sem urgência artificial, sem desconto por tempo limitado, sem CTA vermelho.

### O que nunca acontece

- Prompt de upgrade em sessão com SESSION RESULT negativo ou sem impacto medido.
- Prompt de upgrade antes das 5 sessões positivas.
- Segundo prompt de upgrade (mesmo que o usuário nunca clique).
- Banner permanente, badge, ou indicador visual de "funcionalidade bloqueada".
- E-mail de follow-up após o prompt.

O produto não persiste atrás do usuário. Se ele viu o prompt e ignorou, o assunto está encerrado do lado do produto.

### Gatilho para times

Para o plano Team, o gatilho é diferente: o tech lead precisa ver padrão recorrente **no time**, não em sessão individual. O prompt de Team aparece somente quando o dashboard individual mostra padrão que seria mais útil analisado no agregado:

```text
Você detectou "correction chains" em 4 das últimas 6 sessões.
Se outros devs do seu time têm o mesmo padrão, você não veria aqui.
→ orbit.dev/teams
```

Mesmo: uma vez, sem repetição.

---

## Resumo em uma linha por plano

**FREE:** você sabe o que está errado em cada sessão e o que fazer.  
**PRO Individual:** você sabe se está melhorando ao longo do tempo.  
**PRO Team:** o time lead sabe quais padrões persistem no time.
