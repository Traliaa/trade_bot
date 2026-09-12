package service

import (
	"context"
	"time"
)

// A failed warmup must not leave an otherwise running process permanently
// unable to trade. Readiness remains false until an entire attempt succeeds.
func retryWarmup(ctx context.Context, delay time.Duration, attempt func(context.Context) error, failed func(error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := attempt(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		failed(err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
