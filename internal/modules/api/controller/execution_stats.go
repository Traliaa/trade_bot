package controller

import "net/http"

func (c *TradeController) ExecutionStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := mustAuthUserID(w, r); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if source, ok := c.r.(interface{ ExecutionStats() map[string]uint64 }); ok {
		writeJSON(w, map[string]any{"scope": "current_process", "counts": source.ExecutionStats()})
		return
	}
	http.Error(w, "execution diagnostics unavailable", http.StatusServiceUnavailable)
}
