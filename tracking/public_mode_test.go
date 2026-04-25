package tracking

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPublicMode_FailClosedWithRemoteTrackingNoHMAC verifica que o processo
// falha ao iniciar com ORBIT_MODE=public + ORBIT_REMOTE_TRACKING=on sem HMAC.
// Usa subprocess (mesmo padrão de security_init_test.go) porque log.Fatalf
// termina o processo — não pode ser exercitado no mesmo processo.
func TestPublicMode_FailClosedWithRemoteTrackingNoHMAC(t *testing.T) {
	if os.Getenv("ORBIT_TEST_SUBPROCESS") == "1" {
		// init() já chamou log.Fatalf — nunca alcançado em condição normal.
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestPublicMode_FailClosedWithRemoteTrackingNoHMAC")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"ORBIT_TEST_SUBPROCESS=1",
		"ORBIT_MODE=public",
		"ORBIT_REMOTE_TRACKING=on",
		// ORBIT_HMAC_SECRET ausente intencionalmente.
	}

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("esperado exit não-zero quando ORBIT_REMOTE_TRACKING=on sem HMAC em public mode; saída:\n%s", out)
	}
	if !strings.Contains(string(out), "ORBIT_REMOTE_TRACKING=on requires ORBIT_HMAC_SECRET") {
		t.Fatalf("mensagem fail-closed ausente na saída:\n%s", out)
	}
}

// TestPublicMode_PassWithSecureConfig verifica que CheckPublicModeConfig
// retorna nil com configuração segura (remote tracking + HMAC).
func TestPublicMode_PassWithSecureConfig(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	t.Setenv("ORBIT_REMOTE_TRACKING", "on")
	t.Setenv("ORBIT_HMAC_SECRET", "test-secret-at-least-32-bytes-long!!")

	if err := CheckPublicModeConfig(); err != nil {
		t.Errorf("configuração segura não deveria retornar erro: %v", err)
	}
}

// TestPublicMode_RemoteTrackingDisabledByDefault verifica que public mode sem
// ORBIT_REMOTE_TRACKING é configuração segura (tracking desabilitado = padrão).
func TestPublicMode_RemoteTrackingDisabledByDefault(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	// ORBIT_REMOTE_TRACKING não configurado — padrão seguro.

	if err := CheckPublicModeConfig(); err != nil {
		t.Errorf("public mode sem remote tracking não deveria retornar erro: %v", err)
	}
}

// TestPublicMode_BindAllWithoutHMACFails verifica que ORBIT_BIND_ALL=1
// sem HMAC em public mode é configuração proibida.
func TestPublicMode_BindAllWithoutHMACFails(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	t.Setenv("ORBIT_BIND_ALL", "1")
	// ORBIT_HMAC_SECRET ausente.

	if err := CheckPublicModeConfig(); err == nil {
		t.Error("ORBIT_BIND_ALL=1 sem HMAC deveria retornar erro")
	}
}

// TestPublicMode_BindAllWithHMACOK verifica que ORBIT_BIND_ALL=1 com HMAC
// é aceito (com aviso — mas CheckPublicModeConfig não emite warnings).
func TestPublicMode_BindAllWithHMACOK(t *testing.T) {
	t.Setenv("ORBIT_MODE", "public")
	t.Setenv("ORBIT_BIND_ALL", "1")
	t.Setenv("ORBIT_HMAC_SECRET", "test-secret-at-least-32-bytes-long!!")

	if err := CheckPublicModeConfig(); err != nil {
		t.Errorf("ORBIT_BIND_ALL=1 com HMAC não deveria retornar erro: %v", err)
	}
}

// TestPublicMode_NonPublicModeAlwaysOK verifica que modo não-public não
// é afetado pelo CheckPublicModeConfig.
func TestPublicMode_NonPublicModeAlwaysOK(t *testing.T) {
	// Sem ORBIT_MODE configurado.
	if err := CheckPublicModeConfig(); err != nil {
		t.Errorf("modo não-public não deveria retornar erro: %v", err)
	}
}
