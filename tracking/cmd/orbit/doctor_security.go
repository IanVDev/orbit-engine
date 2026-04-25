// doctor_security.go — orbit doctor --security.
//
// Checklist de segurança para exposição pública do orbit-engine.
// Sempre fail-closed: WARNINGs também retornam exit 1.
// Não possui dependências externas.
package main

import (
	"fmt"
	"os"
	"strings"
)

// runDoctorSecurity executa verificações exclusivas de segurança.
// Sempre strict: WARNING = exit 1 (exposição pública exige configuração limpa).
func runDoctorSecurity(jsonOut bool) error {
	res := &doctorResult{orbitBinPos: -1, localBinPos: -1}

	if !jsonOut {
		fmt.Println()
		fmt.Println("🔐  orbit doctor --security — checklist de segurança")
		fmt.Println("─────────────────────────────────────────────────")
	}

	secCheckOrbitMode(res)
	secCheckHMAC(res)
	secCheckRemoteTracking(res)
	secCheckBinding(res)
	secCheckRedact(res)

	if jsonOut {
		return emitJSONReport(os.Stdout, res)
	}
	printStructuredReport(res)
	return finalize(res, true) // sempre strict
}

// secCheckOrbitMode: ORBIT_MODE=public é recomendado para exposição externa.
func secCheckOrbitMode(res *doctorResult) {
	mode := os.Getenv("ORBIT_MODE")
	switch mode {
	case "public":
		res.add("ORBIT_MODE", sevOK, "public", "")
	case "":
		res.add("ORBIT_MODE", sevWarning,
			"não configurado — recomendado: ORBIT_MODE=public para exposição externa",
			`export ORBIT_MODE=public`)
	default:
		res.add("ORBIT_MODE", sevWarning,
			fmt.Sprintf("valor desconhecido %q — recomendado: public", mode),
			`export ORBIT_MODE=public`)
	}
}

// secCheckHMAC: CRITICAL em modo public, CRITICAL se ORBIT_BIND_ALL=1, WARNING caso contrário.
func secCheckHMAC(res *doctorResult) {
	hmac := os.Getenv("ORBIT_HMAC_SECRET")
	if hmac != "" {
		res.add("ORBIT_HMAC_SECRET", sevOK, fmt.Sprintf("configurado (len=%d)", len(hmac)), "")
		return
	}

	mode := os.Getenv("ORBIT_MODE")
	bindAll := os.Getenv("ORBIT_BIND_ALL") == "1"

	if mode == "public" || bindAll {
		res.add("ORBIT_HMAC_SECRET", sevCritical,
			"ausente — servidor público sem autenticação (fail-closed violado)",
			`export ORBIT_HMAC_SECRET="$(openssl rand -hex 32)"`)
		return
	}

	res.add("ORBIT_HMAC_SECRET", sevWarning,
		"ausente — tracking não autenticado (aceitável apenas em dev local)",
		`export ORBIT_HMAC_SECRET="$(openssl rand -hex 32)"`)
}

// secCheckRemoteTracking: verifica configuração de tracking remoto em public mode.
func secCheckRemoteTracking(res *doctorResult) {
	mode := os.Getenv("ORBIT_MODE")
	rt := os.Getenv("ORBIT_REMOTE_TRACKING")
	rtOn := rt == "1" || rt == "on"

	if mode != "public" {
		if rtOn {
			res.add("ORBIT_REMOTE_TRACKING", sevOK, "habilitado (modo não-public)", "")
		} else {
			res.add("ORBIT_REMOTE_TRACKING", sevOK, "padrão (habilitado em modo não-public)", "")
		}
		return
	}

	if !rtOn {
		res.add("ORBIT_REMOTE_TRACKING", sevOK, "desabilitado — padrão seguro em ORBIT_MODE=public", "")
		return
	}

	if os.Getenv("ORBIT_HMAC_SECRET") == "" {
		res.add("ORBIT_REMOTE_TRACKING", sevCritical,
			"ORBIT_REMOTE_TRACKING=on sem ORBIT_HMAC_SECRET em public mode (fail-closed violado)",
			`export ORBIT_HMAC_SECRET="$(openssl rand -hex 32)"`)
		return
	}

	res.add("ORBIT_REMOTE_TRACKING", sevOK, "habilitado com autenticação HMAC", "")
}

// secCheckBinding: ORBIT_BIND_ALL=1 expõe o servidor em todas as interfaces.
func secCheckBinding(res *doctorResult) {
	bindAll := os.Getenv("ORBIT_BIND_ALL") == "1"
	hmac := os.Getenv("ORBIT_HMAC_SECRET")

	if !bindAll {
		res.add("Network binding", sevOK, "loopback apenas (127.0.0.1) — padrão seguro", "")
		return
	}

	if hmac == "" {
		res.add("Network binding", sevCritical,
			"ORBIT_BIND_ALL=1 sem HMAC — servidor público sem autenticação",
			`export ORBIT_HMAC_SECRET="$(openssl rand -hex 32)"`)
		return
	}

	res.add("Network binding", sevWarning,
		"ORBIT_BIND_ALL=1 com HMAC — prefira proxy reverso + loopback",
		"configure nginx/caddy como proxy reverso e remova ORBIT_BIND_ALL=1")
}

// secCheckRedact: auto-teste rápido do sanitizador de logs.
func secCheckRedact(res *doctorResult) {
	cases := []struct {
		name  string
		input string
		leak  string
	}{
		{"Bearer token", "Authorization: Bearer eyJhbGciOiJSUzI1NiJ9.payload.sig", "eyJhbGciOiJSUzI1NiJ9"},
		{"x-authorization header", "x-authorization: mysecrettoken123456", "mysecrettoken123456"},
		{"password=", "db_pass: password=SuperSecret42", "SuperSecret42"},
	}

	failures := []string{}
	for _, c := range cases {
		out := redactOutput(c.input)
		if strings.Contains(out, c.leak) {
			failures = append(failures, c.name)
		}
	}

	if len(failures) > 0 {
		res.add("Sanitização de logs", sevCritical,
			fmt.Sprintf("padrões NÃO redigidos: %s — vazamento de secrets em logs", strings.Join(failures, ", ")),
			"bug: verifique redact.go")
		return
	}

	res.add("Sanitização de logs", sevOK,
		fmt.Sprintf("%d padrões verificados (Bearer, x-authorization, password)", len(cases)), "")
}
