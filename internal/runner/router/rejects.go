package router

import "trade_bot/internal/modules/strategy/service"

func (r *Router) StrategyRejects(reset bool) service.RejectSnapshot {
	return r.engine.RejectSnapshot(reset)
}
