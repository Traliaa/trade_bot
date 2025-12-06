package exchange

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"
)

type CandleTick struct {
	InstID string
	Close  float64
}

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

// StreamCandlesBatch — один WebSocket на таймфрейм с пачкой инструментов в args.
// Возвращает поток CandleTick: instId + цена закрытия последней закрытой свечи.
func (c *Client) StreamCandlesBatch(ctx context.Context, instIDs []string, timeframe string) <-chan CandleTick {
	ch := make(chan CandleTick)
	go func() {
		defer close(ch)

		if len(instIDs) == 0 {
			return
		}

		channel := "candle" + timeframe // "1m" -> "candle1m"
		url := "wss://ws.okx.com:8443/ws/v5/business"

		// подготавливаем args сразу пачкой
		args := make([]map[string]string, 0, len(instIDs))
		for _, id := range instIDs {
			args = append(args, map[string]string{
				"channel": channel,
				"instId":  id,
			})
		}

		for {
			log.Printf("[WS] batch connect %s %d symbols", channel, len(instIDs))
			conn, _, err := c.wsDialer.Dial(url, nil)
			if err != nil {
				log.Printf("[WS] batch dial error %s: %v", channel, err)
				time.Sleep(time.Second)
				continue
			}

			sub := map[string]any{
				"op":   "subscribe",
				"args": args,
			}
			if err := conn.WriteJSON(sub); err != nil {
				log.Printf("[WS] batch subscribe error %s: %v", channel, err)
				_ = conn.Close()
				continue
			}

			// keepalive ping каждые 20s — иначе OKX рвёт соединение с 4004
			stopPing := make(chan struct{})
			go func() {
				defer close(stopPing)
				t := time.NewTicker(20 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-stopPing:
						return
					case <-t.C:
						_ = conn.WriteJSON(map[string]string{"op": "ping"})
					}
				}
			}()

			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					log.Printf("[WS] batch read error %s: %v", channel, err)
					_ = conn.Close()
					break
				}

				var frame struct {
					Arg struct {
						Channel string `json:"channel"`
						InstID  string `json:"instId"`
					} `json:"arg"`
					Data [][]string `json:"data"`
				}
				if err := json.Unmarshal(msg, &frame); err != nil {
					continue
				}
				if frame.Arg.Channel != channel || len(frame.Data) == 0 {
					continue
				}

				row := frame.Data[0]
				if len(row) < 5 {
					continue
				}
				// формат data: [ts,o,h,l,c,vol,volCcy,volCcyQuote,confirm]
				if len(row) >= 9 && row[8] != "1" {
					continue // ждём закрытую свечу
				}
				closeStr := row[4]
				p, err := strconv.ParseFloat(closeStr, 64)
				if err != nil || p <= 0 {
					continue
				}

				c.SetPrice(frame.Arg.InstID, p)
				select {
				case ch <- CandleTick{InstID: frame.Arg.InstID, Close: p}:
				case <-ctx.Done():
					_ = conn.Close()
					return
				}
			}

			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(time.Second)
			}
		}
	}()
	return ch
}
