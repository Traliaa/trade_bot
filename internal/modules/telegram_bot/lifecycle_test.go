package telegram

import (
	"context"
	"errors"
	"testing"
	"time"
)

type receiverStub struct {
	start func(context.Context) error
	stop  func()
}

func (r receiverStub) Start(ctx context.Context) error { return r.start(ctx) }
func (r receiverStub) Stop()                           { r.stop() }

func TestReceiverLifetimeIsNotStartupContext(t *testing.T) {
	started := make(chan context.Context, 1)
	finished := make(chan struct{})
	hook := receiverHook(receiverStub{start: func(ctx context.Context) error {
		started <- ctx
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	}, stop: func() {}})
	startup, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := hook.OnStart(startup); err != nil {
		t.Fatal(err)
	}
	var runCtx context.Context
	select {
	case runCtx = <-started:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	cancel()
	if runCtx.Err() != nil {
		t.Fatal("startup deadline stopped the receiver")
	}
	shutdown, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := hook.OnStop(shutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("shutdown returned before receiver stopped")
	}
}

func TestReceiverShutdownHonorsDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	hook := receiverHook(receiverStub{start: func(ctx context.Context) error { <-release; return ctx.Err() }, stop: func() {}})
	if err := hook.OnStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := hook.OnStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := hook.OnStop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ignored shutdown deadline: %v", err)
	}
}
