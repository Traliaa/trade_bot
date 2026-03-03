package router

import (
	"time"
	"trade_bot/internal/models"
)

func (r *Router) StrategyRejects(reset bool) models.RejectSnapshot {
	return r.engine.RejectSnapshot(reset)
}
func (r *Router) StrategyTuning() (models.RuntimeTuning, time.Time, time.Time) {
	return r.engine.CurrentTuning()
}
