package strategy

import "trade_bot/internal/models"

// Engine — то, что Runner будет дергать.
type Engine interface {
	OnCandle(symbol string, c models.CandleTick) models.Signal
	Dump(symbol string) string
}
