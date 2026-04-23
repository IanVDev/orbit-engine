package main

import (
	"testing"
)

func TestSpinnerDisabledNoPanic(t *testing.T) {
	s := NewSpinner("test", true)
	s.SetMsg("nova mensagem")
	s.Stop()
	s.Stop() // idempotente
}

func TestSpinnerInactiveTTYNoPanic(t *testing.T) {
	// Em ambiente de teste stderr não é TTY — spinner nasce inativo.
	s := NewSpinner("running...", false)
	s.SetMsg("done")
	s.Stop()
	s.Stop()
}

func TestSpinnerEnabledNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if spinnerEnabled() {
		t.Fatal("spinnerEnabled deve retornar false quando NO_COLOR=1")
	}
}

func TestSpinnerEnabledDumbTerm(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if spinnerEnabled() {
		t.Fatal("spinnerEnabled deve retornar false quando TERM=dumb")
	}
}
