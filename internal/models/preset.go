package models

type Preset struct {
	Name        string
	Description string
	Apply       func(ts *TradingSettings, tr *TrailingConfig)
}
type TrailingPreset struct {
	Name        string
	Description string
	Apply       func(tr *TrailingConfig)
}

var Presets = map[string]Preset{
	"safe": {
		Name:        "🟢 Консервативный",
		Description: "Минимальный риск, подходит новичкам",
		Apply: func(ts *TradingSettings, tr *TrailingConfig) {
			ts.PositionPct = 0.5
			ts.RiskPct = 0.5
			ts.StopPct = 1.2
			ts.TakeProfitRR = 2.0
			ts.Leverage = 5

			tr.PartialEnabled = true
			tr.PartialTriggerR = 1.0
			tr.PartialCloseFrac = 0.5
		},
	},
	"mid": {
		Name:        "🟡 Средний",
		Description: "Баланс риска и доходности",
		Apply: func(ts *TradingSettings, tr *TrailingConfig) {
			ts.PositionPct = 1.0
			ts.RiskPct = 1.0
			ts.StopPct = 1.5
			ts.TakeProfitRR = 2.5
			ts.Leverage = 10
		},
	},
	"aggr": {
		Name:        "🔴 Агрессивный",
		Description: "Высокий риск, только для опытных",
		Apply: func(ts *TradingSettings, tr *TrailingConfig) {
			ts.PositionPct = 3.0
			ts.RiskPct = 2.0
			ts.StopPct = 2.5
			ts.TakeProfitRR = 3.0
			ts.Leverage = 20
		},
	},
}
var TrailingPresets = map[string]TrailingPreset{
	"safe": {
		Name:        "🟢 Осторожный трейлинг",
		Description: "Рано защищаем сделку, меньше откатов и стресса",
		Apply: func(tr *TrailingConfig) {
			// BE — рано в безубыток
			tr.BETriggerR = 0.4
			tr.BEOffsetR = 0.0

			// Lock — аккуратная фиксация
			tr.LockTriggerR = 0.8
			tr.LockOffsetR = 0.2

			// TimeStop — быстро режем слабые входы
			tr.TimeStopBars = 8
			tr.TimeStopMinMFER = 0.3

			// Partial — фиксируем половину
			tr.PartialEnabled = true
			tr.PartialTriggerR = 0.8
			tr.PartialCloseFrac = 0.6
		},
	},

	"mid": {
		Name:        "🟡 Сбалансированный трейлинг",
		Description: "Компромисс между безопасностью и потенциалом роста",
		Apply: func(tr *TrailingConfig) {
			tr.BETriggerR = 0.6
			tr.BEOffsetR = 0.0

			tr.LockTriggerR = 1.0
			tr.LockOffsetR = 0.3

			tr.TimeStopBars = 12
			tr.TimeStopMinMFER = 0.4

			tr.PartialEnabled = true
			tr.PartialTriggerR = 1.0
			tr.PartialCloseFrac = 0.5
		},
	},

	"aggr": {
		Name:        "🔴 Агрессивный трейлинг",
		Description: "Максимум свободы для цены, минимум ранних выходов",
		Apply: func(tr *TrailingConfig) {
			// BE — позже, даём тренду развиться
			tr.BETriggerR = 1.0
			tr.BEOffsetR = 0.1

			// Lock — поздний, но жёсткий
			tr.LockTriggerR = 1.5
			tr.LockOffsetR = 0.5

			// TimeStop — терпим дольше
			tr.TimeStopBars = 20
			tr.TimeStopMinMFER = 0.6

			// Partial — либо мало, либо вообще выключено
			tr.PartialEnabled = false
			tr.PartialTriggerR = 0.0
			tr.PartialCloseFrac = 0.0
		},
	},
}
