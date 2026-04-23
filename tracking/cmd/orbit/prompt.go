package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const promptMinLength = 10

type PromptIntent string

const (
	IntentAnalysis       PromptIntent = "analysis"
	IntentImplementation PromptIntent = "implementation"
	IntentDebug          PromptIntent = "debug"
	IntentArchitecture   PromptIntent = "architecture"
)

const (
	MetricPromptGeneratedTotal = "prompt_generated_total"
	MetricPromptAIOpusTotal    = "prompt_ai_opus_total"
	MetricPromptAISonnetTotal  = "prompt_ai_sonnet_total"
	MetricPromptAIHaikuTotal   = "prompt_ai_haiku_total"
)

// ClassifyIntent inspeciona o input e retorna a PromptIntent correspondente.
// Determinístico — baseado em keywords. Default: IntentImplementation.
// Fail-safe: input vazio ou sem match retorna IntentImplementation, nunca panic.
func ClassifyIntent(input string) PromptIntent {
	l := strings.ToLower(input)
	switch {
	case strings.Contains(l, "debug") ||
		strings.Contains(l, "erro") ||
		strings.Contains(l, "falha") ||
		strings.Contains(l, "bug") ||
		strings.Contains(l, "não funciona") ||
		strings.Contains(l, "reproduzir"):
		return IntentDebug
	case strings.Contains(l, "arquitetura") ||
		strings.Contains(l, "design") ||
		strings.Contains(l, "sistema") ||
		strings.Contains(l, "complexo") ||
		strings.Contains(l, "trade-off") ||
		strings.Contains(l, "alternativa") ||
		strings.Contains(l, "decisão") ||
		strings.Contains(l, "estrutura"):
		return IntentArchitecture
	case strings.Contains(l, "analis") ||
		strings.Contains(l, "entend") ||
		strings.Contains(l, "mape") ||
		strings.Contains(l, "investig") ||
		strings.Contains(l, "decomp") ||
		strings.Contains(l, "revis") ||
		strings.Contains(l, "refator"):
		return IntentAnalysis
	default:
		return IntentImplementation
	}
}

// enrichInput adiciona contexto semântico ao input conforme a intenção.
// Retorna o input original quando não há enriquecimento para o intent.
func enrichInput(input string, intent PromptIntent) string {
	switch intent {
	case IntentDebug:
		return input + "\n\nContexto adicional: descreva o comportamento observado vs esperado, stack trace disponível, e passos para reproduzir."
	case IntentArchitecture:
		return input + "\n\nContexto adicional: liste restrições do sistema, volume esperado, e quais alternativas já foram descartadas."
	case IntentAnalysis:
		return input + "\n\nContexto adicional: indique o escopo da análise (módulo, serviço, fluxo) e o artefato de saída esperado (diagrama, relatório, lista)."
	default:
		return input
	}
}

// generateImplementationPrompt monta o template padrão para tarefas de implementação.
func generateImplementationPrompt(input string) string {
	return fmt.Sprintf(`Atue como engenheiro sênior.

Resumo leigo:
%s

Objetivo:
%s

Contexto:
- Projeto em evolução
- Necessário código real e testável
- Sem overengineering

Risco principal:
- inconsistência ou regressão em código existente

Escopo:
- Implementação direta
- Código completo
- 1 teste anti-regressão

Requisitos:
- Fail-closed
- Logs claros
- Sem dependência desnecessária

Saída esperada:
- Código pronto
- Teste validando comportamento

Use a skill orbit-engine para garantir consistência.
`, input, input)
}

// generateAnalysisPrompt monta o template para análise e entendimento de código.
func generateAnalysisPrompt(input string) string {
	return fmt.Sprintf(`Atue como engenheiro sênior com foco em análise e entendimento.

Objetivo de análise:
%s

Metodologia:
- Decomponha o sistema/fluxo em componentes menores
- Mapeie dependências e fluxos de dados
- Identifique invariantes e contratos implícitos
- Documente assunções

Escopo:
- Análise não-invasiva (sem alterar código)
- Saída: mapeamento estruturado e observações

Risco principal:
- análise incompleta por escopo mal definido

Saída esperada:
- Diagrama ou lista estruturada dos componentes
- Pontos de atenção identificados
- Perguntas em aberto que requerem investigação adicional

Use a skill orbit-engine para garantir consistência.
`, input)
}

// generateDebugPrompt monta o template para diagnóstico e causa raiz.
func generateDebugPrompt(input string) string {
	return fmt.Sprintf(`Atue como engenheiro sênior com foco em diagnóstico e causa raiz.

Problema a diagnosticar:
%s

Metodologia de diagnóstico:
- Reproduza o problema de forma isolada
- Identifique a causa raiz (não apenas o sintoma)
- Diferencie comportamento observado vs esperado

Risco principal:
- corrigir sintoma sem eliminar a causa raiz

Escopo:
- Diagnóstico completo antes de qualquer mudança de código
- Reprodução mínima do problema

Requisitos:
- Descreva o comportamento observado (com evidência: log, stack trace, output)
- Descreva o comportamento esperado
- Hipótese da causa raiz com justificativa
- Passos para reprodução

Saída esperada:
- Causa raiz identificada
- Reprodução mínima
- Correção cirúrgica com teste de regressão

Use a skill orbit-engine para garantir consistência.
`, input)
}

// generateArchitecturePrompt monta o template para decisões arquiteturais.
func generateArchitecturePrompt(input string) string {
	return fmt.Sprintf(`Atue como arquiteto de software sênior.

Decisão arquitetural:
%s

Metodologia:
- Levante as alternativas viáveis (mínimo 2)
- Avalie trade-offs de cada alternativa
- Defina critérios de decisão explícitos

Risco principal:
- decisão irreversível tomada sem análise de trade-offs

Escopo:
- Análise de alternativas, não implementação
- Documentação da decisão (ADR-style)

Requisitos:
- Liste alternativas com prós e contras
- Defina critérios de decisão (performance, manutenibilidade, custo)
- Indique a alternativa recomendada com justificativa

Saída esperada:
- Comparação estruturada das alternativas
- Critérios de decisão ponderados
- Recomendação com justificativa explícita

Use a skill orbit-engine para garantir consistência.
`, input)
}

// validatePromptInput rejeita inputs triviais que gerariam prompt inútil.
func validatePromptInput(input string) error {
	if len(strings.TrimSpace(input)) < promptMinLength {
		return fmt.Errorf(
			"CRITICAL: input insuficiente para gerar prompt útil (mínimo %d caracteres)\n\n"+
				"   Exemplo: orbit prompt \"criar endpoint de autenticação com jwt\"",
			promptMinLength,
		)
	}
	return nil
}

// GeneratePrompt monta um prompt estruturado a partir do objetivo e intenção.
// Template específico por tipo de tarefa — saída determinística.
func GeneratePrompt(userInput string, intent PromptIntent) string {
	input := strings.TrimSpace(userInput)
	enriched := enrichInput(input, intent)
	switch intent {
	case IntentAnalysis:
		return generateAnalysisPrompt(enriched)
	case IntentDebug:
		return generateDebugPrompt(enriched)
	case IntentArchitecture:
		return generateArchitecturePrompt(enriched)
	default:
		return generateImplementationPrompt(enriched)
	}
}

// RecommendAI retorna o modelo Claude recomendado com base na intenção
// e complexidade detectada. Lógica determinística — sem heurística solta.
func RecommendAI(input string) string {
	switch ClassifyIntent(input) {
	case IntentArchitecture:
		return "Opus"
	case IntentDebug, IntentAnalysis:
		return "Sonnet"
	default:
		return "Haiku"
	}
}

// metricNameForAI retorna a constante de métrica correspondente ao modelo.
func metricNameForAI(ai string) string {
	switch ai {
	case "Opus":
		return MetricPromptAIOpusTotal
	case "Sonnet":
		return MetricPromptAISonnetTotal
	default:
		return MetricPromptAIHaikuTotal
	}
}

// copyToClipboard envia text para pbcopy (macOS). Fail-soft: avisa em stderr
// mas não derruba o comando — o output em stdout continua íntegro.
func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard não disponível (pbcopy): %w", err)
	}
	return nil
}

func runPrompt(args []string, copyFlag bool) error {
	if len(args) == 0 {
		return fmt.Errorf(
			"uso: orbit prompt [--copy] \"seu objetivo\"\n\n" +
				"   Exemplos:\n" +
				"     orbit prompt \"criar endpoint de autenticação com jwt\"\n" +
				"     orbit prompt --copy \"debug de erro 500 na api de login\"\n" +
				"     orbit prompt \"arquitetura do sistema de cache\"",
		)
	}

	input := strings.Join(args, " ")
	if err := validatePromptInput(input); err != nil {
		return err
	}

	intent := ClassifyIntent(input)
	prompt := GeneratePrompt(input, intent)
	ai := RecommendAI(input)

	// Métricas fail-soft — erros não bloqueiam o output.
	if _, err := IncrementMetric(MetricPromptGeneratedTotal); err != nil {
		fmt.Fprintf(os.Stderr, "orbit: warn — métrica %s: %v\n", MetricPromptGeneratedTotal, err)
	}
	if _, err := IncrementMetric(metricNameForAI(ai)); err != nil {
		fmt.Fprintf(os.Stderr, "orbit: warn — métrica %s: %v\n", metricNameForAI(ai), err)
	}

	fmt.Fprint(os.Stdout, prompt)
	fmt.Fprintf(os.Stdout, "AI recomendada: %s\n", ai)

	if copyFlag {
		if err := copyToClipboard(prompt); err != nil {
			fmt.Fprintf(os.Stderr, "orbit: warn — %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "orbit: prompt copiado para o clipboard")
		}
	}
	return nil
}
