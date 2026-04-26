package tracking

import "testing"

func TestRedactSecrets_BearerShortToken(t *testing.T) {
	// Token curto (< 10 chars) deve ser redatado.
	// Antes do fix, {10,} deixava tokens como "abc123" visíveis.
	in := "Authorization: Bearer abc123"
	got := RedactSecrets(in)
	if got == in {
		t.Errorf("token curto não foi redatado: %q", got)
	}
	for _, forbidden := range []string{"abc123"} {
		if contains(got, forbidden) {
			t.Errorf("output contém token não redatado %q: %q", forbidden, got)
		}
	}
}

func TestRedactSecrets_BearerLongToken(t *testing.T) {
	in := "Authorization: Bearer eyJhbGciOiJSUzI1NiJ9.payload.sigXXXXXXXXXX"
	got := RedactSecrets(in)
	if contains(got, "eyJhbGciOiJSUzI1NiJ9") {
		t.Errorf("token longo não foi redatado: %q", got)
	}
}

func TestRedactSecrets_Idempotent(t *testing.T) {
	in := "Bearer tokenvalue123"
	once := RedactSecrets(in)
	twice := RedactSecrets(once)
	if once != twice {
		t.Errorf("RedactSecrets não é idempotente:\n  once:  %q\n  twice: %q", once, twice)
	}
}

func TestRedactSecrets_XAuthorization(t *testing.T) {
	in := "x-authorization: mysecrettoken"
	got := RedactSecrets(in)
	if contains(got, "mysecrettoken") {
		t.Errorf("x-authorization não foi redatado: %q", got)
	}
}

func TestRedactSecrets_Password(t *testing.T) {
	for _, in := range []string{
		"password=supersecret",
		"password: supersecret",
		"token=mytoken123",
	} {
		got := RedactSecrets(in)
		if got == in {
			t.Errorf("campo sensível não foi redatado: %q", in)
		}
	}
}

func TestRedactSecrets_NoFalsePositive(t *testing.T) {
	// Strings normais não devem ser alteradas.
	for _, in := range []string{
		"hello world",
		"go test ./...",
		"exit code 0",
	} {
		got := RedactSecrets(in)
		if got != in {
			t.Errorf("falso positivo em %q → %q", in, got)
		}
	}
}

func TestRedactSecrets_SkLiveShortNotRedacted(t *testing.T) {
	// sk-live com menos de 10 chars no sufixo NÃO deve ser redatado
	// (heurística anti-false-positive para paths/slugs curtos).
	in := "sk-live-abc"
	got := RedactSecrets(in)
	if got != in {
		t.Errorf("sk-live curto foi redatado (falso positivo): %q → %q", in, got)
	}
}

func TestRedactSecrets_SkLiveLongRedacted(t *testing.T) {
	in := "sk-live-REALTOKEN1234567890"
	got := RedactSecrets(in)
	if contains(got, "REALTOKEN1234567890") {
		t.Errorf("sk-live longo não foi redatado: %q", got)
	}
}

// contains é um helper local para não importar strings.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
