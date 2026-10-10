package logx

import (
	"bytes"
	"testing"
	"time"
)

func TestPrintfMatchesTSFormat(t *testing.T) {
	var buf bytes.Buffer
	ts := time.Date(2026, 4, 16, 16, 40, 58, 509_000_000, time.UTC)
	l := NewWithWriter("SpendingOracle", &buf, func() time.Time { return ts })

	l.Printf("Found %d active modules in registry", 2)
	l.Printf("trailing newline is not doubled\n")

	want := "[SpendingOracle 2026-04-16T16:40:58.509Z] Found 2 active modules in registry\n" +
		"[SpendingOracle 2026-04-16T16:40:58.509Z] trailing newline is not doubled\n"
	if buf.String() != want {
		t.Fatalf("got %q\nwant %q", buf.String(), want)
	}
}

func TestFormatTimeIsUTCWithMillis(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	ts := time.Date(2026, 1, 2, 5, 4, 5, 7_000_000, loc)
	if got := FormatTime(ts); got != "2026-01-02T03:04:05.007Z" {
		t.Fatalf("FormatTime = %q", got)
	}
}
