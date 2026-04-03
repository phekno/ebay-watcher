package scheduler

import (
	"context"
	"log/slog"
	"time"
)

type RunFunc func(ctx context.Context)

type Scheduler struct {
	interval time.Duration
	fn       RunFunc
}

func New(interval time.Duration, fn RunFunc) *Scheduler {
	return &Scheduler{interval: interval, fn: fn}
}

// Start runs fn immediately, then on every interval tick until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("running initial poll")
	s.fn(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			slog.Info("polling", "at", t.UTC().Format(time.RFC3339))
			s.fn(ctx)
		}
	}
}
