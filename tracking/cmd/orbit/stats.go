// stats.go — exibe métricas de uso do orbit-engine a partir de /metrics.
//
// Conecta ao tracking-server (default: http://localhost:9100), lê o endpoint
// /metrics no formato texto Prometheus e exibe os KPIs mais relevantes:
//   - Execuções (skill_activations_total)
//   - Tokens processados (tokens_used_total)
//   - Decisões automáticas (decisions_total)
//   - Sessões únicas (session_count)
//   - Valor percebido (user_perceived_value_total)
//   - Usuários que voltaram (user_returned_total)
//
// Fail-closed: retorna error se o servidor não responder ou HTTP != 200.
package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// displayMetric descreve uma métrica que deve ser exibida no painel.
type displayMetric struct {
	prefix string // prefixo do nome Prometheus (sem labels)
	label  string // rótulo em português para exibição
	unit   string // ex: "tokens", "execuções", "" para genérico
}

// metricsPanel define a ordem e o conteúdo do painel de stats.
var metricsPanel = []displayMetric{
	{"orbit_skill_activations_total", "Execuções totais", ""},
	{"orbit_tokens_used_total", "Tokens processados", "tokens"},
	{"orbit_decisions_total", "Decisões automáticas", ""},
	{"orbit_session_count", "Sessões únicas", ""},
	{"orbit_user_perceived_value_total", "Eventos de valor percebido", ""},
	{"orbit_user_returned_total", "Usuários que voltaram", ""},
	{"orbit_user_accepted_suggestion_total", "Sugestões aceitas", ""},
	{"orbit_user_ignore_reason_total", "Sugestões ignoradas", ""},
	{"orbit_heartbeat_total", "Heartbeats (uptime indicator)", ""},
}

// runStats conecta ao tracking-server e exibe os KPIs.
// Se share=true, exibe apenas o snippet de compartilhamento e retorna sem
// acessar o servidor (dados puramente locais).
func runStats(host string, share bool) error {
	if err := enforceRuntimePathIntegrity(); err != nil {
		return err
	}

	PrintSection("Orbit Stats")
	fmt.Println()

	if share {
		return printSharePanel()
	}

	PrintKV("Servidor:", host)
	fmt.Println()

	// --- Loop de valor local (JSONL history) ---
	// Fail-soft: mesmo se o servidor estiver down, o usuário vê o que tem.
	printHistoryPanel()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(host + "/metrics")
	if err != nil {
		return fmt.Errorf(
			"não foi possível conectar ao servidor:\n"+
				"   %w\n\n"+
				"   Certifique-se de que o tracking-server está rodando.\n"+
				"   Dica: execute 'orbit quickstart' primeiro.",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/metrics retornou HTTP %d", resp.StatusCode)
	}

	values := sumMetrics(resp.Body)

	found := 0
	for _, dm := range metricsPanel {
		val, ok := values[dm.prefix]
		if !ok {
			continue
		}
		found++
		suffix := ""
		if dm.unit != "" {
			suffix = " " + dm.unit
		}
		PrintKV(dm.label+":", fmtFloat(val)+suffix)
	}

	fmt.Println()
	if found == 0 {
		PrintWarn("Nenhuma métrica encontrada.")
		PrintTip("Execute 'orbit quickstart' para gerar o primeiro evento.")
	} else {
		PrintSuccess(fmt.Sprintf("%d métrica(s) exibida(s)", found))
		PrintTip("Use 'orbit run <cmd>' para registrar execuções com proof.")
	}
	fmt.Println()
	return nil
}

// printHistoryPanel exibe o agregado de valor lido de $ORBIT_HOME/history.jsonl.
// Fail-soft: em caso de erro de I/O, emite WARN e continua sem abortar o stats.
// Fail-closed: se history vazio, mostra mensagem clara e não exibe linha zerada.
func printHistoryPanel() {
	stats, err := tracking.LoadHistoryStats()
	if err != nil {
		PrintWarn("Histórico local indisponível: " + err.Error())
		fmt.Println()
		return
	}
	if stats.TotalSessions == 0 {
		PrintKV("Histórico local:", "vazio")
		PrintTip("Ainda sem sessões — rode o orbit e gere atividade primeiro.")
		fmt.Println()
		return
	}

	PrintKV("Histórico local:", stats.Path)
	PrintKV("Sessões totais:", fmt.Sprintf("%d", stats.TotalSessions))
	PrintKV("Sessões com skill ativa:", fmt.Sprintf("%d", stats.ActivatedSessions))
	PrintKV("Sessões com valor:", fmt.Sprintf("%d", stats.SessionsWithValue))
	PrintKV("Taxa de ativação:", fmt.Sprintf("%.1f%%", stats.ActivationRate()*100))

	// Tokens: total + progressão do dia
	tokenLine := fmt.Sprintf("%d total", stats.TotalTokensSaved)
	if stats.TodayTokensSaved > 0 || stats.HasPrevDay() {
		todayPart := fmt.Sprintf("+%d hoje", stats.TodayTokensSaved)
		if stats.HasPrevDay() {
			pct := stats.TodayVariationPct()
			arrow := "→"
			switch {
			case pct > 0:
				arrow = "↑"
			case pct < 0:
				arrow = "↓"
			}
			todayPart += fmt.Sprintf("  %s %.0f%% vs ontem", arrow, pct)
		}
		tokenLine += "  |  " + todayPart
	}
	PrintKV("Tokens economizados:", tokenLine)
	PrintKV("Custo economizado:", fmt.Sprintf("$%.4f USD", stats.TotalCostSavedUSD))

	// Streak
	switch {
	case stats.StreakDays >= 7:
		PrintSuccess(fmt.Sprintf("🔥 Streak: %d dias gerando valor — sequência excepcional", stats.StreakDays))
	case stats.StreakDays >= 3:
		PrintSuccess(fmt.Sprintf("🔥 Streak: %d dias gerando valor", stats.StreakDays))
	case stats.StreakDays >= 1:
		PrintKV("Streak:", fmt.Sprintf("%d dia(s) com valor", stats.StreakDays))
	}

	fmt.Println()
}

// printSharePanel exibe as 3 variações de texto de compartilhamento.
// Fail-closed se histórico vazio ou inacessível.
func printSharePanel() error {
	stats, err := tracking.LoadHistoryStats()
	if err != nil {
		PrintWarn("Histórico local indisponível: " + err.Error())
		return err
	}
	if stats.TotalSessions == 0 {
		PrintWarn("Sem dados para compartilhar — rode o orbit e gere atividade primeiro.")
		PrintTip("Execute 'orbit quickstart' para gerar o primeiro evento.")
		fmt.Println()
		return nil
	}

	tracking.IncrementStatsShared()

	for _, style := range tracking.AllShareStyles {
		label := tracking.ShareStyleLabel(style)
		PrintDivider()
		fmt.Printf("  %s\n\n", label)
		text := tracking.GenerateShareText(stats, style)
		// Indenta cada linha (minimalista usa \n internamente).
		for _, line := range strings.Split(text, "\n") {
			fmt.Println("  " + line)
		}
		fmt.Println()
	}

	PrintDivider()
	PrintTip("Copie a versão que ressoa com você e compartilhe.")
	fmt.Println()
	return nil
}

// sumMetrics lê o formato texto do Prometheus e retorna
// metricName → soma de todos os valores de todas as séries daquela métrica.
// Linhas de comentário (# …) são ignoradas.
func sumMetrics(r io.Reader) map[string]float64 {
	result := make(map[string]float64)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		// Formato: metric_name[{labels}] value [timestamp]
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		// Remove labels: orbit_foo{x="y"} → orbit_foo
		if idx := strings.IndexByte(name, '{'); idx >= 0 {
			name = name[:idx]
		}
		val, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		result[name] += val
	}
	return result
}

// fmtFloat formata um float64 sem casas decimais quando é inteiro,
// ou com 4 casas decimais para valores fracionários.
func fmtFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%.4f", f)
}
