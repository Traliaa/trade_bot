package runner

import (
	"context"
	"trade_bot/internal/models"
)

// Runner отвечает за одну учётку: confirm + ордера.
func (r *Runner) HandleSignal(ctx context.Context, sig models.Signal) {
	// тут по сути твоя логика:
	//  - лимит по открытым позициям
	//  - Confirm через Telegram (если включено)
	//  - calcTradeParams
	//  - PlaceMarket + PlaceTpsl
	// всё то, что уже написано в confirmWorker, только инициатор – не свои свечи, а sig
	r.queue <- models.Signal{
		InstID: sig.InstID,
		Price:  sig.Price,
		Side:   sig.Side,
		Reason: sig.Reason,
	}
}
