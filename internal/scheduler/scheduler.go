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
	trigger  chan struct{}
}

func New(interval time.Duration, fn RunFunc) *Scheduler {
	return &Scheduler{interval: interval, fn: fn, trigger: make(chan struct{}, 1)}
}

// TriggerNow requests an immediate poll. Non-blocking; if a trigger is already
// pending it is a no-op.
func (s *Scheduler) TriggerNow() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Start runs fn immediately, then on every interval tick or manual trigger
// until ctx is cancelled.
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
		case <-s.trigger:
			slog.Info("polling (triggered)")
			s.fn(ctx)
			ticker.Reset(s.interval)
		}
	}
}
