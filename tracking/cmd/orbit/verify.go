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
	"path/filepath"
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

	// body_hash: cobre o JSON inteiro (defesa contra edição de campos que
	// o proof legado não alcança — output, decision, diagnosis). Back-compat:
	// log sem body_hash é aceito, sinalizado como "legado" na saída.
	if err := verifyBodyHash(w, logPath); err != nil {
		return err
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

// verifyBodyHash lê o log completo, recomputa CanonicalHash e compara.
// Log sem body_hash é tratado como legado — emite nota mas não falha.
func verifyBodyHash(w io.Writer, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("body_hash: read %q: %w", path, err)
	}
	var r RunResult
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("body_hash: unmarshal %q: %w", path, err)
	}
	if r.BodyHash == "" {
		fmt.Fprintf(w, "   ⚠️  log legado sem body_hash — integridade do corpo não verificada\n")
		return nil
	}
	got, err := CanonicalHash(r)
	if err != nil {
		return fmt.Errorf("body_hash recompute: %w", err)
	}
	if got != r.BodyHash {
		fmt.Fprintf(w, "❌  body_hash mismatch em %s\n", path)
		fmt.Fprintf(w, "    armazenado:  %s...\n", safePrefix(r.BodyHash, 32))
		fmt.Fprintf(w, "    recomputado: %s...\n", safePrefix(got, 32))
		return fmt.Errorf("body_hash mismatch — log adulterado")
	}
	return nil
}

// runVerifyChain percorre TODOS os logs em $ORBIT_HOME/logs/ ordenados por
// timestamp e valida que cada prev_proof bate com o body_hash do anterior.
// Fail-closed no primeiro gap. Back-compat assimétrica: logs legados (sem
// body_hash) só são tolerados ANTES da primeira âncora com body_hash — um
// legacy_gap no meio da sequência é reset silencioso, tratado como break.
func runVerifyChain(w io.Writer) error {
	paths, err := ListExecutionLogs()
	if err != nil {
		return fmt.Errorf("chain: list logs: %w", err)
	}
	if len(paths) == 0 {
		fmt.Fprintln(w, "ℹ️  sem logs para verificar")
		return nil
	}
	var prevBodyHash string
	chainStarted := false
	checked, resets := 0, 0
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("chain: read %q: %w", p, err)
		}
		var r RunResult
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("chain: unmarshal %q: %w", p, err)
		}
		if r.BodyHash == "" {
			// Legado após âncora = tentativa de reset silencioso da chain.
			if chainStarted {
				fmt.Fprintf(w, "❌  legacy_gap em %s — log legado após início da chain\n", filepath.Base(p))
				fmt.Fprintf(w, "    apenas logs anteriores à primeira âncora podem omitir body_hash\n")
				return fmt.Errorf("chain break — legacy_gap detectado (reset silencioso da chain)")
			}
			prevBodyHash, resets = "", resets+1
			continue
		}
		if prevBodyHash != "" && r.PrevProof != prevBodyHash {
			fmt.Fprintf(w, "❌  chain break em %s\n", filepath.Base(p))
			fmt.Fprintf(w, "    prev_proof:   %s...\n", safePrefix(r.PrevProof, 32))
			fmt.Fprintf(w, "    esperado:     %s...\n", safePrefix(prevBodyHash, 32))
			return fmt.Errorf("chain break — log removido, reordenado ou adulterado")
		}
		prevBodyHash = r.BodyHash
		chainStarted = true
		checked++
	}
	fmt.Fprintf(w, "✅  chain íntegra — %d logs verificados, %d âncoras legadas\n",
		checked, resets)
	return nil
}

// safePrefix devolve até n chars de s, sem panic se s for menor.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
