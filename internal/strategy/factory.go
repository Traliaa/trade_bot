package strategy

import (
	"log"
	"trade_bot/internal/models"
)

func NewEngine(ts *models.TradingSettings) Engine {
	switch ts.Strategy {
	case "donchian":
		log.Printf("[STRAT] use Donchian: period=%d trendEma=%d",
			ts.DonchianPeriod, ts.TrendEmaPeriod)
		return NewDonchian(DonchianConfig{
			Period:    ts.DonchianPeriod,
			TrendEma:  ts.TrendEmaPeriod,
			MinWarmup: 0,
		})

	case "emarsi", "":
		log.Printf("[STRAT] use EMARSI: S=%d L=%d RSI=%d",
			ts.EMAShort, ts.EMALong, ts.RSIPeriod)
		return NewEMARSI(ts)
	default:
		// здесь ты адаптируешь свою существующую EMARSI под Engine:
		// NewEMARSI должен возвращать тип, реализующий OnCandle/Dump.
		return NewEMARSI(ts)
	}
}
