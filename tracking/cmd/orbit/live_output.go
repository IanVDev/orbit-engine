// live_output.go — exibição em tempo real de stdout/stderr durante orbit run.
//
// Dois invariantes:
//  I-PROOF: RawBytes() preserva bytes originais sem redaction — proof e log
//           usam esses bytes; o terminal recebe apenas a versão redatada.
//  I-SAFE:  emitLocked chama redactOutput antes de qualquer Fprintf.
//           Sem TTY ou em --json/--no-spinner → enabled=false → nenhum print.
package main

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	liveMaxLineLen   = 256
	liveDefaultGrace = 5 * time.Second
)

// LiveOutput captura bytes de stdout/stderr e, quando enabled, os exibe em
// tempo real com redaction de secrets. RawBytes() retorna sempre os bytes
// originais para que proof e log não sejam afetados.
type LiveOutput struct {
	mu           sync.Mutex
	rawBuf       bytes.Buffer
	displayW     io.Writer
	enabled      bool
	maxLineLen   int
	silenceGrace time.Duration

	// Métricas — exportadas para incluir no log de execução.
	Lines      int
	Redactions int
	Truncated  int

	lastOutput    time.Time
	startedAt     time.Time
	shutdown      chan struct{}
	wg            sync.WaitGroup
	stdoutPending []byte
	stderrPending []byte
}

// NewLiveOutput cria um LiveOutput. w recebe output redatado (tipicamente
// os.Stderr, para não interferir com --json no stdout). enabled deve ser
// false em --json mode, --no-spinner, ou sem TTY.
func NewLiveOutput(w io.Writer, enabled bool) *LiveOutput {
	return &LiveOutput{
		displayW:     w,
		enabled:      enabled,
		maxLineLen:   liveMaxLineLen,
		silenceGrace: liveDefaultGrace,
		lastOutput:   time.Now(),
		startedAt:    time.Now(),
		shutdown:     make(chan struct{}),
	}
}

// Start imprime o cabeçalho da seção e inicia o heartbeat (noop se disabled).
func (lo *LiveOutput) Start() {
	if !lo.enabled {
		return
	}
	fmt.Fprintln(lo.displayW, col(ansiDim, "  ── live output ─────────────────────────────────"))
	lo.wg.Add(1)
	go lo.heartbeatLoop()
}

// Stop encerra o heartbeat e faz flush das linhas pendentes (noop se disabled).
func (lo *LiveOutput) Stop() {
	if !lo.enabled {
		return
	}
	close(lo.shutdown)
	lo.wg.Wait()
	lo.mu.Lock()
	defer lo.mu.Unlock()
	if len(lo.stdoutPending) > 0 {
		lo.emitLocked(lo.stdoutPending, false)
		lo.stdoutPending = nil
	}
	if len(lo.stderrPending) > 0 {
		lo.emitLocked(lo.stderrPending, true)
		lo.stderrPending = nil
	}
	fmt.Fprintln(lo.displayW, col(ansiDim, "  ─────────────────────────────────────────────────────"))
}

// StdoutWriter retorna um io.Writer que alimenta o live output como stdout.
func (lo *LiveOutput) StdoutWriter() io.Writer { return &liveStreamWriter{lo: lo, isStderr: false} }

// StderrWriter retorna um io.Writer que alimenta o live output como stderr.
func (lo *LiveOutput) StderrWriter() io.Writer { return &liveStreamWriter{lo: lo, isStderr: true} }

// RawBytes retorna uma cópia dos bytes originais capturados (sem redaction).
// Usado pelo caller para calcular outputBytes (proof) e preencher result.Output.
func (lo *LiveOutput) RawBytes() []byte {
	lo.mu.Lock()
	defer lo.mu.Unlock()
	b := make([]byte, lo.rawBuf.Len())
	copy(b, lo.rawBuf.Bytes())
	return b
}

type liveStreamWriter struct {
	lo       *LiveOutput
	isStderr bool
}

func (sw *liveStreamWriter) Write(p []byte) (int, error) {
	sw.lo.ingest(p, sw.isStderr)
	return len(p), nil
}

func (lo *LiveOutput) ingest(p []byte, isStderr bool) {
	lo.mu.Lock()
	defer lo.mu.Unlock()

	// I-PROOF: gravar sempre, antes de qualquer transformação.
	lo.rawBuf.Write(p)

	if !lo.enabled {
		return
	}

	lo.lastOutput = time.Now()

	var pending *[]byte
	if isStderr {
		pending = &lo.stderrPending
	} else {
		pending = &lo.stdoutPending
	}
	*pending = append(*pending, p...)

	for {
		idx := bytes.IndexByte(*pending, '\n')
		if idx < 0 {
			break
		}
		line := make([]byte, idx)
		copy(line, (*pending)[:idx])
		*pending = (*pending)[idx+1:]
		lo.emitLocked(line, isStderr)
	}
}

// emitLocked exibe uma linha redatada no terminal. Chamada com lo.mu travado.
// I-SAFE: aplica redactOutput antes de qualquer escrita em lo.displayW.
func (lo *LiveOutput) emitLocked(line []byte, isStderr bool) {
	text := string(line)

	if len(text) > lo.maxLineLen {
		text = text[:lo.maxLineLen] + "… (truncated)"
		lo.Truncated++
	}

	redacted := redactOutput(text)
	if redacted != text {
		lo.Redactions++
	}

	prefix := "  "
	if isStderr {
		prefix = "  " + col(ansiDim, "[stderr]") + " "
	}

	fmt.Fprintf(lo.displayW, "%s%s\n", prefix, redacted)
	lo.Lines++
}

func (lo *LiveOutput) heartbeatLoop() {
	defer lo.wg.Done()
	// Tick em metade do grace para não perder a janela em testes acelerados.
	tick := lo.silenceGrace / 2
	if tick < 50*time.Millisecond {
		tick = 50 * time.Millisecond
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-lo.shutdown:
			return
		case <-ticker.C:
			lo.mu.Lock()
			silent := time.Since(lo.lastOutput)
			elapsed := time.Since(lo.startedAt).Round(time.Second)
			lines := lo.Lines
			lo.mu.Unlock()
			if silent >= lo.silenceGrace {
				fmt.Fprintf(lo.displayW, "  %s\n",
					col(ansiDim, fmt.Sprintf("orbit: still running, %s elapsed, %d lines captured",
						elapsed, lines)))
			}
		}
	}
}
