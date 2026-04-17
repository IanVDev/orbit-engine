// history.go — persistência mínima do loop de valor do orbit.
//
// Escopo intencionalmente pequeno:
//   - Uma linha JSON por sessão em ~/.orbit/history.jsonl
//   - AppendHistory é fail-closed: se a gravação falhar, o caller aborta.
//   - ReadHistory é fail-soft: linhas inválidas são puladas e contadas.
//   - AggregateHistoryStats derive KPIs vendáveis (tokens saved, cost saved,
//     sessões com valor, taxa de ativação) sem lógica adicional.
//
// Coexiste com store.go (execuções de `orbit run`). Concerns distintas:
//   - store.go     → prova de execução de comando (output_bytes + proof)
//   - history.go   → valor gerado por sessão de skill (tokens/cost/activation)
package tracking

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// historyFileName é o arquivo JSONL do histórico de valor, dentro de $ORBIT_HOME.
const historyFileName = "history.jsonl"

// ---------------------------------------------------------------------------
// HistoryTime — timestamp sem age bounds para registros históricos
// ---------------------------------------------------------------------------

// HistoryTime é como FlexTime mas sem restrição de idade.
// FlexTime rejeita timestamps > 24h atrás (correto para eventos ao vivo).
// HistoryEntry precisa aceitar timestamps de meses atrás (registros em disco).
type HistoryTime struct{ time.Time }

// UnmarshalJSON aceita RFC3339/RFC3339Nano com timezone obrigatório,
// sem limite de idade. Normaliza para UTC.
func (ht *HistoryTime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		ht.Time = time.Time{}
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return fmt.Errorf("history: timestamp %q is not valid RFC3339 (timezone required)", s)
	}
	ht.Time = t.UTC()
	return nil
}

// MarshalJSON emite sempre RFC3339Nano em UTC.
func (ht HistoryTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(ht.Time.UTC().Format(time.RFC3339Nano))
}

// IsZero delega ao time.Time interno.
func (ht HistoryTime) IsZero() bool { return ht.Time.IsZero() }

// HistoryNowUTC retorna o instante atual como HistoryTime em UTC.
func HistoryNowUTC() HistoryTime { return HistoryTime{Time: time.Now().UTC()} }

// SchemaVersionHistory é a versão atual do schema de HistoryEntry.
// Incrementar APENAS quando mudanças incompatíveis forem introduzidas.
const SchemaVersionHistory = 1

// DefaultUSDPerMillionTokens é a estimativa default de custo ($ / 1M tokens).
// Calibrada para Claude Sonnet input. Override via env ORBIT_USD_PER_MTOK.
const DefaultUSDPerMillionTokens = 3.0

// HistoryEntry representa o valor entregue por uma sessão.
// Uma entrada por sessão (não por evento).
type HistoryEntry struct {
	SchemaVersion  int         `json:"schema_version"`
	SessionID      string      `json:"session_id"`
	Timestamp      HistoryTime `json:"timestamp"` // sem age bound — aceita registros históricos
	SkillActivated bool        `json:"skill_activated"`
	TokensSaved    int64       `json:"tokens_saved"`
	CostSavedUSD   float64     `json:"cost_saved_usd"`
	EventsCount    int         `json:"events_count"`
}

// Validate aplica invariantes mínimas. Fail-closed em AppendHistory.
func (e HistoryEntry) Validate() error {
	if e.SchemaVersion != SchemaVersionHistory {
		return fmt.Errorf("history: schema_version %d unsupported (want %d)",
			e.SchemaVersion, SchemaVersionHistory)
	}
	if e.SessionID == "" {
		return errors.New("history: session_id is required")
	}
	if e.Timestamp.IsZero() {
		return errors.New("history: timestamp is required")
	}
	if e.TokensSaved < 0 {
		return fmt.Errorf("history: tokens_saved must be >=0, got %d", e.TokensSaved)
	}
	if e.CostSavedUSD < 0 {
		return fmt.Errorf("history: cost_saved_usd must be >=0, got %f", e.CostSavedUSD)
	}
	return nil
}

// EstimateCostUSD calcula o custo economizado em USD dado um volume de tokens.
// Usa ORBIT_USD_PER_MTOK se definida; senão DefaultUSDPerMillionTokens.
func EstimateCostUSD(tokens int64) float64 {
	rate := DefaultUSDPerMillionTokens
	if v := strings.TrimSpace(os.Getenv("ORBIT_USD_PER_MTOK")); v != "" {
		var parsed float64
		if _, err := fmt.Sscanf(v, "%f", &parsed); err == nil && parsed >= 0 {
			rate = parsed
		}
	}
	if tokens <= 0 {
		return 0
	}
	return float64(tokens) * rate / 1_000_000.0
}

// historyPath retorna o caminho absoluto de history.jsonl.
func historyPath() (string, error) {
	base, err := ResolveStoreHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, historyFileName), nil
}

// ---------------------------------------------------------------------------
// AppendHistory — fail-closed
// ---------------------------------------------------------------------------

// AppendHistory persiste uma HistoryEntry no JSONL local.
//
// Fail-closed: qualquer falha incrementa historyWriteFailuresTotal e
// retorna erro. Se CostSavedUSD estiver zerado e TokensSaved > 0, o custo
// é estimado automaticamente via EstimateCostUSD.
func AppendHistory(entry HistoryEntry) (HistoryEntry, error) {
	entry.SchemaVersion = SchemaVersionHistory

	if entry.CostSavedUSD == 0 && entry.TokensSaved > 0 {
		entry.CostSavedUSD = EstimateCostUSD(entry.TokensSaved)
	}

	if err := entry.Validate(); err != nil {
		historyWriteFailuresTotal.Inc()
		return entry, err
	}

	base, err := ResolveStoreHome()
	if err != nil {
		historyWriteFailuresTotal.Inc()
		return entry, err
	}
	if err := ensureStoreDir(base); err != nil {
		historyWriteFailuresTotal.Inc()
		return entry, err
	}
	path := filepath.Join(base, historyFileName)

	data, err := json.Marshal(entry)
	if err != nil {
		historyWriteFailuresTotal.Inc()
		return entry, fmt.Errorf("history: marshal: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		historyWriteFailuresTotal.Inc()
		return entry, fmt.Errorf("history: open %q: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		historyWriteFailuresTotal.Inc()
		return entry, fmt.Errorf("history: write %q: %w", path, err)
	}
	if os.Getenv("ORBIT_FSYNC") == "1" {
		if err := f.Sync(); err != nil {
			_ = f.Close()
			historyWriteFailuresTotal.Inc()
			return entry, fmt.Errorf("history: fsync %q: %w", path, err)
		}
	}
	if err := f.Close(); err != nil {
		historyWriteFailuresTotal.Inc()
		return entry, fmt.Errorf("history: close %q: %w", path, err)
	}

	historyEntriesWrittenTotal.Inc()
	return entry, nil
}

// ---------------------------------------------------------------------------
// ReadHistory + AggregateHistoryStats — fail-soft
// ---------------------------------------------------------------------------

// ReadHistory carrega todas as entries válidas do JSONL local.
//
// Fail-soft: linhas inválidas são puladas e contadas via métrica
// historyCorruptedLinesTotal. Ausência de arquivo retorna slice vazio
// sem erro. Erros de I/O (filesystem) são propagados.
//
// Retorna as entries em ordem cronológica de arquivo (append order).
func ReadHistory() ([]HistoryEntry, error) {
	path, err := historyPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []HistoryEntry{}, nil
		}
		return nil, fmt.Errorf("history: open %q: %w", path, err)
	}
	defer f.Close()
	return readHistoryFromReader(f)
}

// readHistoryFromReader é fator testável de ReadHistory.
func readHistoryFromReader(r io.Reader) ([]HistoryEntry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	entries := make([]HistoryEntry, 0, 64)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			historyCorruptedLinesTotal.Inc()
			continue
		}
		if err := e.Validate(); err != nil {
			historyCorruptedLinesTotal.Inc()
			continue
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return entries, fmt.Errorf("history: scan: %w", err)
	}
	historyEntriesReadTotal.Add(float64(len(entries)))
	return entries, nil
}

// HistoryStats é o agregado vendável exibido pelo `orbit stats`.
type HistoryStats struct {
	Path              string
	TotalSessions     int
	ActivatedSessions int
	SessionsWithValue int
	TotalTokensSaved  int64
	TotalCostSavedUSD float64
	// Progressão temporal (calculada por AggregateHistoryStatsAt).
	TodayTokensSaved   int64 // tokens economizados no dia atual (UTC)
	PrevDayTokensSaved int64 // tokens economizados no dia anterior (UTC)
	StreakDays         int   // dias consecutivos com valor (TokensSaved > 0), do mais recente
}

// ActivationRate retorna ActivatedSessions / TotalSessions, ou 0 se não houver
// sessões (evita divisão por zero — fail-closed conceitual).
func (s HistoryStats) ActivationRate() float64 {
	if s.TotalSessions == 0 {
		return 0
	}
	return float64(s.ActivatedSessions) / float64(s.TotalSessions)
}

// TodayVariationPct retorna a variação percentual de tokens hoje vs ontem.
// Retorna 0 quando ontem==0 (evita divisão por zero); o caller usa
// HasPrevDay() para distinguir "sem ontem" de "variação zero".
func (s HistoryStats) TodayVariationPct() float64 {
	if s.PrevDayTokensSaved == 0 {
		return 0
	}
	return float64(s.TodayTokensSaved-s.PrevDayTokensSaved) / float64(s.PrevDayTokensSaved) * 100
}

// HasPrevDay retorna true se havia atividade no dia anterior.
func (s HistoryStats) HasPrevDay() bool { return s.PrevDayTokensSaved > 0 }

// AggregateHistoryStats reduz um slice de entries ao resumo de valor.
// Usa time.Now().UTC() como referência temporal; use AggregateHistoryStatsAt
// para injetar um clock controlado em testes.
func AggregateHistoryStats(entries []HistoryEntry) HistoryStats {
	return AggregateHistoryStatsAt(entries, time.Now().UTC())
}

// AggregateHistoryStatsAt é a versão com clock injetável de AggregateHistoryStats.
// Função pura — não lê disco, não retorna erro.
func AggregateHistoryStatsAt(entries []HistoryEntry, now time.Time) HistoryStats {
	today := now.Truncate(24 * time.Hour)
	yesterday := today.Add(-24 * time.Hour)

	// dayKey normaliza um timestamp para "YYYY-MM-DD" em UTC.
	dayKey := func(t time.Time) string { return t.UTC().Format("2006-01-02") }
	todayKey := dayKey(today)
	yesterdayKey := dayKey(yesterday)

	// dayValueMap: YYYY-MM-DD → true se alguma entry naquele dia tem TokensSaved > 0.
	dayValueMap := make(map[string]bool)

	s := HistoryStats{}
	for _, e := range entries {
		s.TotalSessions++
		if e.SkillActivated {
			s.ActivatedSessions++
		}
		if e.TokensSaved > 0 {
			s.SessionsWithValue++
		}
		s.TotalTokensSaved += e.TokensSaved
		s.TotalCostSavedUSD += e.CostSavedUSD

		k := dayKey(e.Timestamp.Time)
		switch k {
		case todayKey:
			s.TodayTokensSaved += e.TokensSaved
		case yesterdayKey:
			s.PrevDayTokensSaved += e.TokensSaved
		}
		if e.TokensSaved > 0 {
			dayValueMap[k] = true
		}
	}

	// Streak: dias consecutivos com valor, contados a partir de hoje para o passado.
	// Se hoje não tiver valor, streak = 0.
	if dayValueMap[todayKey] {
		// Coleta e ordena os dias com valor em ordem decrescente.
		days := make([]string, 0, len(dayValueMap))
		for d := range dayValueMap {
			days = append(days, d)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(days)))

		streak := 0
		expected := todayKey
		for _, d := range days {
			if d == expected {
				streak++
				// avança para o dia anterior
				t, _ := time.Parse("2006-01-02", expected)
				expected = dayKey(t.Add(-24 * time.Hour))
			} else if d < expected {
				break // gap — streak terminado
			}
			// d > expected: dia futuro (não deveria ocorrer) — ignorar
		}
		s.StreakDays = streak
	}

	return s
}

// LoadHistoryStats é o atalho usado pelo CLI: lê o JSONL e agrega.
// Preenche Path mesmo que não haja entries.
func LoadHistoryStats() (HistoryStats, error) {
	path, err := historyPath()
	if err != nil {
		return HistoryStats{}, err
	}
	entries, err := ReadHistory()
	if err != nil {
		return HistoryStats{Path: path}, err
	}
	stats := AggregateHistoryStats(entries)
	stats.Path = path
	return stats, nil
}

// ---------------------------------------------------------------------------
// Prometheus metrics — loop de valor
// ---------------------------------------------------------------------------

var (
	historyEntriesWrittenTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_history_entries_written_total",
			Help: "Total HistoryEntry records appended to the local history JSONL.",
		},
	)
	historyWriteFailuresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_history_write_failures_total",
			Help: "Total failures while appending to the local history JSONL (fail-closed).",
		},
	)
	historyEntriesReadTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_history_entries_read_total",
			Help: "Total valid HistoryEntry records read during aggregation.",
		},
	)
	historyCorruptedLinesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_history_corrupted_lines_total",
			Help: "Total invalid lines skipped while reading the local history JSONL.",
		},
	)
)

// RegisterHistoryMetrics registra os contadores do history no registerer dado.
// Mantido separado de RegisterMetrics para não invalidar v1_contract_test.go.
func RegisterHistoryMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		historyEntriesWrittenTotal,
		historyWriteFailuresTotal,
		historyEntriesReadTotal,
		historyCorruptedLinesTotal,
	)
}
