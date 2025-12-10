package service

import "context"

// StreamCandles — поток закрытых свечей OKX по таймфрейму ("1m","5m","15m").
// legacy: обёртка над батч-версией для одного инструмента.
func (c *Client) StreamCandles(ctx context.Context, instID, timeframe string) <-chan float64 {
	out := make(chan float64)
	go func() {
		defer close(out)
		ch := c.StreamCandlesBatch(ctx, []string{instID}, timeframe)
		for {
			select {
			case <-ctx.Done():
				return
			case tick, ok := <-ch:
				if !ok {
					return
				}
				if tick.InstID != instID {
					continue
				}
				out <- tick.Close
			}
		}
	}()
	return out
}
