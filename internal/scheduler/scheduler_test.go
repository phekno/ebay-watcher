package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_RunsImmediately(t *testing.T) {
	var count atomic.Int32

	s := New(time.Hour, func(ctx context.Context) {
		count.Add(1)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	s.Start(ctx)

	if c := count.Load(); c < 1 {
		t.Errorf("expected fn to run at least once, ran %d times", c)
	}
}

func TestScheduler_RunsOnTick(t *testing.T) {
	var count atomic.Int32

	s := New(50*time.Millisecond, func(ctx context.Context) {
		count.Add(1)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()

	s.Start(ctx)

	// 1 immediate + 2-3 ticks in 180ms with 50ms interval
	if c := count.Load(); c < 3 {
		t.Errorf("expected fn to run at least 3 times, ran %d times", c)
	}
}

func TestScheduler_StopsOnCancel(t *testing.T) {
	var count atomic.Int32

	s := New(10*time.Millisecond, func(ctx context.Context) {
		count.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()

	// Let it run a few ticks
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// good, Start returned
	case <-time.After(time.Second):
		t.Fatal("Start did not return after context cancellation")
	}

	countAtStop := count.Load()
	time.Sleep(50 * time.Millisecond)

	if count.Load() != countAtStop {
		t.Error("fn continued running after context cancellation")
	}
}
