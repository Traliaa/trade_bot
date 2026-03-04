package service

import (
	"context"
	"encoding/json"
	"math/rand"
	"strconv"
	"time"
	"trade_bot/internal/models"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// StreamCandles — поток закрытых свечей OKX по таймфрейму ("1m","5m","15m").
// legacy: обёртка над батч-версией для одного инструмента.
func (s *Service) StreamCandles(ctx context.Context, instID, timeframe string) <-chan float64 {
	out := make(chan float64)
	go func() {
		defer close(out)
		ch := s.StreamCandlesBatch(ctx, []string{instID}, timeframe)
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
// Возвращает поток CandleTick: instId + полная информация по закрытой свече.
func (s *Service) StreamCandlesBatch(ctx context.Context, instIDs []string, timeframe string) <-chan models.CandleTick {
	out := make(chan models.CandleTick, 1024) // буфер помогает не стопорить WS

	go func() {
		defer close(out)

		//log := s.Logger.With(
		//	zap.String("stream", "candles_batch"),
		//	zap.String("timeframe", timeframe),
		//	zap.Int("symbols", len(instIDs)),
		//)

		if len(instIDs) == 0 {
			s.Logger.Debug("skip: empty instIDs")
			return
		}

		okxBar := toOKXBar(timeframe)
		channel := "candle" + okxBar
		tfDur := timeframeToDuration(timeframe)
		url := "wss://ws.okx.com:8443/ws/v5/business"

		args := make([]map[string]string, 0, len(instIDs))
		for _, id := range instIDs {
			args = append(args, map[string]string{
				"channel": channel,
				"instId":  id,
			})
		}

		// counters for periodic stats
		var (
			reconnects  int
			recvFrames  int
			recvCandles int
			sentCandles int
			dropCandles int
		)

		// periodic stats ticker (lives across reconnects)
		statsT := time.NewTicker(10 * time.Second)
		defer statsT.Stop()

		s.Logger.Info("ws batch stream starting")

		for {
			select {
			case <-ctx.Done():
				s.Logger.Info("context done, stop stream", zap.Error(context.Cause(ctx)))
				return
			default:
			}

			reconnects++
			s.Logger.Info("ws connect", zap.Int("attempt", reconnects))

			conn, _, err := s.wsDialer.Dial(url, nil)
			if err != nil {
				s.Logger.Warn("ws dial error", zap.Error(err))
				time.Sleep(time.Second)
				continue
			}

			// per-connection cancel
			connCtx, cancel := context.WithCancel(ctx)

			// subscribe
			sub := map[string]any{"op": "subscribe", "args": args}
			if err := conn.WriteJSON(sub); err != nil {
				s.Logger.Warn("ws subscribe write error", zap.Error(err))
				cancel()
				_ = conn.Close()
				time.Sleep(time.Second)
				continue
			}
			s.Logger.Info("ws subscribed")

			// ping loop (stops on cancel)
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
						_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
						if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
							// let read loop fail -> reconnect
							s.Logger.Warn("ws ping failed", zap.Error(err))
							return
						}
					}
				}
			}()

			// read loop
			readErr := func() error {
				// sampling: чтобы не спамить каждую свечу
				// 1/2000 как у тебя было, но теперь видно и дропы
				const sampleN = 2000

				for {
					select {
					case <-connCtx.Done():
						return nil
					case <-statsT.C:
						//s.Logger.Info("ws stats",
						//	zap.Int("recv_frames", recvFrames),
						//	zap.Int("recv_candles", recvCandles),
						//	zap.Int("sent_candles", sentCandles),
						//	zap.Int("drop_candles", dropCandles),
						//	zap.Int("reconnects", reconnects),
						//)
					default:
					}

					_, msg, err := conn.ReadMessage()
					if err != nil {
						return err
					}
					recvFrames++

					// meta parse (event/op)
					var meta struct {
						Event string `json:"event"`
						Op    string `json:"op"`
						Msg   string `json:"msg"`
						Code  string `json:"code"`
					}
					_ = json.Unmarshal(msg, &meta)

					if meta.Event == "error" {
						s.Logger.Warn("okx event=error",
							zap.String("code", meta.Code),
							zap.String("msg", meta.Msg),
						)
						continue
					}
					if meta.Op == "pong" || meta.Event == "subscribe" {
						// можно Debug если надо
						continue
					}

					// main candles frame
					var frame struct {
						Arg struct {
							Channel string `json:"channel"`
							InstID  string `json:"instId"`
						} `json:"arg"`
						Data [][]string `json:"data"`
					}
					if err := json.Unmarshal(msg, &frame); err != nil {
						s.Logger.Debug("frame unmarshal failed", zap.Error(err))
						continue
					}
					if frame.Arg.Channel != channel || len(frame.Data) == 0 {
						continue
					}

					for _, row := range frame.Data {
						if len(row) < 6 {
							continue
						}
						// confirm = last element
						if row[len(row)-1] != "1" {
							continue
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

						recvCandles++

						// ✅ что получили из OKX (sampling, иначе слишком много)
						if sampleN > 0 && rand.Intn(sampleN) == 0 {
							s.Logger.Debug("okx candle recv (sample)",
								zap.String("instId", tick.InstID),
								zap.Int64("ts_ms", tsMs),
								zap.Float64("close", tick.Close),
								zap.Float64("vol", tick.Volume),
							)
						}

						// ✅ что отдаем дальше (и фиксируем дропы)
						select {
						case out <- tick:
							sentCandles++
						default:
							// канал переполнен — важно увидеть это, иначе “теряются свечи”
							dropCandles++
							s.Logger.Warn("out channel full, drop candle",
								zap.String("instId", tick.InstID),
								zap.Int("out_cap", cap(out)),
								zap.Int("out_len", len(out)),
								zap.Int64("ts_ms", tsMs),
							)
						}
					}
				}
			}()

			// cleanup
			cancel()
			_ = conn.Close()
			<-pingDone

			if readErr != nil && ctx.Err() == nil {
				s.Logger.Warn("ws read loop error, reconnect", zap.Error(readErr))
				time.Sleep(time.Second)
				continue
			}

			s.Logger.Info("ws stream finished")
			return
		}
	}()

	return out
}
