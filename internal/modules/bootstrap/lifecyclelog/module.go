package lifecyclelog

import (
	"context"
	"log"
	"time"

	"go.uber.org/fx"
)

func WrapHook(name string, hook fx.Hook) fx.Hook {
	return fx.Hook{
		OnStart: func(ctx context.Context) error {
			start := time.Now()
			log.Printf("[LC] OnStart begin: %s", name)

			if hook.OnStart == nil {
				log.Printf("[LC] OnStart end: %s (no-op) dur=%s", name, time.Since(start))
				return nil
			}

			err := hook.OnStart(ctx)
			log.Printf("[LC] OnStart end: %s dur=%s err=%v", name, time.Since(start), err)
			return err
		},
		OnStop: func(ctx context.Context) error {
			start := time.Now()
			log.Printf("[LC] OnStop begin: %s", name)

			if hook.OnStop == nil {
				log.Printf("[LC] OnStop end: %s (no-op) dur=%s", name, time.Since(start))
				return nil
			}

			err := hook.OnStop(ctx)
			log.Printf("[LC] OnStop end: %s dur=%s err=%v", name, time.Since(start), err)
			return err
		},
	}
}
