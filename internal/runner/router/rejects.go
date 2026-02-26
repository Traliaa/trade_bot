package router

import (
	"time"
	"trade_bot/internal/modules/strategy/service"
)

func (r *Router) StrategyRejects(reset bool) service.RejectSnapshot {
	return r.engine.RejectSnapshot(reset)
}
func (r *Router) StrategyTuning() (service.RuntimeTuning, time.Time, time.Time) {
	return r.engine.CurrentTuning()
}
