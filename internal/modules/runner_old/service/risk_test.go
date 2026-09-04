package service

import (
	"testing"

	"trade_bot/internal/models"
)

func TestActualRiskUSDTScalesConfiguredRiskToFilledSize(t *testing.T) {
	params := &models.TradeParams{
		Size: 8,
		SizeMeta: &models.SizeCalcResult{
			RawRiskSz: 10,
			RiskUSDT:  25,
		},
	}

	if got := actualRiskUSDT(params); got != 20 {
		t.Fatalf("expected actual risk 20 USDT, got %v", got)
	}
}
