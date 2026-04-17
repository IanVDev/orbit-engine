// share.go — geração de texto de compartilhamento para orbit stats.
//
// Três variações de copy, todas determinísticas e sem dados sensíveis:
//
//   ShareStyleTechnical   — métricas diretas, formato compacto
//   ShareStyleProvocative — narrativa + desafio social
//   ShareStyleMinimalist  — limpo, ideal para cards/posts
//
// Invariantes:
//   - Sem paths, session IDs ou nomes de comando.
//   - Sempre ≤ shareMaxChars caracteres Unicode.
//   - GenerateShareText(s) sem estilo → ShareStyleTechnical (backward compat).
package tracking

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
)

// shareMaxChars é o limite máximo de caracteres Unicode no texto de share.
const shareMaxChars = 200

// ShareStyle define o estilo de copy do texto de compartilhamento.
type ShareStyle int

const (
	ShareStyleTechnical   ShareStyle = iota // métricas diretas
	ShareStyleProvocative                   // narrativa + desafio
	ShareStyleMinimalist                    // limpo e minimalista
)

// AllShareStyles lista os estilos em ordem de exibição no CLI.
var AllShareStyles = []ShareStyle{
	ShareStyleTechnical,
	ShareStyleProvocative,
	ShareStyleMinimalist,
}

// ShareStyleLabel retorna o rótulo legível de um estilo.
func ShareStyleLabel(s ShareStyle) string {
	switch s {
	case ShareStyleProvocative:
		return "Provocativa"
	case ShareStyleMinimalist:
		return "Minimalista"
	default:
		return "Técnica"
	}
}

// GenerateShareText retorna o texto de compartilhamento para o estilo dado.
// Se nenhum estilo for passado, usa ShareStyleTechnical (backward compat).
// Determinístico: mesmo input → mesmo output. Sempre ≤ shareMaxChars chars.
func GenerateShareText(s HistoryStats, styles ...ShareStyle) string {
	style := ShareStyleTechnical
	if len(styles) > 0 {
		style = styles[0]
	}
	return generateShareVariant(s, style)
}

func generateShareVariant(s HistoryStats, style ShareStyle) string {
	rate := int(s.ActivationRate() * 100)
	var text string

	switch style {
	case ShareStyleProvocative:
		text = fmt.Sprintf("Stopped wasting AI tokens. %d saved · %d%% efficiency",
			s.TotalTokensSaved, rate)
		if s.StreakDays >= 1 {
			text += fmt.Sprintf(" · 🔥 %d-day streak", s.StreakDays)
		}
		text += " — orbit.run"

	case ShareStyleMinimalist:
		lines := []string{
			fmt.Sprintf("%d tokens saved.", s.TotalTokensSaved),
			fmt.Sprintf("%d%% efficiency.", rate),
		}
		if s.StreakDays >= 1 {
			lines = append(lines, fmt.Sprintf("%d days straight.", s.StreakDays))
		}
		lines = append(lines, "orbit.run")
		text = strings.Join(lines, "\n")

	default: // ShareStyleTechnical
		text = fmt.Sprintf("🤖 orbit: %d tokens saved ($%.4f) · efficiency %d%%",
			s.TotalTokensSaved, s.TotalCostSavedUSD, rate)
		if s.StreakDays >= 1 {
			text += fmt.Sprintf(" · 🔥 %d-day streak", s.StreakDays)
		}
		text += " | orbit.run"
	}

	// Hard-truncate como segurança (não deve ocorrer em uso normal).
	if utf8.RuneCountInString(text) > shareMaxChars {
		runes := []rune(text)
		text = string(runes[:shareMaxChars])
	}
	return text
}

// ---------------------------------------------------------------------------
// Prometheus metric — share events
// ---------------------------------------------------------------------------

var statsSharedTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "orbit_stats_shared_total",
	Help: "Total de vezes que o usuário gerou texto de compartilhamento via orbit stats --share.",
})

// IncrementStatsShared registra um evento de compartilhamento.
func IncrementStatsShared() { statsSharedTotal.Inc() }

// RegisterShareMetrics registra o contador de share no registerer dado.
func RegisterShareMetrics(reg prometheus.Registerer) {
	reg.MustRegister(statsSharedTotal)
}
