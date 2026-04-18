// language.go — inferência determinística de "language" a partir do comando
// executado, para satisfazer o contrato do parser Python (campo essencial
// quando version está presente).
//
// Sem heurística mágica: só mapeamentos exatos. Desconhecido → "other".
package main

// DetectLanguage mapeia o binário executado para uma string canônica.
func DetectLanguage(cmdName string, args []string) string {
	switch cmdName {
	case "go":
		return "go"
	case "python", "python3", "pytest":
		return "python"
	case "node", "npm", "npx", "yarn", "pnpm":
		return "javascript"
	case "cargo", "rustc":
		return "rust"
	case "ruby", "bundle", "rake":
		return "ruby"
	case "java", "mvn", "gradle":
		return "java"
	case "git":
		return "git"
	case "docker":
		return "docker"
	}
	_ = args
	return "other"
}
