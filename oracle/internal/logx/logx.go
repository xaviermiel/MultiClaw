// Package logx writes log lines in the format of the former TypeScript
// oracle:
//
//	[SpendingOracle 2026-04-16T16:40:58.509Z] message
package logx

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Logf is the logging function shape used by packages that only need to emit
// messages (e.g. the pure spending core).
type Logf func(format string, args ...any)

// Discard is a Logf that drops everything.
func Discard(string, ...any) {}

// Logger prefixes every line with "[<name> <ISO-8601 UTC with millis>]".
type Logger struct {
	name string
	out  io.Writer
	now  func() time.Time
	mu   *sync.Mutex
}

// stdoutMu serializes writes from all loggers sharing os.Stdout so lines from
// concurrent goroutines never interleave.
var stdoutMu sync.Mutex

// New returns a Logger writing to stdout.
func New(name string) *Logger {
	return &Logger{name: name, out: os.Stdout, now: time.Now, mu: &stdoutMu}
}

// NewWithWriter returns a Logger writing to w with the given clock (tests).
func NewWithWriter(name string, w io.Writer, now func() time.Time) *Logger {
	return &Logger{name: name, out: w, now: now, mu: &sync.Mutex{}}
}

// Printf formats and writes one log line.
func (l *Logger) Printf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := "[" + l.name + " " + FormatTime(l.now()) + "] " + strings.TrimSuffix(msg, "\n") + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.out, line)
}

// Logf returns l.Printf as a Logf.
func (l *Logger) Logf() Logf { return l.Printf }

// FormatTime matches JavaScript's Date.prototype.toISOString().
func FormatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
