package service

import (
	"fmt"
	"trade_bot/internal/models"
)

func formatTradeSettings(ts *models.TradingSettings) string {
	return fmt.Sprintf(
		"*⚙️ Торговля*\n\n"+
			"Плечо: `%dx`\n"+
			"Макс. позиций: `%d`\n"+
			"Размер позиции: `%s%%`\n\n"+
			"Подтверждения: *%s*\n"+
			"Timeout: `%s`\n"+
			"Cooldown: `%s`\n",
		ts.Leverage,
		ts.MaxOpenPositions,
		f2(ts.PositionPct),
		onOff(ts.ConfirmRequired),
		ts.ConfirmTimeout.String(),
		ts.CooldownPerSymbol.String(),
	)
}

func formatRiskSettings(ts *models.TradingSettings) string {
	return fmt.Sprintf(
		"*📉 Риск / SL / TP*\n\n"+
			"Risk: `%s%%` на сделку\n"+
			"Stop: `%s%%`\n"+
			"TP: `%sR`\n",
		f2(ts.RiskPct),
		f2(ts.StopPct),
		f2(ts.TakeProfitRR),
	)
}

func formatTrailing(cfg *models.TrailingConfig) string {
	return fmt.Sprintf(
		"*🧲 Trailing / Partial*\n\n"+
			"*BE*\n"+
			"  Trigger: `%sR`\n"+
			"  Offset:  `%sR`\n\n"+
			"*Lock*\n"+
			"  Trigger: `%sR`\n"+
			"  Offset:  `%sR`\n\n"+
			"*TimeStop*\n"+
			"  Bars: `%d`\n"+
			"  MinMFE: `%sR`\n\n"+
			"*Partial*\n"+
			"  Enabled: *%s*\n"+
			"  Trigger: `%sR`\n"+
			"  Close: `%s%%`\n",
		f2(cfg.BETriggerR),
		f2(cfg.BEOffsetR),
		f2(cfg.LockTriggerR),
		f2(cfg.LockOffsetR),
		cfg.TimeStopBars,
		f2(cfg.TimeStopMinMFER),
		onOff(cfg.PartialEnabled),
		f2(cfg.PartialTriggerR),
		f2(cfg.PartialCloseFrac*100),
	)
}
