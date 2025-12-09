package strategy

import "trade_bot/internal/models"

func NewEngine(ts *models.TradingSettings) Engine {
	switch ts.Strategy {
	case "donchian":
		return NewDonchian(DonchianConfig{
			Period:    ts.DonchianPeriod,
			TrendEma:  ts.TrendEmaPeriod,
			MinWarmup: 0,
		})

	case "emarsi", "":
		fallthrough
	default:
		// здесь ты адаптируешь свою существующую EMARSI под Engine:
		// NewEMARSI должен возвращать тип, реализующий OnCandle/Dump.
		return NewEMARSI(EMARSIConfig{
			EMAShort:      ts.EMAShort,
			EMALong:       ts.EMALong,
			RSIPeriod:     ts.RSIPeriod,
			RSIOverbought: ts.RSIOverbought,
			RSIOSold:      ts.RSIOSold,
		})
	}
}
