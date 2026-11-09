package main

import (
	"strings"
	"testing"
)

func TestReadableFirstTextLatency(t *testing.T) {
	for _, test := range []struct{ record, want string }{
		{`{"msg":"completion finished","duration":2000000000,"time_to_first_text":1500000000,"count":1500000000}`, "time_to_first_text=1.5s"},
		{`{"msg":"completion finished","duration":2000000000,"time_to_first_text":0}`, "time_to_first_text=0s"},
		{`time=2026-11-09T10:00:00Z level=INFO msg="completion finished" duration=2s time_to_first_text=1.5s`, "time_to_first_text=1.5s"},
	} {
		line := formatJournalLine(test.record)
		if !strings.Contains(line, "duration=2s") || !strings.Contains(line, test.want) {
			t.Fatalf("duration not readable: %s", line)
		}
		if strings.Contains(test.record, `"count"`) && !strings.Contains(line, "count=1500000000") {
			t.Fatalf("unrelated number changed: %s", line)
		}
	}
}
