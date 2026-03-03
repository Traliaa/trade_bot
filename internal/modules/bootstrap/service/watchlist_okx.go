package service

import (
	okx_websocket "trade_bot/internal/modules/okx_websocket/service"
)

type OkxWatchlist struct{ mx *okx_websocket.Service }

func NewWatchlist(mx *okx_websocket.Service) *OkxWatchlist {
	return &OkxWatchlist{mx: mx}
}

func (w *OkxWatchlist) TopVolatile(n int) ([]string, error) {
	return w.mx.TopVolatile(n) // подгони сигнатуру под свою
}
