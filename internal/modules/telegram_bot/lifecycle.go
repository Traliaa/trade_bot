package telegram

import (
	"context"
	"trade_bot/pkg/logger"

	"go.uber.org/fx"
)

type receiver interface {
	Start(context.Context) error
	Stop()
}

func receiverHook(t receiver) fx.Hook {
	var cancel context.CancelFunc
	var done chan struct{}
	return fx.Hook{
		OnStart: func(context.Context) error {
			// Fx's startup deadline must not become the receiver's lifetime.
			var runCtx context.Context
			runCtx, cancel = context.WithCancel(context.Background())
			done = make(chan struct{})
			go func() {
				defer close(done)
				if err := t.Start(runCtx); err != nil && runCtx.Err() == nil {
					logger.Error("telegram run failed: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel == nil {
				return nil
			}
			cancel()
			t.Stop()
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}
