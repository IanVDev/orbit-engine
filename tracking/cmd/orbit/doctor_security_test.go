package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestDoctorSecurity_NoHMACInPublicMode verifica que orbit doctor --security
// retorna CRITICAL quando ORBIT_MODE=public sem ORBIT_HMAC_SECRET.
// Anti-regressão: rodar sem HMAC → FAIL.
func TestDoctorSecurity_NoHMACInPublicMode(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	t.Setenv("ORBIT_HMAC_SECRET", "")
	t.Setenv("ORBIT_REMOTE_TRACKING", "")
	t.Setenv("ORBIT_BIND_ALL", "")

	err := runDoctorSecurity(false)
	if err == nil {
		t.Fatal("esperado erro (CRITICAL) em public mode sem HMAC; recebeu nil")
	}
	if !strings.Contains(err.Error(), "CRITICAL") {
		t.Errorf("erro esperado conter CRITICAL, recebeu: %v", err)
	}
}

// TestDoctorSecurity_PassWithSecureConfig verifica que orbit doctor --security
// retorna nil com configuração segura completa.
// Anti-regressão: rodar com config segura → PASS.
func TestDoctorSecurity_PassWithSecureConfig(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	t.Setenv("ORBIT_HMAC_SECRET", "test-secret-at-least-32-bytes-long!!")
	t.Setenv("ORBIT_REMOTE_TRACKING", "")
	t.Setenv("ORBIT_BIND_ALL", "")

	err := runDoctorSecurity(false)
	if err != nil {
		t.Errorf("configuração segura não deveria retornar erro: %v", err)
	}
}

// TestDoctorSecurity_RemoteTrackingWithoutHMACCritical verifica que
// ORBIT_REMOTE_TRACKING=on sem HMAC em public mode gera CRITICAL.
func TestDoctorSecurity_RemoteTrackingWithoutHMACCritical(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	t.Setenv("ORBIT_REMOTE_TRACKING", "on")
	t.Setenv("ORBIT_HMAC_SECRET", "")
	t.Setenv("ORBIT_BIND_ALL", "")

	err := runDoctorSecurity(false)
	if err == nil {
		t.Fatal("esperado CRITICAL com remote tracking sem HMAC; recebeu nil")
	}
}

// TestDoctorSecurity_RemoteTrackingWithHMACOK verifica que
// ORBIT_REMOTE_TRACKING=on com HMAC em public mode é aceito.
func TestDoctorSecurity_RemoteTrackingWithHMACOK(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	t.Setenv("ORBIT_REMOTE_TRACKING", "on")
	t.Setenv("ORBIT_HMAC_SECRET", "test-secret-at-least-32-bytes-long!!")
	t.Setenv("ORBIT_BIND_ALL", "")

	err := runDoctorSecurity(false)
	if err != nil {
		t.Errorf("remote tracking com HMAC não deveria retornar erro: %v", err)
	}
}

// TestDoctorSecurity_BindAllWithoutHMACCritical verifica que ORBIT_BIND_ALL=1
// sem HMAC gera CRITICAL independente do ORBIT_MODE.
func TestDoctorSecurity_BindAllWithoutHMACCritical(t *testing.T) {
	t.Setenv("ORBIT_BIND_ALL", "1")
	t.Setenv("ORBIT_HMAC_SECRET", "")
	t.Setenv("ORBIT_MODE", "")
	t.Setenv("ORBIT_REMOTE_TRACKING", "")

	err := runDoctorSecurity(false)
	if err == nil {
		t.Fatal("esperado CRITICAL com ORBIT_BIND_ALL=1 sem HMAC; recebeu nil")
	}
}

// TestDoctorSecurity_JSONOutputIsValid verifica que --security com --json
// emite DoctorReport com os campos obrigatórios.
func TestDoctorSecurity_JSONOutputIsValid(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	t.Setenv("ORBIT_HMAC_SECRET", "test-secret-at-least-32-bytes-long!!")
	t.Setenv("ORBIT_REMOTE_TRACKING", "")
	t.Setenv("ORBIT_BIND_ALL", "")

	// Constrói o resultado diretamente e verifica o shape do JSON.
	res := &doctorResult{orbitBinPos: -1, localBinPos: -1}
	secCheckOrbitMode(res)
	secCheckHMAC(res)
	secCheckRemoteTracking(res)
	secCheckBinding(res)
	secCheckRedact(res)

	var buf bytes.Buffer
	if err := emitJSONReport(&buf, res); err != nil {
		t.Fatalf("emitJSONReport falhou: %v", err)
	}
	got := buf.String()
	for _, field := range []string{`"version"`, `"checks"`, `"summary"`} {
		if !strings.Contains(got, field) {
			t.Errorf("saída JSON não contém campo %s: %q", field, got)
		}
	}
}

// TestDoctorSecurity_RedactSanityPasses verifica que o auto-teste do
// sanitizador integrado ao doctor --security detecta todos os padrões.
func TestDoctorSecurity_RedactSanityPasses(t *testing.T) {
	res := &doctorResult{orbitBinPos: -1, localBinPos: -1}
	secCheckRedact(res)

	for _, c := range res.checks {
		if c.name == "Sanitização de logs" {
			if c.severity != sevOK {
				t.Errorf("sanitização falhou: %s", c.detail)
			}
			return
		}
	}
	t.Error("check 'Sanitização de logs' não encontrado")
}

// TestDoctorSecurity_NoModeWarning verifica que ORBIT_MODE não configurado
// gera WARNING (não silencioso em modo --security strict).
func TestDoctorSecurity_NoModeWarning(t *testing.T) {
	t.Setenv("ORBIT_MODE", "")
	t.Setenv("ORBIT_HMAC_SECRET", "test-secret-at-least-32-bytes-long!!")
	t.Setenv("ORBIT_REMOTE_TRACKING", "")
	t.Setenv("ORBIT_BIND_ALL", "")

	// --security é sempre strict: WARNING = exit 1
	err := runDoctorSecurity(false)
	if err == nil {
		t.Fatal("esperado WARNING (strict) quando ORBIT_MODE não configurado; recebeu nil")
	}
}
