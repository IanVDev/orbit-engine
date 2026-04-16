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
func runStats(host string) error {
	PrintSection("Orbit Stats")
	PrintKV("Servidor:", host)
	fmt.Println()

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
