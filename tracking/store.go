// store.go — persistência local em JSONL append-only para execuções do orbit.
//
// Invariantes:
//   - Write é fail-closed: se append falhar, o caller DEVE abortar.
//   - Read é fail-soft: linhas inválidas são puladas e contadas; a função
//     nunca retorna erro por corrupção de linha isolada.
//   - Cada linha carrega schema_version explícito para forward-compat.
//   - prev_proof forma uma chain simples contra reordenação/edição manual.
//
// Formato: JSONL (uma SessionRecord por linha, separada por '\n').
// Path: $ORBIT_HOME/sessions.jsonl (default ~/.orbit/sessions.jsonl).
// Permissões: dir 0o700, arquivo 0o600 — conteúdo pode vazar comandos.
package tracking

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// SchemaVersionStore é a versão atual do schema escrita em cada linha.
// Incrementar APENAS quando adicionar campos incompatíveis.
const SchemaVersionStore = 1

// storeFileName é o nome fixo do arquivo JSONL dentro de $ORBIT_HOME.
const storeFileName = "sessions.jsonl"

// SessionRecord é o que fica persistido por execução de `orbit run`.
// Serializado como JSON em uma única linha.
type SessionRecord struct {
	SchemaVersion int      `json:"schema_version"`
	SessionID     string   `json:"session_id"`
	Timestamp     FlexTime `json:"timestamp"`
	Command       string   `json:"command"`
	Args          []string `json:"args,omitempty"`
	ExitCode      int      `json:"exit_code"`
	OutputBytes   int64    `json:"output_bytes"`
	Proof         string   `json:"proof"`
	PrevProof     string   `json:"prev_proof,omitempty"`
}

// Validate retorna erro se campos obrigatórios estiverem vazios.
// Fail-closed: chamado antes de cada append e depois de cada read.
func (r SessionRecord) Validate() error {
	if r.SchemaVersion != SchemaVersionStore {
		return fmt.Errorf("store: schema_version %d unsupported (want %d)",
			r.SchemaVersion, SchemaVersionStore)
	}
	if r.SessionID == "" {
		return errors.New("store: session_id is required")
	}
	if r.Proof == "" {
		return errors.New("store: proof is required")
	}
	if r.Command == "" {
		return errors.New("store: command is required")
	}
	if r.Timestamp.IsZero() {
		return errors.New("store: timestamp is required")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Path resolution
// ---------------------------------------------------------------------------

// ResolveStoreHome retorna o diretório base de dados locais do orbit.
// Preferência: ORBIT_HOME > $HOME/.orbit.
// Retorna erro se nenhum dos dois puder ser resolvido (fail-closed).
func ResolveStoreHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv("ORBIT_HOME")); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("store: cannot resolve home dir: %w", err)
	}
	return filepath.Join(home, ".orbit"), nil
}

// StorePath retorna o caminho absoluto do arquivo JSONL.
func StorePath() (string, error) {
	base, err := ResolveStoreHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, storeFileName), nil
}

// ensureStoreDir cria $ORBIT_HOME com permissões 0o700 se necessário.
func ensureStoreDir(base string) error {
	info, err := os.Stat(base)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("store: %q exists and is not a directory", base)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store: stat %q: %w", base, err)
	}
	return os.MkdirAll(base, 0o700)
}

// ---------------------------------------------------------------------------
// Append (fail-closed)
// ---------------------------------------------------------------------------

// AppendSessionRecord persiste um registro no JSONL local.
//
// Regras:
//   - Se prev_proof não estiver preenchido, é resolvido a partir da última
//     linha válida do arquivo (ou "" para genesis).
//   - Falha em qualquer etapa incrementa storeWriteFailuresTotal e retorna
//     erro — o caller DEVE propagar o erro (fail-closed).
//
// Retorna o registro enriquecido com PrevProof e SchemaVersion.
func AppendSessionRecord(rec SessionRecord) (SessionRecord, error) {
	rec.SchemaVersion = SchemaVersionStore

	if err := rec.Validate(); err != nil {
		storeWriteFailuresTotal.Inc()
		return rec, err
	}

	base, err := ResolveStoreHome()
	if err != nil {
		storeWriteFailuresTotal.Inc()
		return rec, err
	}
	if err := ensureStoreDir(base); err != nil {
		storeWriteFailuresTotal.Inc()
		return rec, err
	}
	path := filepath.Join(base, storeFileName)

	// Resolve prev_proof a partir do último registro válido no arquivo.
	if rec.PrevProof == "" {
		prev, lastErr := lastValidProof(path)
		if lastErr != nil {
			storeWriteFailuresTotal.Inc()
			return rec, lastErr
		}
		rec.PrevProof = prev
	}

	// Serializa.
	data, err := json.Marshal(rec)
	if err != nil {
		storeWriteFailuresTotal.Inc()
		return rec, fmt.Errorf("store: marshal: %w", err)
	}
	data = append(data, '\n')

	// O_APPEND garante atomicidade POSIX para writes < PIPE_BUF (4096B),
	// que é o caso típico de um SessionRecord.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		storeWriteFailuresTotal.Inc()
		return rec, fmt.Errorf("store: open %q: %w", path, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		storeWriteFailuresTotal.Inc()
		return rec, fmt.Errorf("store: write %q: %w", path, err)
	}

	// fsync explícito se ORBIT_FSYNC=1 (mais lento, durabilidade total).
	if os.Getenv("ORBIT_FSYNC") == "1" {
		if err := f.Sync(); err != nil {
			_ = f.Close()
			storeWriteFailuresTotal.Inc()
			return rec, fmt.Errorf("store: fsync %q: %w", path, err)
		}
	}

	if err := f.Close(); err != nil {
		storeWriteFailuresTotal.Inc()
		return rec, fmt.Errorf("store: close %q: %w", path, err)
	}

	storeRecordsWrittenTotal.Inc()
	return rec, nil
}

// lastValidProof lê o arquivo do início ao fim e retorna o proof da última
// linha válida (ou "" se o arquivo não existir / estiver vazio / todas as
// linhas forem inválidas). Nunca retorna erro para arquivo inexistente.
func lastValidProof(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("store: open %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Linhas grandes são evitadas por design; 1 MiB é limite seguro.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	last := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r SessionRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		if err := r.Validate(); err != nil {
			continue
		}
		last = r.Proof
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("store: scan %q: %w", path, err)
	}
	return last, nil
}

// ---------------------------------------------------------------------------
// Read + aggregate (fail-soft)
// ---------------------------------------------------------------------------

// StoreStats é o agregado que `orbit stats` exibe a partir do JSONL local.
type StoreStats struct {
	Path             string
	TotalRecords     int
	Successful       int
	OutputBytesTotal int64
	ChainBreaks      int
	Corrupted        int
	Last             []SessionRecord // ordem cronológica; cap = cfg.LastN
}

// ReadStats percorre o JSONL e agrega métricas.
//
// Fail-soft: linhas inválidas são ignoradas e contadas em Corrupted; erros
// de I/O (ex: arquivo aberto concorrentemente) retornam erro. Ausência de
// arquivo retorna StoreStats vazio sem erro.
func ReadStats(lastN int) (StoreStats, error) {
	if lastN <= 0 {
		lastN = 5
	}
	stats := StoreStats{}

	path, err := StorePath()
	if err != nil {
		return stats, err
	}
	stats.Path = path

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return stats, nil
		}
		return stats, fmt.Errorf("store: open %q: %w", path, err)
	}
	defer f.Close()

	return aggregateFromReader(f, stats, lastN)
}

// aggregateFromReader é fator testável de ReadStats.
func aggregateFromReader(r io.Reader, stats StoreStats, lastN int) (StoreStats, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	prevProof := ""
	ring := make([]SessionRecord, 0, lastN)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec SessionRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			stats.Corrupted++
			storeCorruptedLinesTotal.Inc()
			continue
		}
		if err := rec.Validate(); err != nil {
			stats.Corrupted++
			storeCorruptedLinesTotal.Inc()
			continue
		}

		// Chain integrity: prev_proof da linha N deve bater com proof da N-1.
		if prevProof != "" && rec.PrevProof != prevProof {
			stats.ChainBreaks++
			storeChainBreaksTotal.Inc()
		}
		prevProof = rec.Proof

		stats.TotalRecords++
		if rec.ExitCode == 0 {
			stats.Successful++
		}
		stats.OutputBytesTotal += rec.OutputBytes

		// Ring buffer das últimas N.
		if len(ring) < lastN {
			ring = append(ring, rec)
		} else {
			copy(ring, ring[1:])
			ring[len(ring)-1] = rec
		}
	}
	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("store: scan: %w", err)
	}

	storeRecordsReadTotal.Add(float64(stats.TotalRecords))
	stats.Last = ring
	return stats, nil
}

// ---------------------------------------------------------------------------
// Prometheus metrics — loop de valor local
// ---------------------------------------------------------------------------

var (
	storeRecordsWrittenTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_store_records_written_total",
			Help: "Total session records appended to the local JSONL store.",
		},
	)
	storeWriteFailuresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_store_write_failures_total",
			Help: "Total failures while appending to the local JSONL store (fail-closed path).",
		},
	)
	storeRecordsReadTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_store_records_read_total",
			Help: "Total valid session records read from the local JSONL store during aggregation.",
		},
	)
	storeCorruptedLinesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_store_corrupted_lines_total",
			Help: "Total invalid lines skipped while reading the local JSONL store (fail-soft).",
		},
	)
	storeChainBreaksTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orbit_store_chain_breaks_total",
			Help: "Total prev_proof chain breaks detected while reading the local JSONL store.",
		},
	)
)

// RegisterStoreMetrics registra os contadores do store no registerer dado.
// Mantido separado de RegisterMetrics (não-invasivo) para evitar alterar
// o contract test existente. Deve ser chamado uma vez por processo.
func RegisterStoreMetrics(reg prometheus.Registerer) {
	reg.MustRegister(
		storeRecordsWrittenTotal,
		storeWriteFailuresTotal,
		storeRecordsReadTotal,
		storeCorruptedLinesTotal,
		storeChainBreaksTotal,
	)
}
