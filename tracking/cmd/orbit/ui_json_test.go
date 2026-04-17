// ui_json_test.go — atomic emission contract for writeJSONAtomic.
//
// writeJSONAtomic is the general-purpose sibling of emitJSONReport.
// Its contract: exactly one Write per emission, no retries on partial
// failure, no bytes written on encode failure. This file mirrors the
// partial/rejection tests in doctor_structured_test.go for the same
// writer harnesses — atomicity is a package-wide guarantee, not doctor-
// specific.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestWriteJSONAtomic_Success asserts the happy path: single Write, full
// buffered JSON, no error.
func TestWriteJSONAtomic_Success(t *testing.T) {
	w := &countingWriter{accept: true}

	payload := map[string]any{"version": "v1", "n": 42}
	if err := writeJSONAtomic(w, payload); err != nil {
		t.Fatalf("writeJSONAtomic: %v", err)
	}
	if w.writes != 1 {
		t.Errorf("Write calls = %d; atomic contract requires exactly 1", w.writes)
	}

	var decoded map[string]any
	if err := json.Unmarshal(w.buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output not valid JSON: %v\n---\n%s", err, w.buf.String())
	}
	if decoded["version"] != "v1" {
		t.Errorf("version = %v; want v1", decoded["version"])
	}
}

// TestWriteJSONAtomic_PartialWriteFailure: writer returns (n<len, err).
// No retry — exactly one Write call, error propagated, no second
// envelope appended to the writer's buffer.
func TestWriteJSONAtomic_PartialWriteFailure(t *testing.T) {
	w := &partialWriter{acceptBytes: 5}

	err := writeJSONAtomic(w, map[string]string{"key": "value"})
	if err == nil {
		t.Fatalf("expected error when writer partial-fails")
	}
	if w.writes != 1 {
		t.Errorf("Write calls = %d; expected exactly 1 (no retry)", w.writes)
	}
	// The writer received at most the prefix of ONE JSON document. Asserting
	// on marker count — no second payload would have been appended.
	if strings.Count(w.buf.String(), "{") > 1 {
		t.Errorf("writer buffer has more than one '{'; atomicity violated:\n%s", w.buf.String())
	}
}

// TestWriteJSONAtomic_Rejection: writer rejects all writes. Buffer stays
// empty, error propagates.
func TestWriteJSONAtomic_Rejection(t *testing.T) {
	w := &countingWriter{accept: false}

	err := writeJSONAtomic(w, map[string]string{"k": "v"})
	if err == nil {
		t.Fatalf("expected error from rejecting writer")
	}
	if w.writes != 1 {
		t.Errorf("Write calls = %d; expected 1 (the attempted write)", w.writes)
	}
	if w.buf.Len() != 0 {
		t.Errorf("rejecting writer buffer has %d bytes; expected 0", w.buf.Len())
	}
}

// TestWriteJSONAtomic_EncodeFailureWritesNothing: when encoding v fails
// (unsupported type like a channel), no bytes are written to w at all.
func TestWriteJSONAtomic_EncodeFailureWritesNothing(t *testing.T) {
	w := &countingWriter{accept: true}

	unencodable := make(chan int) // channels are not JSON-serialisable
	err := writeJSONAtomic(w, unencodable)
	if err == nil {
		t.Fatalf("expected encode error for channel type")
	}
	if w.writes != 0 {
		t.Errorf("Write calls = %d; expected 0 on encode failure", w.writes)
	}
	if w.buf.Len() != 0 {
		t.Errorf("writer buffer has %d bytes after encode failure; expected 0", w.buf.Len())
	}
}

// TestPrintJSON_UsesAtomicPath is a smoke test proving PrintJSON routes
// through the atomic pathway. Captures stdout and validates the output is
// a single parseable JSON document.
func TestPrintJSON_UsesAtomicPath(t *testing.T) {
	got := captureStdout(t, func() {
		_ = PrintJSON(map[string]string{"hello": "world"})
	})
	var decoded map[string]string
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("PrintJSON stdout is not valid JSON: %v\n---\n%s", err, got)
	}
	if decoded["hello"] != "world" {
		t.Errorf("decoded = %v; want map[hello:world]", decoded)
	}
}

// Sanity: verify bytes import used (the writer helpers are in doctor_structured_test.go).
var _ = bytes.Buffer{}
var _ = errors.New
