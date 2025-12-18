package service

import (
	okx_websocket "trade_bot/internal/modules/okx_websocket/service"
)

type OkxWatchlist struct{ mx *okx_websocket.Client }

func NewWatchlist(mx *okx_websocket.Client) *OkxWatchlist {
	return &OkxWatchlist{mx: mx}
}

func (w *OkxWatchlist) TopVolatile(n int) []string {
	return w.mx.TopVolatile(n) // подгони сигнатуру под свою
}
