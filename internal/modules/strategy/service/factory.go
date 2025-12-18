package service

import (
	"trade_bot/internal/modules/config"
)

func NewEngine(cfg *config.Config) Engine {
	return NewDonchianV2HTF(cfg)
}
