// history.go — subcomando `orbit history`.
//
// Lista registros locais de execução em $ORBIT_HOME/logs/ com redaction
// obrigatória, ordenação mais-recente-primeiro e comportamento fail-closed
// em registros inválidos.
//
// Campos sensíveis (sensitiveHistoryFields) são redacted antes de qualquer
// renderização — inclusive em --json. Registros sem session_id ou timestamp
// são excluídos da tabela confiável e contados como inválidos.
//
// Exit codes:
//
//	0  registros válidos encontrados (ou diretório vazio)
//	1  diretório inacessível / erro de I/O
//	2  algum registro inválido encontrado (degradado parcialmente)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// sensitiveHistoryFields lista os campos JSON que contêm dados sensíveis
// e devem ser redacted antes de qualquer renderização.
// Atualizar quando um novo campo potencialmente sensível for adicionado
// ao RunResult e usado em history.
var sensitiveHistoryFields = []string{
	"output",
	"args",
	"guidance",
	"decision_reason",
}

// historyRecord é o subconjunto de RunResult lido pelo history.
// Campos desnecessários para exibição são omitidos da memória.
type historyRecord struct {
	Version        int      `json:"version"`
	SessionID      string   `json:"session_id"`
	Timestamp      string   `json:"timestamp"`
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	ExitCode       int      `json:"exit_code"`
	DurationMs     int64    `json:"duration_ms"`
	Criticality    string   `json:"criticality,omitempty"`
	Event          string   `json:"event,omitempty"`
	Decision       string   `json:"decision,omitempty"`
	DecisionReason string   `json:"decision_reason,omitempty"`
	Output         string   `json:"output,omitempty"`
	Guidance       string   `json:"guidance,omitempty"`
}

// exitCodeError transporta um exit code específico (!=1) sem poluir a
// mensagem de erro padrão do main. Detectado via errors.As em main.go.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string { return e.msg }

// historyRecordValid retorna true se o registro tem campos mínimos para
// ser exibido como confiável: session_id e timestamp não vazios.
func historyRecordValid(r historyRecord) bool {
	return r.SessionID != "" && r.Timestamp != ""
}

// redactHistoryRecord aplica redactOutput a todos os campos sensíveis.
// Opera em cópia — não modifica o original.
func redactHistoryRecord(r historyRecord) historyRecord {
	r.Output = redactOutput(r.Output)
	r.Guidance = redactOutput(r.Guidance)
	r.DecisionReason = redactOutput(r.DecisionReason)
	redacted := make([]string, len(r.Args))
	for i, a := range r.Args {
		redacted[i] = redactOutput(a)
	}
	r.Args = redacted
	return r
}

func historyStatus(r historyRecord) string {
	if r.ExitCode == 0 {
		return "OK"
	}
	return "FAIL"
}

func historyDuration(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000.0)
}

// historyTimestamp formata RFC3339 para exibição humana em hora local.
func historyTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
	}
	if err != nil {
		return ts
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// historySessionShort retorna até 12 chars do session_id.
func historySessionShort(sid string) string {
	if len(sid) <= 12 {
		return sid
	}
	return sid[:12] + "..."
}

// historyCommandSafe retorna o basename do comando, truncado em 20 chars.
// Não inclui args para evitar vazar paths e valores sensíveis.
func historyCommandSafe(cmd string) string {
	parts := strings.Split(cmd, "/")
	base := parts[len(parts)-1]
	if len(base) > 20 {
		return base[:17] + "..."
	}
	return base
}

func historyRisk(r historyRecord) string {
	if r.Criticality != "" {
		return r.Criticality
	}
	return "-"
}

// historyJSONRecord é o shape de cada entrada no output --json.
// Nunca inclui output bruto — decision_reason já está redacted.
type historyJSONRecord struct {
	SessionID      string `json:"session_id"`
	Timestamp      string `json:"timestamp"`
	Status         string `json:"status"`
	Command        string `json:"command"`
	DurationMs     int64  `json:"duration_ms"`
	Risk           string `json:"risk"`
	Event          string `json:"event,omitempty"`
	Decision       string `json:"decision,omitempty"`
	DecisionReason string `json:"decision_reason,omitempty"`
}

// historyJSONOutput é o envelope completo para --json.
type historyJSONOutput struct {
	Records         []historyJSONRecord  `json:"records"`
	Warnings        []historyJSONWarning `json:"warnings,omitempty"`
	IntegrityStatus string               `json:"integrity_status"`
}

type historyJSONWarning struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// runHistory é o entrypoint do subcomando.
func runHistory(args []string) error {
	return runHistoryTo(os.Stdout, os.Stderr, args)
}

// runHistoryTo é a forma testável: escreve em out/errOut em vez de os.Stdout/Stderr.
func runHistoryTo(out, errOut io.Writer, args []string) error {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	limit    := fs.Int("limit",  0,     "número máximo de registros a exibir (0 = todos)")
	failed   := fs.Bool("failed", false, "exibir apenas registros com exit_code != 0")
	jsonMode := fs.Bool("json",   false, "output JSON estruturado")
	detail   := fs.String("detail", "",  "exibir detalhes de um session_id (prefixo)")
	_ = fs.Parse(args)

	paths, err := ListExecutionLogs()
	if err != nil {
		return fmt.Errorf("history: listar logs: %w", err)
	}

	// Mais recente primeiro: inverte a ordem lexicográfica crescente.
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))

	var validRecords []historyRecord
	invalidCount := 0

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			invalidCount++
			continue
		}
		var r historyRecord
		if err := json.Unmarshal(data, &r); err != nil {
			invalidCount++
			continue
		}
		if !historyRecordValid(r) {
			invalidCount++
			continue
		}
		validRecords = append(validRecords, r)
	}

	// Filtros: --failed e --detail são aplicados antes do limit.
	filtered := make([]historyRecord, 0, len(validRecords))
	for _, r := range validRecords {
		if *failed && r.ExitCode == 0 {
			continue
		}
		if *detail != "" && !strings.HasPrefix(r.SessionID, *detail) {
			continue
		}
		filtered = append(filtered, r)
	}

	// --limit
	if *limit > 0 && len(filtered) > *limit {
		filtered = filtered[:*limit]
	}

	// Redaction obrigatória — aplicada antes de qualquer renderização.
	for i := range filtered {
		filtered[i] = redactHistoryRecord(filtered[i])
	}

	// Atalho: --detail sem resultado claro.
	if *detail != "" && len(filtered) == 0 {
		fmt.Fprintf(out, "Nenhum registro encontrado para session_id: %s\n", *detail)
		if invalidCount > 0 {
			fmt.Fprintf(errOut, "\n⚠  %d registro(s) ignorado(s) por falha de integridade.\n", invalidCount)
			fmt.Fprintf(errOut, "Execute: orbit verify --chain\n")
			return &exitCodeError{code: 2, msg: fmt.Sprintf("%d registro(s) inválido(s)", invalidCount)}
		}
		return nil
	}

	if *jsonMode {
		return renderHistoryJSON(out, errOut, filtered, invalidCount)
	}
	if *detail != "" && len(filtered) == 1 {
		return renderHistoryDetail(out, errOut, filtered[0], invalidCount)
	}
	return renderHistoryTable(out, errOut, filtered, invalidCount)
}

func renderHistoryTable(out, errOut io.Writer, records []historyRecord, invalidCount int) error {
	if len(records) == 0 && invalidCount == 0 {
		base, _ := tracking.ResolveStoreHome()
		fmt.Fprintf(out, "Nenhum registro encontrado.\n")
		fmt.Fprintf(out, "Diretório verificado: %s\n", base+"/"+logsDirName)
		return nil
	}

	fmt.Fprintf(out, "◆ orbit history\n")
	fmt.Fprintf(out, "%s\n", strings.Repeat("─", 72))

	if len(records) > 0 {
		fmt.Fprintf(out, "Registros: %d\n\n", len(records))
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tSTATUS\tCMD\tDURATION\tRISK\tSESSION")
		for _, r := range records {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				historyTimestamp(r.Timestamp),
				historyStatus(r),
				historyCommandSafe(r.Command),
				historyDuration(r.DurationMs),
				historyRisk(r),
				historySessionShort(r.SessionID),
			)
		}
		_ = w.Flush()
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Use: orbit history --detail <session_id>")
	}

	if invalidCount > 0 {
		fmt.Fprintf(errOut, "\n⚠  %d registro(s) ignorado(s) por falha de integridade.\n", invalidCount)
		fmt.Fprintf(errOut, "Execute: orbit verify --chain\n")
		return &exitCodeError{code: 2, msg: fmt.Sprintf("%d registro(s) inválido(s)", invalidCount)}
	}
	return nil
}

func renderHistoryDetail(out, errOut io.Writer, r historyRecord, invalidCount int) error {
	const maxOutputDisplay = 2000

	fmt.Fprintf(out, "◆ orbit history --detail\n")
	fmt.Fprintf(out, "%s\n", strings.Repeat("─", 72))
	fmt.Fprintf(out, "Session:    %s\n", r.SessionID)
	fmt.Fprintf(out, "Timestamp:  %s\n", historyTimestamp(r.Timestamp))
	fmt.Fprintf(out, "Status:     %s (exit_code=%d)\n", historyStatus(r), r.ExitCode)
	fmt.Fprintf(out, "Command:    %s\n", r.Command)
	if len(r.Args) > 0 {
		fmt.Fprintf(out, "Args:       %s\n", strings.Join(r.Args, " "))
	}
	fmt.Fprintf(out, "Duration:   %s\n", historyDuration(r.DurationMs))
	fmt.Fprintf(out, "Risk:       %s\n", historyRisk(r))
	fmt.Fprintf(out, "Event:      %s\n", r.Event)
	fmt.Fprintf(out, "Decision:   %s\n", r.Decision)
	if r.DecisionReason != "" {
		fmt.Fprintf(out, "Reason:     %s\n", r.DecisionReason)
	}
	if r.Output != "" {
		disp := r.Output
		if len(disp) > maxOutputDisplay {
			disp = disp[:maxOutputDisplay] + fmt.Sprintf("\n[TRUNCATED: %d chars omitidos]", len(r.Output)-maxOutputDisplay)
		}
		fmt.Fprintf(out, "\n--- Output (redacted) ---\n%s\n", disp)
	}

	if invalidCount > 0 {
		fmt.Fprintf(errOut, "\n⚠  %d registro(s) ignorado(s) por falha de integridade.\n", invalidCount)
		fmt.Fprintf(errOut, "Execute: orbit verify --chain\n")
		return &exitCodeError{code: 2, msg: fmt.Sprintf("%d registro(s) inválido(s)", invalidCount)}
	}
	return nil
}

func renderHistoryJSON(out, errOut io.Writer, records []historyRecord, invalidCount int) error {
	jsonRecs := make([]historyJSONRecord, 0, len(records))
	for _, r := range records {
		jsonRecs = append(jsonRecs, historyJSONRecord{
			SessionID:      r.SessionID,
			Timestamp:      r.Timestamp,
			Status:         historyStatus(r),
			Command:        historyCommandSafe(r.Command),
			DurationMs:     r.DurationMs,
			Risk:           historyRisk(r),
			Event:          r.Event,
			Decision:       r.Decision,
			DecisionReason: r.DecisionReason,
		})
	}

	integrityStatus := "OK"
	var warnings []historyJSONWarning
	if invalidCount > 0 {
		integrityStatus = "DEGRADED"
		warnings = []historyJSONWarning{{Type: "invalid_record", Count: invalidCount}}
	}

	payload := historyJSONOutput{
		Records:         jsonRecs,
		IntegrityStatus: integrityStatus,
		Warnings:        warnings,
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return fmt.Errorf("history: json encode: %w", err)
	}

	if invalidCount > 0 {
		return &exitCodeError{code: 2, msg: fmt.Sprintf("%d registro(s) inválido(s)", invalidCount)}
	}
	return nil
}
