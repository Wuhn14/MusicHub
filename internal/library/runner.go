package library

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// RunnerStatus is a snapshot of the background scanner.
type RunnerStatus struct {
	Running    bool
	LastReport Report
	LastRun    time.Time
	LastError  string
}

// Runner runs the scanner at startup, then at a configurable interval or on
// demand. Only one scan runs at a time.
type Runner struct {
	sc       *Scanner
	interval func() time.Duration
	trigger  chan struct{}

	mu     sync.Mutex
	status RunnerStatus
}

// NewRunner builds a runner. interval is read before each wait, so settings
// changes apply without a restart.
func NewRunner(sc *Scanner, interval func() time.Duration) *Runner {
	return &Runner{sc: sc, interval: interval, trigger: make(chan struct{}, 1)}
}

// Status returns the current state.
func (r *Runner) Status() RunnerStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Trigger asks for an immediate scan. It never blocks and does nothing if a
// scan is already running.
func (r *Runner) Trigger() {
	r.mu.Lock()
	running := r.status.Running
	r.mu.Unlock()
	if running {
		return
	}
	select {
	case r.trigger <- struct{}{}:
	default:
	}
}

// Run scans immediately, then waits for the interval or a trigger, until ctx
// is done.
func (r *Runner) Run(ctx context.Context) {
	for {
		r.scanOnce(ctx)
		d := r.interval()
		if d <= 0 {
			d = 15 * time.Minute
		}
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-r.trigger:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (r *Runner) scanOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	r.status.Running = true
	r.mu.Unlock()

	rep, err := r.sc.Scan(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Running = false
	r.status.LastReport = rep
	r.status.LastRun = r.sc.now()
	r.status.LastError = ""
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.status.LastError = err.Error()
		slog.Error("scan de la bibliothèque en échec", "err", err)
		return
	}
	slog.Info("scan de la bibliothèque terminé", "vus", rep.Seen, "ajoutés", rep.Added,
		"modifiés", rep.Updated, "illisibles", rep.Unreadable, "disparus", rep.Missing, "ignorés", rep.Skipped)
}
