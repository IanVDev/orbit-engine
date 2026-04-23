package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// spinnerEnabled retorna true quando o spinner deve ser exibido.
// Usa stderrIsTTY (session_banner.go) mais os overrides de NO_COLOR e TERM.
func spinnerEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return stderrIsTTY()
}

// Spinner renders a braille animation on stderr while a long operation runs.
// All writes go to stderr so stdout (proof, JSON output) is never touched.
// When inactive (non-TTY, disabled, or CI), every method is a no-op.
type Spinner struct {
	update   chan string
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	active   bool
}

// NewSpinner starts the spinner with msg. Pass disabled=true to suppress it
// unconditionally (e.g. --no-spinner flag or JSON mode).
func NewSpinner(msg string, disabled bool) *Spinner {
	s := &Spinner{
		update: make(chan string, 4),
		stop:   make(chan struct{}),
	}
	if !spinnerEnabled() || disabled {
		return s
	}
	s.active = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		current := msg
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				fmt.Fprint(os.Stderr, "\r\033[K")
				return
			case m := <-s.update:
				current = m
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r%s %s", frames[i%len(frames)], current)
				i++
			}
		}
	}()
	return s
}

// SetMsg updates the status message displayed next to the spinner.
func (s *Spinner) SetMsg(msg string) {
	if !s.active {
		return
	}
	select {
	case s.update <- msg:
	default:
	}
}

// Stop halts the spinner and clears its line. Safe to call multiple times.
func (s *Spinner) Stop() {
	if !s.active {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
}
