package main

import (
	"strings"
	"testing"
)

func TestGeneratePromptSections(t *testing.T) {
	out := GeneratePrompt("criar api de usuários com autenticação", IntentImplementation)
	for _, section := range []string{"Resumo leigo:", "Objetivo:", "Risco principal:", "Escopo:", "Requisitos:", "Saída esperada:"} {
		if !strings.Contains(out, section) {
			t.Fatalf("prompt deve conter seção %q", section)
		}
	}
	if !strings.Contains(out, "orbit-engine") {
		t.Fatal("prompt deve referenciar skill orbit-engine")
	}
}

func TestGeneratePromptContainsInput(t *testing.T) {
	input := "criar api de usuários com autenticação"
	out := GeneratePrompt(input, IntentImplementation)
	if !strings.Contains(out, input) {
		t.Fatal("prompt deve conter o objetivo fornecido pelo usuário")
	}
}

func TestGeneratePromptTrimsInput(t *testing.T) {
	out := GeneratePrompt("  objetivo com espaços  ", IntentImplementation)
	if strings.Contains(out, "  objetivo") {
		t.Fatal("GeneratePrompt deve aplicar TrimSpace no input")
	}
}

func TestValidatePromptInputTooShort(t *testing.T) {
	for _, bad := range []string{"", "asdf", "   ", "abc"} {
		if err := validatePromptInput(bad); err == nil {
			t.Fatalf("validatePromptInput(%q) deve falhar para input curto", bad)
		}
	}
}

func TestValidatePromptInputValid(t *testing.T) {
	if err := validatePromptInput("criar api de usuários"); err != nil {
		t.Fatalf("validatePromptInput falhou para input válido: %v", err)
	}
}

func TestRecommendAIOpus(t *testing.T) {
	for _, kw := range []string{"arquitetura", "sistema", "complexo", "design"} {
		if got := RecommendAI(kw); got != "Opus" {
			t.Fatalf("RecommendAI(%q) = %q, want Opus", kw, got)
		}
	}
}

func TestRecommendAISonnet(t *testing.T) {
	for _, kw := range []string{"refatorar", "analisar", "debug", "revisar"} {
		if got := RecommendAI(kw); got != "Sonnet" {
			t.Fatalf("RecommendAI(%q) = %q, want Sonnet", kw, got)
		}
	}
}

func TestRecommendAIHaikuDefault(t *testing.T) {
	if got := RecommendAI("criar endpoint simples"); got != "Haiku" {
		t.Fatalf("RecommendAI default = %q, want Haiku", got)
	}
}

func TestMetricNameForAI(t *testing.T) {
	cases := map[string]string{
		"Opus":   MetricPromptAIOpusTotal,
		"Sonnet": MetricPromptAISonnetTotal,
		"Haiku":  MetricPromptAIHaikuTotal,
		"other":  MetricPromptAIHaikuTotal,
	}
	for ai, want := range cases {
		if got := metricNameForAI(ai); got != want {
			t.Fatalf("metricNameForAI(%q) = %q, want %q", ai, got, want)
		}
	}
}

func TestClassifyIntentDefault(t *testing.T) {
	for _, input := range []string{
		"criar endpoint de autenticação",
		"adicionar campo ao modelo",
		"implementar paginação na listagem",
		"criar função de hash",
	} {
		t.Run(input, func(t *testing.T) {
			if got := ClassifyIntent(input); got != IntentImplementation {
				t.Fatalf("ClassifyIntent(%q) = %q, want %q", input, got, IntentImplementation)
			}
		})
	}
}

func TestClassifyIntentAnalysis(t *testing.T) {
	for _, input := range []string{"analisar módulo de logs", "entender o fluxo de autenticação", "mapear dependências do serviço"} {
		t.Run(input, func(t *testing.T) {
			if got := ClassifyIntent(input); got != IntentAnalysis {
				t.Fatalf("ClassifyIntent(%q) = %q, want %q", input, got, IntentAnalysis)
			}
		})
	}
}

func TestClassifyIntentDebug(t *testing.T) {
	for _, input := range []string{"debug do endpoint de login", "investigar erro 500 na api", "reproduzir bug de concorrência"} {
		t.Run(input, func(t *testing.T) {
			if got := ClassifyIntent(input); got != IntentDebug {
				t.Fatalf("ClassifyIntent(%q) = %q, want %q", input, got, IntentDebug)
			}
		})
	}
}

func TestClassifyIntentArchitecture(t *testing.T) {
	for _, input := range []string{"arquitetura do sistema de cache", "design da camada de persistência", "trade-off entre kafka e rabbitmq"} {
		t.Run(input, func(t *testing.T) {
			if got := ClassifyIntent(input); got != IntentArchitecture {
				t.Fatalf("ClassifyIntent(%q) = %q, want %q", input, got, IntentArchitecture)
			}
		})
	}
}
