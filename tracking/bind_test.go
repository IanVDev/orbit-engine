package tracking

import "testing"

func TestResolveListenAddr(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		bindAll   string
		want      string
	}{
		{"default loopback for bare port", ":9100", "", "127.0.0.1:9100"},
		{"bind-all opt-in", ":9100", "1", ":9100"},
		{"bind-all=0 stays loopback", ":9100", "0", "127.0.0.1:9100"},
		{"bind-all=other stays loopback", ":9100", "true", "127.0.0.1:9100"},
		{"explicit loopback unchanged", "127.0.0.1:9100", "1", "127.0.0.1:9100"},
		{"explicit host unchanged", "10.0.0.5:9100", "", "10.0.0.5:9100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(BindAllEnv, tc.bindAll)
			got := ResolveListenAddr(tc.requested)
			if got != tc.want {
				t.Fatalf("ResolveListenAddr(%q) with %s=%q = %q, want %q",
					tc.requested, BindAllEnv, tc.bindAll, got, tc.want)
			}
		})
	}
}
