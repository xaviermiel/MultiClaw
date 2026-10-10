// Package schedule runs the oracle's periodic jobs.
package schedule

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
)

// Interval is a cron.Schedule firing a fixed time after each run ends.
type Interval time.Duration

// Next implements cron.Schedule.
func (i Interval) Next(t time.Time) time.Time { return t.Add(time.Duration(i)) }

// Loop runs job immediately, then like Every.
func Loop(ctx context.Context, sched cron.Schedule, job func(context.Context)) {
	job(ctx)
	Every(ctx, sched, job)
}

// Every runs job at the next activation of sched, then at the first
// activation after each run ends, until ctx ends. Runs never overlap:
// activations that fall while job is running are skipped (node-cron let
// them overlap). A schedule that never fires again (a date that doesn't
// exist, such as February 30, for which robfig's Next returns the zero
// time) waits for ctx, as node-cron never ran it.
func Every(ctx context.Context, sched cron.Schedule, job func(context.Context)) {
	for {
		next := sched.Next(time.Now())
		if next.IsZero() {
			<-ctx.Done()
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		job(ctx)
	}
}
