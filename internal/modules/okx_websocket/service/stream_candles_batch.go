package service

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"strconv"
	"time"
	"trade_bot/internal/models"

	"github.com/gorilla/websocket"
)

// StreamCandlesBatch — один WebSocket на таймфрейм с пачкой инструментов в args.
// Возвращает поток CandleTick: instId + полная информация по закрытой свече.
func (c *Client) StreamCandlesBatch(ctx context.Context, instIDs []string, timeframe string) <-chan models.CandleTick {
	out := make(chan models.CandleTick, 1024) // буфер помогает не стопорить WS
	go func() {
		defer close(out)
		if len(instIDs) == 0 {
			return
		}

		channel := "candle" + timeframe
		url := "wss://ws.okx.com:8443/ws/v5/business"
		tfDur := timeframeToDuration(timeframe)

		args := make([]map[string]string, 0, len(instIDs))
		for _, id := range instIDs {
			args = append(args, map[string]string{
				"channel": channel,
				"instId":  id,
			})
		}

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			log.Printf("[WS] batch connect %s %d symbols", channel, len(instIDs))
			conn, _, err := c.wsDialer.Dial(url, nil)
			if err != nil {
				log.Printf("[WS] batch dial error %s: %v", channel, err)
				time.Sleep(time.Second)
				continue
			}

			// per-connection cancel
			connCtx, cancel := context.WithCancel(ctx)

			// подписка
			sub := map[string]any{"op": "subscribe", "args": args}
			if err := conn.WriteJSON(sub); err != nil {
				cancel()
				_ = conn.Close()
				time.Sleep(time.Second)
				continue
			}

			// ping loop (останавливается cancel())
			pingDone := make(chan struct{})
			go func() {
				defer close(pingDone)
				t := time.NewTicker(20 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-connCtx.Done():
						return
					case <-t.C:
						// OKX нормально принимает {"op":"ping"}
						_ = conn.WriteMessage(websocket.TextMessage, []byte("ping"))
					}
				}
			}()

			// read loop
			readErr := func() error {
				for {
					_, msg, err := conn.ReadMessage()
					if err != nil {
						return err
					}

					// 1) попробуем распознать event/op (необязательно, но полезно)
					var meta struct {
						Event string `json:"event"`
						Op    string `json:"op"`
						Msg   string `json:"msg"`
						Code  string `json:"code"`
					}
					_ = json.Unmarshal(msg, &meta)
					if meta.Event == "error" {
						log.Printf("[WS] %s event=error code=%s msg=%s", channel, meta.Code, meta.Msg)
						continue
					}
					if meta.Op == "pong" || meta.Event == "subscribe" {
						continue
					}

					// 2) основной парс свечей
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

					for _, row := range frame.Data {
						if len(row) < 6 {
							continue
						}
						// confirm = последний элемент
						if row[len(row)-1] != "1" {
							continue
						}
						if rand.Intn(2000) == 0 {
							log.Printf("[WS] %s %s confirm=1 ts=%s close=%s", frame.Arg.InstID, timeframe, row[0], row[4])
						}

						tsMs, err := strconv.ParseInt(row[0], 10, 64)
						if err != nil {
							continue
						}
						start := time.UnixMilli(tsMs)
						end := start
						if tfDur > 0 {
							end = start.Add(tfDur)
						}

						open, e1 := strconv.ParseFloat(row[1], 64)
						high, e2 := strconv.ParseFloat(row[2], 64)
						low, e3 := strconv.ParseFloat(row[3], 64)
						closep, e4 := strconv.ParseFloat(row[4], 64)
						if e1 != nil || e2 != nil || e3 != nil || e4 != nil || closep <= 0 {
							continue
						}

						vol, _ := strconv.ParseFloat(row[5], 64)

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
						case out <- tick:
						case <-connCtx.Done():
							return nil
						}
					}
				}
			}()

			// закрываем conn, останавливаем ping
			cancel()
			_ = conn.Close()
			<-pingDone

			if readErr != nil && ctx.Err() == nil {
				log.Printf("[WS] batch read error %s: %v", channel, readErr)
				time.Sleep(time.Second)
				continue
			}
			return
		}
	}()
	return out
}
