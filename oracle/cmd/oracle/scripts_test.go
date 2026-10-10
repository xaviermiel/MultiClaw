package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// logLines renders oracle log lines: each entry is "Service HH:MM:SS message".
func logLines(entries ...string) string {
	var b strings.Builder
	for _, e := range entries {
		service, rest, _ := strings.Cut(e, " ")
		clock, msg, _ := strings.Cut(rest, " ")
		b.WriteString("[" + service + " 2026-10-09T" + clock + ".123Z] " + msg + "\n")
	}
	return b.String()
}

// refresh is a spending refresh started at clock, with one subaccount
// decision per entry of decisions ("" for a refresh that fails before it).
func refresh(clock string, allowance, decision string) []string {
	lines := []string{
		"SpendingOracle " + clock + " === Spending Oracle: Periodic Refresh ===",
		"SpendingOracle " + clock + " --- Processing module: 0xAbC0000000000000000000000000000000000001 ---",
	}
	if decision == "" {
		return append(lines, "SpendingOracle "+clock+" Error in periodic refresh: getSubaccountsByRole(1): timeout")
	}
	return append(lines,
		"SpendingOracle "+clock+" Processing subaccount: 0xaaaa0000000000000000000000000000000000a1",
		"SpendingOracle "+clock+" Allowance: safeValue="+allowance,
		"SpendingOracle "+clock+" "+decision,
		"SpendingOracle "+clock+" === Periodic Refresh Complete ===",
	)
}

func safeValueCheck(clock, total string) []string {
	return []string{
		"SafeValue " + clock + " === Safe Value Monitor: Starting check ===",
		"SafeValue " + clock + " --- Processing module: 0xAbC0000000000000000000000000000000000001 ---",
		"SafeValue " + clock + " Total USD Value: $" + total + " (on-chain: $39.99)",
		"SafeValue " + clock + " Updating due to staleness (16608000s > 2700s)",
		"SafeValue " + clock + " === Safe Value Monitor: Complete ===",
	}
}

func join(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// The shadow-run comparison is the go/no-go for removing the TS oracle.
func TestShadowCompare(t *testing.T) {
	for _, tool := range []string{"bash", "awk", "join", "sort"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
	const update = "  Reason: stale (1234s > 2700s)"
	// The startup runs (at different seconds) are never compared.
	tsStart := join(safeValueCheck("16:49:13", "583.46"), refresh("16:49:14", "583 bps=1000", update))
	goStart := join(safeValueCheck("16:49:20", "583.46"), refresh("16:49:21", "583 bps=1000", update))
	cases := []struct {
		name     string
		ts, go_  []string
		code     int
		want     []string
		wantNone string
	}{
		{
			name: "identical",
			ts:   join(tsStart, safeValueCheck("16:50:00", "583.46"), refresh("16:52:00", "583 bps=1000", update)),
			go_:  join(goStart, safeValueCheck("16:50:00", "583.46"), refresh("16:52:00", "583 bps=1000", "  Reason: stale (99s > 2700s)")),
			code: 0,
			want: []string{
				"4 decisions in cycles both oracles ran: 4 identical, 0 different.",
				"Not compared, in cycles one oracle ran alone (startup runs, skipped cycles): TS 4, Go 4.",
			},
		},
		{
			name: "a different decision",
			ts:   join(tsStart, refresh("16:52:00", "583 bps=1000", update)),
			go_:  join(goStart, refresh("16:52:00", "583 bps=1000", "Skipping batch update - no changes needed: ...")),
			code: 1,
			want: []string{"DIFF batch 2026-10-09T16:52:00", "  ts: update: stale (Ns > 2700s)", "  go: no update", "1 different"},
		},
		{
			// The Go refresh failed before the subaccount the TS updated.
			name: "a decision only one oracle logged in a cycle both ran",
			ts:   join(tsStart, refresh("16:52:00", "583 bps=1000", update)),
			go_:  join(goStart, refresh("16:52:00", "", "")),
			code: 1,
			want: []string{"DIFF allowance 2026-10-09T16:52:00", "  go: (not logged)", "2 different"},
		},
		{
			name: "nothing compared",
			ts:   tsStart,
			go_:  goStart,
			code: 2,
			want: []string{"0 decisions in cycles both oracles ran"},
		},
	}
	script, err := filepath.Abs("../../scripts/shadow-compare.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tsLog, goLog := filepath.Join(dir, "ts.log"), filepath.Join(dir, "go.log")
			if err := os.WriteFile(tsLog, []byte(logLines(tc.ts...)), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(goLog, []byte(logLines(tc.go_...)), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("bash", script, tsLog, goLog).CombinedOutput()
			code := 0
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tc.code {
				t.Fatalf("exit %d, want %d:\n%s", code, tc.code, out)
			}
			for _, w := range tc.want {
				if !strings.Contains(string(out), w) {
					t.Fatalf("output lacks %q:\n%s", w, out)
				}
			}
		})
	}
}
