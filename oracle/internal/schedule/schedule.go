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

// Loop runs job immediately, then at the first activation of sched after each
// run ends, until ctx ends. Runs never overlap: activations that fall while
// job is running are skipped (node-cron let them overlap).
func Loop(ctx context.Context, sched cron.Schedule, job func(context.Context)) {
	for ctx.Err() == nil {
		job(ctx)
		timer := time.NewTimer(time.Until(sched.Next(time.Now())))
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
