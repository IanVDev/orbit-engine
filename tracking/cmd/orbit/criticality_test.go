package main

import "testing"

func TestComputeCriticality(t *testing.T) {
	cases := []struct {
		name     string
		event    EventType
		exitCode int
		want     Criticality
	}{
		{"CODE_CHANGE sempre low", EventCodeChange, 0, CriticalityLow},
		{"CODE_CHANGE exit != 0 ainda low", EventCodeChange, 1, CriticalityLow},
		{"TEST_RUN passando — sem criticality", EventTestRun, 0, CriticalityNone},
		{"TEST_RUN falhando — medium (TEST_FAIL)", EventTestRun, 1, CriticalityMedium},
		{"TEST_RUN falha de segfault — medium", EventTestRun, 139, CriticalityMedium},
		{"CODE_MERGE sempre high", EventCodeMerge, 0, CriticalityHigh},
		{"CODE_MERGE com conflito — high", EventCodeMerge, 1, CriticalityHigh},
		{"UNKNOWN — nenhum nível", EventUnknown, 0, CriticalityNone},
		{"evento vazio — nenhum nível (fail-closed)", EventType(""), 0, CriticalityNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeCriticality(tc.event, tc.exitCode)
			if got != tc.want {
				t.Fatalf("ComputeCriticality(%q, %d) = %q, want %q",
					tc.event, tc.exitCode, got, tc.want)
			}
		})
	}
}
