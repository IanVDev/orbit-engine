package tracking

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// -----------------------------------------------------------------------------
// TestStatsProgression
//
// ANTI-REGRESSION GATE da progressão temporal.
//
// Simula 4 dias de uso com clock injetado e valida:
//   - TodayTokensSaved e PrevDayTokensSaved isolados corretamente
//   - TodayVariationPct positiva/negativa/zero calculada certo
//   - StreakDays contando apenas dias consecutivos com valor
//   - Gap em dia interrompido quebra streak
//   - Sem dados hoje → streak 0, even que ontem tenha tido valor
// -----------------------------------------------------------------------------

func TestStatsProgression(t *testing.T) {
	// Clock de referência: "hoje" = 2026-04-16 UTC
	now := time.Date(2026, 4, 16, 15, 0, 0, 0, time.UTC)
	today := now.Truncate(24 * time.Hour)
	day := func(d int) HistoryTime { // helper: now + d days (negativo = passado)
		return HistoryTime{Time: today.Add(time.Duration(d) * 24 * time.Hour)}
	}

	t.Run("today_vs_prevday_variation_positive", func(t *testing.T) {
		entries := []HistoryEntry{
			{SchemaVersion: 1, SessionID: "s1", Timestamp: day(-1), SkillActivated: true, TokensSaved: 3000, CostSavedUSD: 0.009},
			{SchemaVersion: 1, SessionID: "s2", Timestamp: day(0), SkillActivated: true, TokensSaved: 5000, CostSavedUSD: 0.015},
		}
		s := AggregateHistoryStatsAt(entries, now)

		if s.TodayTokensSaved != 5000 {
			t.Fatalf("TodayTokensSaved=%d, want 5000", s.TodayTokensSaved)
		}
		if s.PrevDayTokensSaved != 3000 {
			t.Fatalf("PrevDayTokensSaved=%d, want 3000", s.PrevDayTokensSaved)
		}
		pct := s.TodayVariationPct()
		if math.Abs(pct-66.666) > 0.1 {
			t.Fatalf("TodayVariationPct=%.3f, want ~66.7%%", pct)
		}
		if !s.HasPrevDay() {
			t.Fatalf("HasPrevDay must be true when yesterday had tokens")
		}
	})

	t.Run("variation_negative", func(t *testing.T) {
		entries := []HistoryEntry{
			{SchemaVersion: 1, SessionID: "s1", Timestamp: day(-1), SkillActivated: true, TokensSaved: 6000},
			{SchemaVersion: 1, SessionID: "s2", Timestamp: day(0), SkillActivated: true, TokensSaved: 2000},
		}
		s := AggregateHistoryStatsAt(entries, now)
		pct := s.TodayVariationPct()
		if math.Abs(pct-(-66.666)) > 0.1 {
			t.Fatalf("variation must be negative ~-66.7%%, got %.3f", pct)
		}
	})

	t.Run("no_prev_day_today_has_value", func(t *testing.T) {
		entries := []HistoryEntry{
			{SchemaVersion: 1, SessionID: "s1", Timestamp: day(0), SkillActivated: true, TokensSaved: 4000},
		}
		s := AggregateHistoryStatsAt(entries, now)
		if s.HasPrevDay() {
			t.Fatalf("HasPrevDay must be false when no entries yesterday")
		}
		if s.TodayVariationPct() != 0 {
			t.Fatalf("TodayVariationPct must be 0 when no prev day, got %f", s.TodayVariationPct())
		}
	})

	t.Run("streak_consecutive_3_days", func(t *testing.T) {
		entries := []HistoryEntry{
			{SchemaVersion: 1, SessionID: "s1", Timestamp: day(-2), SkillActivated: true, TokensSaved: 1000},
			{SchemaVersion: 1, SessionID: "s2", Timestamp: day(-1), SkillActivated: true, TokensSaved: 2000},
			{SchemaVersion: 1, SessionID: "s3", Timestamp: day(0), SkillActivated: true, TokensSaved: 3000},
		}
		s := AggregateHistoryStatsAt(entries, now)
		if s.StreakDays != 3 {
			t.Fatalf("StreakDays=%d, want 3 (3 consecutive days with value)", s.StreakDays)
		}
	})

	t.Run("streak_broken_by_gap", func(t *testing.T) {
		// Dia -3 e -1 têm valor, mas dia -2 é gap → streak a partir de hoje deve parar em 2
		entries := []HistoryEntry{
			{SchemaVersion: 1, SessionID: "s1", Timestamp: day(-3), SkillActivated: true, TokensSaved: 500},
			// dia -2: sem entries → gap
			{SchemaVersion: 1, SessionID: "s2", Timestamp: day(-1), SkillActivated: true, TokensSaved: 1000},
			{SchemaVersion: 1, SessionID: "s3", Timestamp: day(0), SkillActivated: true, TokensSaved: 2000},
		}
		s := AggregateHistoryStatsAt(entries, now)
		if s.StreakDays != 2 {
			t.Fatalf("StreakDays=%d, want 2 (gap on day-2 breaks streak)", s.StreakDays)
		}
	})

	t.Run("streak_zero_when_no_value_today", func(t *testing.T) {
		// Ontem teve valor mas hoje não → streak = 0
		entries := []HistoryEntry{
			{SchemaVersion: 1, SessionID: "s1", Timestamp: day(-1), SkillActivated: true, TokensSaved: 2000},
			{SchemaVersion: 1, SessionID: "s2", Timestamp: day(0), SkillActivated: true, TokensSaved: 0}, // ativada mas sem valor
		}
		s := AggregateHistoryStatsAt(entries, now)
		if s.StreakDays != 0 {
			t.Fatalf("StreakDays=%d, want 0 (today has no value)", s.StreakDays)
		}
	})

	t.Run("session_skill_activated_no_tokens_does_not_count_for_streak", func(t *testing.T) {
		entries := []HistoryEntry{
			{SchemaVersion: 1, SessionID: "s1", Timestamp: day(0), SkillActivated: true, TokensSaved: 0},
		}
		s := AggregateHistoryStatsAt(entries, now)
		if s.StreakDays != 0 {
			t.Fatalf("StreakDays=%d, want 0 (activation without tokens is not value)", s.StreakDays)
		}
	})

	t.Run("total_tokens_and_cost_unaffected_by_progression", func(t *testing.T) {
		// Garante que campos existentes não mudam com a adição de progressão.
		entries := []HistoryEntry{
			{SchemaVersion: 1, SessionID: "s1", Timestamp: day(-5), SkillActivated: false, TokensSaved: 0},
			{SchemaVersion: 1, SessionID: "s2", Timestamp: day(-2), SkillActivated: true, TokensSaved: 1200, CostSavedUSD: 0.0036},
			{SchemaVersion: 1, SessionID: "s3", Timestamp: day(0), SkillActivated: true, TokensSaved: 800, CostSavedUSD: 0.0024},
		}
		s := AggregateHistoryStatsAt(entries, now)
		if s.TotalSessions != 3 {
			t.Fatalf("TotalSessions=%d, want 3", s.TotalSessions)
		}
		if s.TotalTokensSaved != 2000 {
			t.Fatalf("TotalTokensSaved=%d, want 2000", s.TotalTokensSaved)
		}
		if math.Abs(s.TotalCostSavedUSD-0.006) > 1e-9 {
			t.Fatalf("TotalCostSavedUSD=%f, want 0.006", s.TotalCostSavedUSD)
		}
	})
}

// TestHistoryLoopValueInvariant
//
// ANTI-REGRESSION GATE do loop de valor.
//
// Simula uma execução real:
//   1) 3 sessões: uma sem skill, uma ativa com valor, uma ativa sem valor.
//   2) Persiste via AppendHistory em $ORBIT_HOME/history.jsonl isolado.
//   3) Recupera via ReadHistory e valida round-trip completo.
//   4) AggregateHistoryStats deve bater com os totais esperados:
//        - TotalSessions, ActivatedSessions, SessionsWithValue
//        - TotalTokensSaved, TotalCostSavedUSD (via EstimateCostUSD)
//        - ActivationRate
//   5) Append inválido deve falhar-fechado (arquivo não cresce).
//   6) Linha corrompida no JSONL deve ser pulada em ReadHistory (fail-soft).
//
// Se este teste falhar, o comando `orbit stats` mente para o usuário.
// -----------------------------------------------------------------------------

func TestHistoryLoopValueInvariant(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)
	t.Setenv("ORBIT_USD_PER_MTOK", "3") // trava custo em $3/M tokens

	// --- 1. Simula 3 sessões ---

	e1, err := AppendHistory(HistoryEntry{
		SessionID:      "sess-no-skill",
		Timestamp:      HistoryNowUTC(),
		SkillActivated: false,
		TokensSaved:    0,
		EventsCount:    8,
	})
	if err != nil {
		t.Fatalf("append e1 failed: %v", err)
	}
	if e1.CostSavedUSD != 0 {
		t.Fatalf("e1 cost must be 0 for no-skill session, got %f", e1.CostSavedUSD)
	}

	e2, err := AppendHistory(HistoryEntry{
		SessionID:      "sess-skill-value",
		Timestamp:      HistoryNowUTC(),
		SkillActivated: true,
		TokensSaved:    5_000, // => custo = 5000 * 3 / 1M = 0.015
		EventsCount:    12,
	})
	if err != nil {
		t.Fatalf("append e2 failed: %v", err)
	}
	// Auto-cálculo de custo deve ter preenchido.
	wantCost := 0.015
	if math.Abs(e2.CostSavedUSD-wantCost) > 1e-9 {
		t.Fatalf("e2 auto-cost mismatch: got %f want %f", e2.CostSavedUSD, wantCost)
	}

	e3, err := AppendHistory(HistoryEntry{
		SessionID:      "sess-skill-novalue",
		Timestamp:      HistoryNowUTC(),
		SkillActivated: true,
		TokensSaved:    0,
		EventsCount:    3,
	})
	if err != nil {
		t.Fatalf("append e3 failed: %v", err)
	}
	if e3.SessionID == "" {
		t.Fatalf("e3 returned without session_id")
	}

	// --- 2. Valida arquivo físico ---

	path := filepath.Join(tmp, "history.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	// Round-trip de uma linha arbitrária.
	var roundTrip HistoryEntry
	if err := json.Unmarshal([]byte(lines[1]), &roundTrip); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	if roundTrip.SessionID != "sess-skill-value" {
		t.Fatalf("round-trip session_id mismatch: %q", roundTrip.SessionID)
	}
	if roundTrip.SchemaVersion != SchemaVersionHistory {
		t.Fatalf("round-trip schema_version=%d, want %d",
			roundTrip.SchemaVersion, SchemaVersionHistory)
	}

	// --- 3. ReadHistory recupera tudo ---

	entries, err := ReadHistory()
	if err != nil {
		t.Fatalf("ReadHistory failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("ReadHistory returned %d entries, want 3", len(entries))
	}

	// --- 4. Aggregate stats exatos ---

	stats := AggregateHistoryStats(entries)
	if stats.TotalSessions != 3 {
		t.Fatalf("TotalSessions=%d, want 3", stats.TotalSessions)
	}
	if stats.ActivatedSessions != 2 {
		t.Fatalf("ActivatedSessions=%d, want 2", stats.ActivatedSessions)
	}
	if stats.SessionsWithValue != 1 {
		t.Fatalf("SessionsWithValue=%d, want 1", stats.SessionsWithValue)
	}
	if stats.TotalTokensSaved != 5_000 {
		t.Fatalf("TotalTokensSaved=%d, want 5000", stats.TotalTokensSaved)
	}
	if math.Abs(stats.TotalCostSavedUSD-0.015) > 1e-9 {
		t.Fatalf("TotalCostSavedUSD=%f, want 0.015", stats.TotalCostSavedUSD)
	}
	if rate := stats.ActivationRate(); math.Abs(rate-2.0/3.0) > 1e-9 {
		t.Fatalf("ActivationRate=%f, want %f", rate, 2.0/3.0)
	}

	// LoadHistoryStats (atalho do CLI) deve retornar o mesmo.
	loaded, err := LoadHistoryStats()
	if err != nil {
		t.Fatalf("LoadHistoryStats failed: %v", err)
	}
	if loaded.TotalSessions != 3 || loaded.TotalTokensSaved != 5_000 {
		t.Fatalf("LoadHistoryStats mismatch: %+v", loaded)
	}
	if loaded.Path != path {
		t.Fatalf("LoadHistoryStats.Path=%q, want %q", loaded.Path, path)
	}

	// --- 5. FAIL-CLOSED: entry inválida não persiste ---

	sizeBefore, _ := os.Stat(path)
	_, err = AppendHistory(HistoryEntry{
		// missing SessionID + Timestamp → Validate() falha
	})
	if err == nil {
		t.Fatalf("AppendHistory must fail-closed on invalid entry")
	}
	sizeAfter, _ := os.Stat(path)
	if sizeBefore.Size() != sizeAfter.Size() {
		t.Fatalf("file grew after invalid append: before=%d after=%d",
			sizeBefore.Size(), sizeAfter.Size())
	}

	// Tokens negativos → fail-closed também.
	_, err = AppendHistory(HistoryEntry{
		SessionID:   "bad",
		Timestamp:   HistoryNowUTC(),
		TokensSaved: -1,
	})
	if err == nil {
		t.Fatalf("AppendHistory must reject negative tokens_saved")
	}

	// --- 6. FAIL-SOFT: linha corrompida é pulada ---

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("reopen for corruption: %v", err)
	}
	if _, err := f.WriteString("this is not json\n"); err != nil {
		t.Fatalf("write corruption: %v", err)
	}
	_ = f.Close()

	entriesAfterCorrupt, err := ReadHistory()
	if err != nil {
		t.Fatalf("ReadHistory must be fail-soft, got error: %v", err)
	}
	if len(entriesAfterCorrupt) != 3 {
		t.Fatalf("after corruption got %d entries, want 3 (bad line must be skipped)",
			len(entriesAfterCorrupt))
	}
}
