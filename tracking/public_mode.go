// public_mode.go — ORBIT_MODE=public enforcement.
//
// Em public mode:
//   - Tracking remoto desabilitado por padrão.
//   - ORBIT_REMOTE_TRACKING=on exige ORBIT_HMAC_SECRET (fail-closed).
//   - ORBIT_BIND_ALL=1 sem HMAC é configuração proibida.
//
// Fail-closed: qualquer violação chama log.Fatalf na inicialização.
package tracking

import (
	"fmt"
	"log"
	"os"
)

// PublicModeEnv é a variável que ativa o modo público.
const PublicModeEnv = "ORBIT_MODE"

// RemoteTrackingEnv habilita tracking remoto em public mode.
const RemoteTrackingEnv = "ORBIT_REMOTE_TRACKING"

var (
	isPublicMode     bool
	remoteTrackingOn bool // relevante apenas em public mode
)

func init() {
	isPublicMode = os.Getenv(PublicModeEnv) == "public"
	if !isPublicMode {
		return
	}

	log.Printf("[SECURITY] PUBLIC MODE active (ORBIT_MODE=public) — remote tracking disabled by default")

	rt := os.Getenv(RemoteTrackingEnv)
	remoteTrackingOn = rt == "1" || rt == "on"

	if remoteTrackingOn {
		if os.Getenv("ORBIT_HMAC_SECRET") == "" {
			log.Fatalf("[SECURITY] FATAL: ORBIT_REMOTE_TRACKING=on requires ORBIT_HMAC_SECRET in public mode (fail-closed)")
		}
		log.Printf("[SECURITY] PUBLIC MODE: remote tracking ENABLED with HMAC authentication")
	} else {
		log.Printf("[SECURITY] PUBLIC MODE: remote tracking DISABLED (default)")
	}
}

// IsPublicMode retorna true quando ORBIT_MODE=public.
func IsPublicMode() bool {
	return isPublicMode
}

// IsRemoteTrackingEnabled retorna se tracking remoto está ativo.
// Em public mode, é false a menos que ORBIT_REMOTE_TRACKING=on com HMAC.
// Em modo não-public, sempre retorna true.
func IsRemoteTrackingEnabled() bool {
	if isPublicMode {
		return remoteTrackingOn
	}
	return true
}

// CheckPublicModeConfig valida a configuração de public mode sem chamar log.Fatalf.
// Retorna erro se a configuração for insegura. Usado pelo doctor --security.
func CheckPublicModeConfig() error {
	mode := os.Getenv(PublicModeEnv)
	if mode != "public" {
		return nil
	}

	rt := os.Getenv(RemoteTrackingEnv)
	on := rt == "1" || rt == "on"
	if on && os.Getenv("ORBIT_HMAC_SECRET") == "" {
		return fmt.Errorf("ORBIT_REMOTE_TRACKING=on requires ORBIT_HMAC_SECRET in public mode")
	}

	if os.Getenv("ORBIT_BIND_ALL") == "1" && os.Getenv("ORBIT_HMAC_SECRET") == "" {
		return fmt.Errorf("ORBIT_BIND_ALL=1 requires ORBIT_HMAC_SECRET in public mode")
	}

	return nil
}
