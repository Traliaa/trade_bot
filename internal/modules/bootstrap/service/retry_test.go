package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryWarmupRecoversWithoutRestart(t *testing.T) {
	attempts, failures := 0, 0
	err := retryWarmup(context.Background(), time.Millisecond, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("insufficient closed history")
		}
		return nil
	}, func(error) { failures++ })
	if err != nil || attempts != 3 || failures != 2 {
		t.Fatalf("attempts=%d failures=%d err=%v", attempts, failures, err)
	}
}

func TestRetryWarmupCancellationInterruptsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	err := retryWarmup(ctx, time.Hour, func(context.Context) error {
		attempts++
		return errors.New("unavailable")
	}, func(error) { cancel() })
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestRetryWarmupAlreadyCanceledDoesNotStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := retryWarmup(ctx, time.Hour, func(context.Context) error {
		t.Fatal("attempted warmup after shutdown")
		return nil
	}, func(error) { t.Fatal("reported shutdown as failure") })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWarmupEmptyUniverseIsNotSuccess(t *testing.T) {
	s := &Service{}
	if err := s.Warmup(context.Background(), nil); err == nil || s.done.Load() || s.started.Load() {
		t.Fatalf("empty warmup must fail and allow retry: %v", err)
	}
}
