package main

import (
	"strings"
	"testing"
)

// TestStartupGuard_Matrix é o anti-regressão central da guarda fail-closed.
// Valida a regra pura (sem I/O): cada violação vira um motivo acumulado;
// cenário limpo continua OK.
func TestStartupGuard_Matrix(t *testing.T) {
	cases := []struct {
		name          string
		selfCommit    string
		pathCommit    string
		found, active string
		wantOK        bool
		wantReason    string // substring esperada em Reasons se wantOK=false
	}{
		{
			name:       "clean env → pass",
			selfCommit: "abc1234",
			pathCommit: "abc1234",
			found:      "/usr/local/bin/orbit",
			active:     "/usr/local/bin/orbit",
			wantOK:     true,
		},
		{
			name:       "commit unknown → fail",
			selfCommit: "unknown",
			pathCommit: "abc1234",
			found:      "/usr/local/bin/orbit",
			active:     "/usr/local/bin/orbit",
			wantOK:     false,
			wantReason: "sem commit stamp",
		},
		{
			name:       "commit mismatch → fail",
			selfCommit: "SELF",
			pathCommit: "OTHER",
			found:      "/usr/local/bin/orbit",
			active:     "/usr/local/bin/orbit",
			wantOK:     false,
			wantReason: "commit mismatch",
		},
		{
			name:       "multiple distinct binaries → fail",
			selfCommit: "abc1234",
			pathCommit: "abc1234",
			found:      "/usr/local/bin/orbit|/Users/x/.orbit/bin/orbit",
			active:     "/usr/local/bin/orbit",
			wantOK:     false,
			wantReason: "binários orbit distintos",
		},
		{
			name:       "same path listed twice → OK (countDistinct dedup)",
			selfCommit: "abc1234",
			pathCommit: "abc1234",
			found:      "/usr/local/bin/orbit|/usr/local/bin/orbit",
			active:     "/usr/local/bin/orbit",
			wantOK:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var found []string
			if tc.found != "" {
				found = strings.Split(tc.found, "|")
			}
			v := evaluateStartupIntegrity(
				"/tmp/self", tc.selfCommit, found, tc.active, tc.pathCommit,
			)
			if v.OK != tc.wantOK {
				t.Fatalf("OK=%v, want %v (reasons=%v)", v.OK, tc.wantOK, v.Reasons)
			}
			if !tc.wantOK {
				if len(v.Reasons) == 0 {
					t.Fatal("esperava reasons não-vazio quando OK=false")
				}
				joined := strings.Join(v.Reasons, " | ")
				if !strings.Contains(joined, tc.wantReason) {
					t.Errorf("reasons não menciona %q: %s", tc.wantReason, joined)
				}
				if len(v.FixHints) != len(v.Reasons) {
					t.Errorf("FixHints (%d) != Reasons (%d)", len(v.FixHints), len(v.Reasons))
				}
			}
		})
	}
}
