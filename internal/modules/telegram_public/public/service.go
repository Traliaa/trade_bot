package public

import (
	"context"
	"sync"
	"time"
	"trade_bot/pkg/logger"

	"go.uber.org/zap"
)

type Service struct {
	n PublicNotifier
	r Repo

	heartbeatEvery time.Duration

	mu   sync.Mutex
	last Status

	stop chan struct{}
}

func NewService(n PublicNotifier, r Repo) *Service {
	return &Service{
		n:              n,
		r:              r,
		heartbeatEvery: 5 * time.Minute,
		stop:           make(chan struct{}),
	}
}

func (s *Service) Start(ctx context.Context) {
	t := time.NewTicker(s.heartbeatEvery)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-t.C:
				s.Heartbeat(context.Background())
			}
		}
	}()
}

func (s *Service) Stop() { close(s.stop) }

func (s *Service) Set(ctx context.Context, st Status) {
	st.UpdatedAt = time.Now()

	s.mu.Lock()
	s.last = st
	s.mu.Unlock()

	if err := s.sendOrEdit(ctx, st); err != nil {
		logger.Error("public status send/edit failed", zap.Error(err))
	}
}

func (s *Service) Heartbeat(ctx context.Context) {
	s.mu.Lock()
	st := s.last
	s.mu.Unlock()

	if st.State == "" {
		return
	}

	st.UpdatedAt = time.Now()
	if err := s.sendOrEdit(ctx, st); err != nil {
		logger.Error("public status heartbeat failed", zap.Error(err))
	}
}

func (s *Service) sendOrEdit(ctx context.Context, st Status) error {
	meta, ok, err := s.r.Get(ctx)
	if err != nil {
		return err
	}

	text := st.Render()

	if !ok || meta.MessageID == 0 {
		id, err := s.n.SendServiceText(ctx, text)
		if err != nil {
			return err
		}
		return s.r.Upsert(ctx, Meta{MessageID: id})
	}

	return s.n.EditServiceText(ctx, meta.MessageID, text)
}
