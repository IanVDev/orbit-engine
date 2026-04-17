package tracking

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestGenerateShareText valida o formato e o tamanho do texto de compartilhamento.
//
// Invariantes de cada variação:
//   - Contém tokens_saved, eficiência e CTA orbit.run.
//   - Sempre ≤ 200 caracteres Unicode (mesmo com valores extremos).
//   - Não exibe streak quando StreakDays == 0.
//   - Não vaza dados sensíveis (paths, session IDs).
//   - É legível: tem pelo menos 4 tokens de texto separados.
//   - É determinístico: mesmo input → mesmo output.
func TestGenerateShareText(t *testing.T) {
	// ── Testes de compatibilidade com o estilo padrão (técnica) ──────────────

	t.Run("format_contains_required_fields", func(t *testing.T) {
		s := HistoryStats{
			TotalSessions:     5,
			ActivatedSessions: 4,
			TotalTokensSaved:  13800,
			TotalCostSavedUSD: 0.0414,
			StreakDays:        4,
		}
		text := GenerateShareText(s) // sem estilo → ShareStyleTechnical

		if !strings.Contains(text, "13800") {
			t.Fatalf("share text deve conter tokens_saved, got: %q", text)
		}
		if !strings.Contains(text, "80%") {
			t.Fatalf("share text deve conter eficiência (80%% = 4/5), got: %q", text)
		}
		if !strings.Contains(text, "4") {
			t.Fatalf("share text deve conter streak count, got: %q", text)
		}
	})

	t.Run("size_under_200_chars", func(t *testing.T) {
		s := HistoryStats{
			TotalSessions:     999999,
			ActivatedSessions: 999999,
			TotalTokensSaved:  999999999,
			TotalCostSavedUSD: 9999.9999,
			StreakDays:        365,
		}
		text := GenerateShareText(s)
		n := utf8.RuneCountInString(text)
		if n > shareMaxChars {
			t.Fatalf("share text tem %d chars, want ≤%d: %q", n, shareMaxChars, text)
		}
	})

	t.Run("no_streak_when_zero", func(t *testing.T) {
		s := HistoryStats{
			TotalSessions:     1,
			ActivatedSessions: 1,
			TotalTokensSaved:  1000,
			TotalCostSavedUSD: 0.003,
			StreakDays:        0,
		}
		text := GenerateShareText(s)
		if strings.Contains(text, "streak") {
			t.Fatalf("share text não deve mostrar streak quando StreakDays=0, got: %q", text)
		}
	})

	t.Run("no_sensitive_data", func(t *testing.T) {
		s := HistoryStats{
			Path:              "/home/user/.orbit/history.jsonl",
			TotalTokensSaved:  5000,
			TotalCostSavedUSD: 0.015,
			ActivatedSessions: 2,
			TotalSessions:     3,
		}
		text := GenerateShareText(s)
		if strings.Contains(text, "/home") || strings.Contains(text, "history.jsonl") {
			t.Fatalf("share text não deve vazar paths sensíveis, got: %q", text)
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		s := HistoryStats{
			TotalSessions:     3,
			ActivatedSessions: 2,
			TotalTokensSaved:  5000,
			TotalCostSavedUSD: 0.015,
			StreakDays:        2,
		}
		a := GenerateShareText(s)
		b := GenerateShareText(s)
		if a != b {
			t.Fatalf("GenerateShareText deve ser determinístico: %q != %q", a, b)
		}
	})

	// ── Testes que cobrem os 3 estilos ───────────────────────────────────────

	t.Run("all_styles_have_cta", func(t *testing.T) {
		s := HistoryStats{
			TotalSessions:     3,
			ActivatedSessions: 2,
			TotalTokensSaved:  5000,
			TotalCostSavedUSD: 0.015,
			StreakDays:        2,
		}
		for _, style := range AllShareStyles {
			text := GenerateShareText(s, style)
			if !strings.Contains(text, "orbit.run") {
				t.Errorf("style %d (%s): share text deve conter CTA orbit.run, got: %q",
					style, ShareStyleLabel(style), text)
			}
		}
	})

	t.Run("all_styles_under_200_chars", func(t *testing.T) {
		s := HistoryStats{
			TotalSessions:     999999,
			ActivatedSessions: 999999,
			TotalTokensSaved:  999999999,
			TotalCostSavedUSD: 9999.9999,
			StreakDays:        365,
		}
		for _, style := range AllShareStyles {
			text := GenerateShareText(s, style)
			n := utf8.RuneCountInString(text)
			if n > shareMaxChars {
				t.Errorf("style %d (%s): share text tem %d chars, want ≤%d: %q",
					style, ShareStyleLabel(style), n, shareMaxChars, text)
			}
		}
	})

	t.Run("all_styles_legible", func(t *testing.T) {
		// Legível = tem pelo menos 4 tokens de texto separados por whitespace.
		s := HistoryStats{
			TotalSessions:     3,
			ActivatedSessions: 2,
			TotalTokensSaved:  5000,
			TotalCostSavedUSD: 0.015,
		}
		for _, style := range AllShareStyles {
			text := GenerateShareText(s, style)
			if len(strings.Fields(text)) < 4 {
				t.Errorf("style %d (%s): share text não é legível (poucos tokens), got: %q",
					style, ShareStyleLabel(style), text)
			}
		}
	})

	t.Run("no_streak_in_any_style_when_zero", func(t *testing.T) {
		s := HistoryStats{
			TotalSessions:     1,
			ActivatedSessions: 1,
			TotalTokensSaved:  1000,
			TotalCostSavedUSD: 0.003,
			StreakDays:        0,
		}
		for _, style := range AllShareStyles {
			text := GenerateShareText(s, style)
			if strings.Contains(text, "streak") {
				t.Errorf("style %d (%s): não deve conter 'streak' quando StreakDays=0, got: %q",
					style, ShareStyleLabel(style), text)
			}
		}
	})
}
