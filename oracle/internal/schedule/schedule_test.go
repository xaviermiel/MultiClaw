package schedule

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
)

func TestLoopRunsImmediatelyThenOnSchedule(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var runs atomic.Int32
	start := time.Now()
	var first time.Duration
	Loop(ctx, Interval(20*time.Millisecond), func(context.Context) {
		if runs.Add(1) == 1 {
			first = time.Since(start)
		}
	})
	if first > 10*time.Millisecond {
		t.Fatalf("first run after %s, want immediately", first)
	}
	// ~5 runs in 100ms; allow for slow machines.
	if n := runs.Load(); n < 3 || n > 6 {
		t.Fatalf("%d runs in 100ms at a 20ms interval", n)
	}
}

func TestLoopNeverOverlapsRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var running, overlaps, runs atomic.Int32
	// Slow runs on a fast schedule must still run one at a time.
	Loop(ctx, Interval(5*time.Millisecond), func(context.Context) {
		if running.Add(1) > 1 {
			overlaps.Add(1)
		}
		runs.Add(1)
		time.Sleep(15 * time.Millisecond)
		running.Add(-1)
	})
	if overlaps.Load() != 0 || runs.Load() < 2 {
		t.Fatalf("%d overlaps in %d runs", overlaps.Load(), runs.Load())
	}
}

func TestLoopStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sched, err := config.CronParser.Parse("0 0 1 1 *") // next January 1st
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var runs atomic.Int32
	go func() {
		Loop(ctx, sched, func(context.Context) { runs.Add(1) })
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Loop did not return after cancel")
	}
	if runs.Load() != 1 {
		t.Fatalf("%d runs, want the immediate one only", runs.Load())
	}
}

func TestEveryWaitsForTheFirstActivation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var runs atomic.Int32
	Every(ctx, Interval(time.Hour), func(context.Context) { runs.Add(1) })
	if runs.Load() != 0 {
		t.Fatalf("%d runs before the first activation", runs.Load())
	}
}

// robfig's Next returns the zero time for a date that never comes: node-cron
// never ran such a schedule, and Every must not run it back to back.
func TestEveryNeverRunsAScheduleThatNeverFires(t *testing.T) {
	sched, err := config.CronParser.Parse("0 0 30 2 *") // February 30
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var runs atomic.Int32
	Loop(ctx, sched, func(context.Context) { runs.Add(1) })
	if n := runs.Load(); n != 1 {
		t.Fatalf("%d runs, want only the immediate one", n)
	}
	if ctx.Err() == nil {
		t.Fatal("Loop returned before its context ended")
	}
}
