// verify.go — comando `orbit verify <log_file>`.
//
// Recomputa o proof de uma execução previamente registrada em
// $ORBIT_HOME/logs/ e o compara com o campo `proof` armazenado.
// Fecha o pilar de "prove" do produto: o registro não é só assinado,
// é re-validável a qualquer momento por qualquer caller.
//
// Contrato (fail-closed):
//   - arquivo inexistente / ilegível             → exit 1
//   - JSON inválido                              → exit 1
//   - campos essenciais ausentes                 → exit 1
//   - hash recomputado != proof armazenado       → exit 1
//   - match exato                                → exit 0
//
// O hash é a mesma função usada por `orbit run`:
//
//	sha256(session_id + "|" + timestamp.RFC3339Nano + "|" + output_bytes)
//
// Mudar a fórmula em qualquer lado quebra todos os logs históricos —
// por isso ambas as chamadas usam tracking.ComputeHash.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

// verifyRecord é o subset do RunResult necessário para recomputar o proof.
// Mantido separado para isolar o verify de mudanças cosméticas no schema:
// só falha se um destes 4 campos sumir, não se um campo novo aparecer.
type verifyRecord struct {
	Proof       string `json:"proof"`
	SessionID   string `json:"session_id"`
	Timestamp   string `json:"timestamp"`
	OutputBytes int64  `json:"output_bytes"`
}

// runVerify é o entrypoint do subcomando. Imprime resultado em w (stdout
// por padrão) e devolve erro fail-closed quando o log não confere.
func runVerify(logPath string) error {
	return verifyTo(os.Stdout, logPath)
}

// verifyTo é a forma testável: escreve em w em vez de os.Stdout.
func verifyTo(w io.Writer, logPath string) error {
	if logPath == "" {
		return fmt.Errorf("verify: caminho do log é obrigatório")
	}

	f, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("verify: não consegui abrir %q: %w", logPath, err)
	}
	defer f.Close()

	rec, err := decodeVerifyRecord(f)
	if err != nil {
		return fmt.Errorf("verify: %s: %w", logPath, err)
	}

	expected, err := recomputeProof(rec)
	if err != nil {
		return fmt.Errorf("verify: %s: %w", logPath, err)
	}

	if expected != rec.Proof {
		// Mostra prefixos curtos — proof completo polui terminal e o
		// diff visual já é decisivo nos primeiros 16 chars.
		fmt.Fprintf(w, "❌  proof mismatch em %s\n", logPath)
		fmt.Fprintf(w, "    armazenado: %s...\n", safePrefix(rec.Proof, 32))
		fmt.Fprintf(w, "    recomputado: %s...\n", safePrefix(expected, 32))
		return fmt.Errorf("proof mismatch — log adulterado ou schema divergente")
	}

	fmt.Fprintf(w, "✅  proof confere — %s\n", logPath)
	fmt.Fprintf(w, "    sha256: %s\n", expected)
	fmt.Fprintf(w, "    session: %s · ts: %s · bytes: %d\n",
		rec.SessionID, rec.Timestamp, rec.OutputBytes)
	return nil
}

// decodeVerifyRecord lê o JSON e valida apenas o subset usado no proof.
// Fail-closed: ausência de qualquer campo essencial é erro.
func decodeVerifyRecord(r io.Reader) (verifyRecord, error) {
	var rec verifyRecord
	dec := json.NewDecoder(r)
	if err := dec.Decode(&rec); err != nil {
		return rec, fmt.Errorf("JSON inválido: %w", err)
	}
	if rec.Proof == "" {
		return rec, fmt.Errorf("campo essencial ausente: proof")
	}
	if rec.SessionID == "" {
		return rec, fmt.Errorf("campo essencial ausente: session_id")
	}
	if rec.Timestamp == "" {
		return rec, fmt.Errorf("campo essencial ausente: timestamp")
	}
	// output_bytes pode ser 0 legitimamente; aqui só checamos parse.
	return rec, nil
}

// recomputeProof aplica exatamente a mesma fórmula de tracking.ComputeHash
// usada em run.go:126. Qualquer divergência aqui quebra todos os logs
// históricos — por isso passamos pela função canônica em vez de duplicar.
func recomputeProof(rec verifyRecord) (string, error) {
	ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return "", fmt.Errorf("timestamp inválido %q: %w", rec.Timestamp, err)
	}
	return tracking.ComputeHash(rec.SessionID, ts, rec.OutputBytes), nil
}

// safePrefix devolve até n chars de s, sem panic se s for menor.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
