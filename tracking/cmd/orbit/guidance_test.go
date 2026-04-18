package main

import "testing"

func TestBuildGuidance(t *testing.T) {
	cases := []struct {
		name     string
		event    EventType
		exitCode int
		output   string
		want     string
	}{
		{
			name:     "TEST_FAIL Go — extrai file:line do stack",
			event:    EventTestRun,
			exitCode: 1,
			output:   "--- FAIL: TestX (0.00s)\n    main_test.go:42: unexpected value\nFAIL",
			want:     "main_test.go:42",
		},
		{
			name:     "TEST_FAIL Python — traceback com path relativo",
			event:    EventTestRun,
			exitCode: 1,
			output:   "Traceback (most recent call last):\n  File \"tests/foo.py\", line 10, in test_x\n    assert 1 == 2\nAssertionError",
			want:     "", // formato Python "File \"x.py\", line N" não casa o regex (não tem ':<n>')
		},
		{
			name:     "TEST_FAIL com file:line embutido — casa",
			event:    EventTestRun,
			exitCode: 1,
			output:   "E   AssertionError at tests/foo.py:10: expected 1, got 2",
			want:     "tests/foo.py:10",
		},
		{
			name:     "TEST_RUN passando — sem guidance (silêncio quando saudável)",
			event:    EventTestRun,
			exitCode: 0,
			output:   "PASS\nok  example.com/x 0.123s",
			want:     "",
		},
		{
			name:     "CODE_CHANGE — sem guidance (não é categoria de ação)",
			event:    EventCodeChange,
			exitCode: 0,
			output:   "src/main.go:1 modified",
			want:     "",
		},
		{
			name:     "TEST_FAIL sem file:line no output — fail-closed (vazio)",
			event:    EventTestRun,
			exitCode: 1,
			output:   "some generic error without any location",
			want:     "",
		},
		{
			name:     "TEST_FAIL com output vazio — vazio (fail-closed)",
			event:    EventTestRun,
			exitCode: 1,
			output:   "",
			want:     "",
		},
		{
			name:     "TEST_FAIL pega PRIMEIRA ocorrência",
			event:    EventTestRun,
			exitCode: 1,
			output:   "first.go:10 failed\nsecond.go:20 also failed",
			want:     "first.go:10",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildGuidance(tc.event, tc.exitCode, tc.output)
			if got != tc.want {
				t.Fatalf("BuildGuidance(%q, %d, _) = %q, want %q",
					tc.event, tc.exitCode, got, tc.want)
			}
		})
	}
}
