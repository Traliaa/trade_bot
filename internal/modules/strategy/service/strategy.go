package service

import "trade_bot/internal/models"

type Engine interface {
	// ok==true когда есть сигнал
	// becameReady==true когда символ перешёл в "готов" (после прогрева)
	OnCandle(t models.CandleTick) (sig models.Signal, ok bool, becameReady bool)

	IsReady(symbol string) bool
	Dump(symbol string) string
	Name() string
}
