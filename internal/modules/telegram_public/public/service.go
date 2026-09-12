package public

import (
	"context"
	"sync"
	"time"
	"trade_bot/pkg/logger"
)

type Service struct {
	n PublicNotifier
	r Repo

	mu             sync.Mutex
	last           Status
	events         []Status
	incidentActive bool
	history        HistoryStore
}

func NewService(n PublicNotifier, r Repo) *Service {
	return &Service{
		n:    n,
		r:    r,
		last: Status{State: StateRestarting, UpdatedAt: time.Now()},
	}
}

func (s *Service) Start(ctx context.Context) {
	// Status is read on demand by the mini app, not republished to Telegram.
}

func (s *Service) Stop() {}

func (s *Service) Set(ctx context.Context, st Status) {
	if err := s.SendOrEdit(ctx, st); err != nil {
		logger.Error("public status send/edit failed: %v", err)
	}
}

func (s *Service) Heartbeat(ctx context.Context) {
	// Compatibility only. A timer must not manufacture a fresh health timestamp.
}

func (s *Service) SendOrEdit(ctx context.Context, st Status) error {
	st.UpdatedAt = time.Now()
	s.mu.Lock()
	previous := s.last.State
	history := s.history
	s.last = st
	alert := false
	if st.State == StateError && !s.incidentActive {
		s.incidentActive = true
		alert = true
	} else if st.State == StateReady && s.incidentActive {
		s.incidentActive = false
		alert = true
	}
	if previous != st.State {
		s.events = append(s.events, st)
		if len(s.events) > 50 {
			s.events = s.events[len(s.events)-50:]
		}
	}
	s.mu.Unlock()
	if previous != st.State && history != nil {
		historyCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		if err := history.Append(historyCtx, st); err != nil {
			logger.Error("status history write failed: %v", err)
		}
		cancel()
	}
	// Send one startup failure and one recovery; routine progress is silent.
	if s.n != nil && alert {
		notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, err := s.n.SendServiceText(notifyCtx, st.RenderHTML()); err != nil {
			logger.Error("public status alert failed: %v", err)
		}
	}
	return nil // Telegram must never gate strategy readiness.
}

func (s *Service) Snapshot() (Status, []Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, append([]Status(nil), s.events...)
}
func (s *Service) SendServiceText(ctx context.Context, text string) (messageID int, err error) {
	return s.n.SendServiceText(ctx, text)
}
