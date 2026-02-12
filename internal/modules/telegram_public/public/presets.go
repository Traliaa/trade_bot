package public

import "context"

func (s *Service) Restarting(ctx context.Context, exchange string, instruments int) {
	s.Set(ctx, Status{State: StateRestarting, Exchange: exchange, Instruments: instruments})
}

func (s *Service) Preparing(ctx context.Context, exchange string, instruments int, progress int) {
	s.Set(ctx, Status{State: StatePreparing, Exchange: exchange, Instruments: instruments, Progress: progress})
}

func (s *Service) Ready(ctx context.Context, exchange string, instruments int) {
	s.Set(ctx, Status{State: StateReady, Exchange: exchange, Instruments: instruments, Progress: 100})
}

func (s *Service) Paused(ctx context.Context, exchange string, instruments int, reason string) {
	s.Set(ctx, Status{State: StatePaused, Exchange: exchange, Instruments: instruments, PauseReason: reason})
}

func (s *Service) Error(ctx context.Context, exchange string, instruments int, hint string) {
	s.Set(ctx, Status{State: StateError, Exchange: exchange, Instruments: instruments, ErrorHint: hint})
}
