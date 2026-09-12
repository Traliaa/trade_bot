package service

import (
	"context"
	"fmt"
	"time"
	"trade_bot/internal/modules/runner_old/sessions"
)

type noticeWindow struct {
	At         time.Time
	Suppressed uint64
}

func (r *Service) countDecision(reason string) {
	r.diagnosticsMu.Lock()
	defer r.diagnosticsMu.Unlock()
	if r.diagnostics == nil {
		r.diagnostics = map[string]uint64{}
	}
	r.diagnostics[reason]++
}
func (r *Service) ExecutionStats() map[string]uint64 {
	r.diagnosticsMu.Lock()
	defer r.diagnosticsMu.Unlock()
	out := map[string]uint64{}
	for k, v := range r.diagnostics {
		out[k] = v
	}
	return out
}
func (r *Service) noticeAllowed(key string, now time.Time) (bool, uint64) {
	r.diagnosticsMu.Lock()
	defer r.diagnosticsMu.Unlock()
	if r.notices == nil {
		r.notices = map[string]noticeWindow{}
	}
	w, ok := r.notices[key]
	if ok && now.Sub(w.At) < 15*time.Minute {
		w.Suppressed++
		r.notices[key] = w
		return false, 0
	}
	if len(r.notices) >= 10000 {
		for k, v := range r.notices {
			if now.Sub(v.At) >= 15*time.Minute {
				delete(r.notices, k)
			}
		}
		if len(r.notices) >= 10000 {
			return false, 0
		}
	}
	r.notices[key] = noticeWindow{At: now}
	return true, w.Suppressed
}

// Only routine limit rejections are throttled. Trade events and errors remain immediate.
func (r *Service) notifyReject(ctx context.Context, s *sessions.UserSession, reason, format string, args ...any) {
	r.countDecision(reason)
	allowed, suppressed := r.noticeAllowed(fmt.Sprintf("%d/%s", s.User.TelegramID, reason), time.Now())
	if !allowed {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if suppressed > 0 {
		msg += fmt.Sprintf("\nПовторных уведомлений этой причины за интервал скрыто: %d. Все отказы учтены в диагностике.", suppressed)
	}
	s.Notifier.Send(ctx, s.User.TelegramID, msg)
}
