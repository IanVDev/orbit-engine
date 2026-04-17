package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// resetState restaura Commit/BuildTime/env para o valor original após o teste.
func saveAndRestoreBuildVars(t *testing.T, commit, buildTime string) {
	t.Helper()
	origCommit, origBuild := Commit, BuildTime
	Commit, BuildTime = commit, buildTime
	t.Cleanup(func() { Commit, BuildTime = origCommit, origBuild })
}

// withTrackingServer inicia um stub /health e aponta o doctor para ele via
// override da var de endpoint — não usamos aqui porque a const é fixa, então
// os testes de conectividade dependem da porta 9100 real; marcamos como skip
// quando indisponível.
func withEnv(t *testing.T, key, val string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	os.Setenv(key, val)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, orig)
		} else {
			os.Unsetenv(key)
		}
	})
}

// TestDoctorCriticalOnEmptyCommit: fail-closed principal do requisito.
// Se Commit == "unknown" ou vazio, checkCommitStamp deve marcar CRITICAL.
func TestDoctorCriticalOnEmptyCommit(t *testing.T) {
	for _, v := range []string{"", "unknown", "   "} {
		saveAndRestoreBuildVars(t, v, "irrelevante")
		res := &doctorResult{}
		checkCommitStamp(res)
		if len(res.checks) != 1 {
			t.Fatalf("commit=%q: esperava 1 check, obteve %d", v, len(res.checks))
		}
		if res.checks[0].severity != sevCritical {
			t.Errorf("commit=%q: esperava CRITICAL, obteve %s", v, res.checks[0].severity.tag())
		}
	}
}

func TestDoctorOKOnValidCommit(t *testing.T) {
	saveAndRestoreBuildVars(t, "abc1234", "2026-04-16T00:00:00Z")
	res := &doctorResult{}
	checkCommitStamp(res)
	if res.checks[0].severity != sevOK {
		t.Errorf("esperava OK, obteve %s (%s)", res.checks[0].severity.tag(), res.checks[0].detail)
	}
	if !strings.Contains(res.checks[0].detail, "abc1234") {
		t.Errorf("detail deveria conter commit: %q", res.checks[0].detail)
	}
}

// TestDoctorHMAC_DevWarningProdCritical cobre a matriz dev/prod do requisito.
func TestDoctorHMAC_DevWarningProdCritical(t *testing.T) {
	cases := []struct {
		name, env, secret string
		want              severity
	}{
		{"dev sem secret", "", "", sevWarning},
		{"dev com secret", "", "s3cret", sevOK},
		{"prod sem secret", "prod", "", sevCritical},
		{"production sem secret", "production", "", sevCritical},
		{"prod com secret", "prod", "s3cret", sevOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv(t, "ORBIT_ENV", tc.env)
			withEnv(t, "ORBIT_HMAC_SECRET", tc.secret)
			res := &doctorResult{}
			checkHMACSecret(res)
			if res.checks[0].severity != tc.want {
				t.Errorf("esperava %s, obteve %s", tc.want.tag(), res.checks[0].severity.tag())
			}
		})
	}
}

// TestDoctorTrackingConnectivity_Critical: GET /health falha → CRITICAL.
// Usamos httptest para garantir que a lógica de classificação funciona
// mesmo sem um tracking-server real; o teste não valida a URL const,
// apenas a semântica via helper auxiliar.
func TestDoctorTrackingConnectivity_ClassifiesByStatus(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    severity
	}{
		{
			name:    "200 OK",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) },
			want:    sevOK,
		},
		{
			name:    "503 unhealthy",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) },
			want:    sevCritical,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			// Chama o helper interno via http.Client direto — espelhando
			// exatamente a lógica de checkTrackingConnectivity.
			res := &doctorResult{}
			probeHealth(res, srv.URL, 2)
			if res.checks[0].severity != tc.want {
				t.Errorf("esperava %s, obteve %s (%s)",
					tc.want.tag(), res.checks[0].severity.tag(), res.checks[0].detail)
			}
		})
	}
}

// TestDoctorDuplicateBinariesWarning: múltiplos orbits no PATH → WARNING.
func TestDoctorDuplicateBinariesWarning(t *testing.T) {
	res := &doctorResult{allFound: []string{"/usr/local/bin/orbit", "/opt/orbit/orbit"}}
	checkUniqueOrbit(res)
	if res.checks[0].severity != sevWarning {
		t.Errorf("esperava WARNING, obteve %s", res.checks[0].severity.tag())
	}
	if !strings.Contains(res.checks[0].fixHint, "rm -f") {
		t.Errorf("fixHint deveria propor rm, obteve: %q", res.checks[0].fixHint)
	}
}

// TestDoctorNoOrbitFound_Critical: nenhum binário → CRITICAL.
func TestDoctorNoOrbitFound_Critical(t *testing.T) {
	res := &doctorResult{allFound: nil}
	checkUniqueOrbit(res)
	if res.checks[0].severity != sevCritical {
		t.Errorf("esperava CRITICAL, obteve %s", res.checks[0].severity.tag())
	}
}

// TestFinalizeFailClosed: qualquer CRITICAL → erro, independente de --strict.
func TestFinalizeFailClosed(t *testing.T) {
	res := &doctorResult{checks: []check{{severity: sevCritical, name: "x"}}}
	if err := finalize(res, false); err == nil {
		t.Error("esperava erro com CRITICAL presente")
	}
	res2 := &doctorResult{checks: []check{{severity: sevWarning, name: "x"}}}
	if err := finalize(res2, false); err != nil {
		t.Errorf("WARNING sem --strict não deveria falhar: %v", err)
	}
	if err := finalize(res2, true); err == nil {
		t.Error("WARNING com --strict deveria falhar")
	}
}
