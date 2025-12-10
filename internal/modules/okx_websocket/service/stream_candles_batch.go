package service

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"
	"trade_bot/internal/models"
)

// StreamCandlesBatch — один WebSocket на таймфрейм с пачкой инструментов в args.
// Возвращает поток CandleTick: instId + полная информация по закрытой свече.
func (c *Client) StreamCandlesBatch(ctx context.Context, instIDs []string, timeframe string) <-chan models.CandleTick {
	ch := make(chan models.CandleTick)

	go func() {
		defer close(ch)

		if len(instIDs) == 0 {
			return
		}

		channel := "candle" + timeframe // "1m" -> "candle1m"
		url := "wss://ws.okx.com:8443/ws/v5/business"
		tfDur := timeframeToDuration(timeframe)

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

			// основной read-loop
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

				// у OKX может приходить несколько свечей в одном кадре — пройдёмся по всем
				for _, row := range frame.Data {
					// ожидаемый формат data:
					// [ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm]
					if len(row) < 5 {
						continue
					}

					// confirm всегда в последнем элементе, не хардкодим индекс 8
					if row[len(row)-1] != "1" {
						continue // ждём закрытую свечу
					}

					// 0: ts (ms)
					tsMs, err := strconv.ParseInt(row[0], 10, 64)
					if err != nil {
						continue
					}
					start := time.UnixMilli(tsMs)
					end := start
					if tfDur > 0 {
						end = start.Add(tfDur)
					}

					// 1..4: OHLC
					open, err1 := strconv.ParseFloat(row[1], 64)
					high, err2 := strconv.ParseFloat(row[2], 64)
					low, err3 := strconv.ParseFloat(row[3], 64)
					closep, err4 := strconv.ParseFloat(row[4], 64)
					if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
						continue
					}
					if closep <= 0 {
						continue
					}

					// 5: объём в контрактах
					var vol float64
					if len(row) >= 6 {
						vol, _ = strconv.ParseFloat(row[5], 64)
					}

					// 7: объём в quote (если есть)
					var volQuote float64
					if len(row) >= 8 {
						volQuote, _ = strconv.ParseFloat(row[7], 64)
					}

					tick := models.CandleTick{
						InstID:       frame.Arg.InstID,
						Open:         open,
						High:         high,
						Low:          low,
						Close:        closep,
						Volume:       vol,
						QuoteVolume:  volQuote,
						Start:        start,
						End:          end,
						TimeframeRaw: timeframe,
					}

					select {
					case ch <- tick:
					case <-ctx.Done():
						_ = conn.Close()
						return
					}
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
