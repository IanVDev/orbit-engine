package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const promptMinLength = 10

const (
	MetricPromptGeneratedTotal = "prompt_generated_total"
	MetricPromptAIOpusTotal    = "prompt_ai_opus_total"
	MetricPromptAISonnetTotal  = "prompt_ai_sonnet_total"
	MetricPromptAIHaikuTotal   = "prompt_ai_haiku_total"
)

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

// GeneratePrompt monta um prompt estruturado no padrão Aurya-style a partir
// do objetivo descrito pelo usuário. Template fixo — saída determinística.
func GeneratePrompt(userInput string) string {
	input := strings.TrimSpace(userInput)
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

// RecommendAI retorna o modelo Claude recomendado com base em palavras-chave
// no input. Lógica determinística — sem heurística solta.
func RecommendAI(input string) string {
	l := strings.ToLower(input)
	switch {
	case strings.Contains(l, "arquitetura"),
		strings.Contains(l, "sistema"),
		strings.Contains(l, "complexo"),
		strings.Contains(l, "design"):
		return "Opus"
	case strings.Contains(l, "refatorar"),
		strings.Contains(l, "analisar"),
		strings.Contains(l, "debug"),
		strings.Contains(l, "revisar"):
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
				"     orbit prompt --copy \"refatorar módulo de logs\"\n" +
				"     orbit prompt \"criar api simples de usuários\"",
		)
	}

	input := strings.Join(args, " ")
	if err := validatePromptInput(input); err != nil {
		return err
	}

	prompt := GeneratePrompt(input)
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
