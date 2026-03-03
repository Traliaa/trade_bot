package base

import (
	"context"
	"sync"
)

// serviceWithStopped
type serviceWithStopped interface {
	Service

	// Stopped возвращает канал для ожидания остановки сервиса.
	Stopped() <-chan struct{}
}

// StartStop ...
type StartStop struct {
	cancelFunc context.CancelCauseFunc // функция отмены контекста
	isRunning  bool                    // флаг работы сервиса
	mu         sync.Mutex              // мьютекс для синхронизации
	started    chan struct{}           // канал запуска
	stopped    chan struct{}           // канал остановки
}

//// Init должен вызываться в начале функции Start сервиса.
//func (s *StartStop) Init(ctx context.Context, b *Base) {
//	go func() {
//		if b.IsStartNow {
//			b.SetState(true)
//		}
//		if err := b.rtCfgClient.WatchVariable(
//			ctx, b.configKeyIsRTC, func(_, newVariable realtimeconfig.Variable) {
//				if v := newVariable.Value().Bool(); v {
//					s.SetState(true)
//					return
//				}
//				s.SetState(false)
//			},
//		); err != nil {
//			b.Logger.Error(ctx, "error on subscribe", err, watermill.LogFields{"key": b.configKeyIsRTC})
//			s.Stop()
//		}
//	}()
//}

//func (s *StartStop) SetState(b bool) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	if t.state == state {
//		return
//	}
//	t.state = state
//	if !state {
//		t.stop()
//		return
//	}
//	s.Start(context.Background())
//}

// StartInit должен вызываться в начале функции Start сервиса.
func (s *StartStop) StartInit(ctx context.Context) (context.Context, bool, func(), func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return ctx, false, nil, nil
	}

	s.isRunning = true

	// Создаем каналы только если они не были предварительно созданы
	if s.started == nil {
		s.started = make(chan struct{})
	}
	if s.stopped == nil {
		s.stopped = make(chan struct{})
	}

	ctx, s.cancelFunc = context.WithCancelCause(ctx)

	closeStartedOnce := sync.OnceFunc(func() { close(s.started) })

	return ctx, true, closeStartedOnce, func() {
		closeStartedOnce() // Гарантированное закрытие канала запуска
		close(s.stopped)   // Закрытие канала остановки
	}
}

// Started возвращает канал, который закрывается при завершении запуска сервиса
func (s *StartStop) Started() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started == nil {
		s.started = make(chan struct{})
	}

	return s.started
}

// startStopFunc - реализация Service через функцию.
type startStopFunc struct {
	StartStop
	startFunc func(ctx context.Context, shouldStart bool, started, stopped func()) error
}

// StartStopFunc создает Service из функции.
func StartStopFunc(startFunc func(ctx context.Context, shouldStart bool, started, stopped func()) error) *startStopFunc {
	return &startStopFunc{
		startFunc: startFunc,
	}
}

func (s *startStopFunc) Start(ctx context.Context) error {
	return s.startFunc(s.StartInit(ctx))
}

// StartAll запускает все сервисы. При ошибке останавливает уже запущенные.
func StartAll(ctx context.Context, services ...Service) error {
	for i, service := range services {
		if err := service.Start(ctx); err != nil {
			StopAllParallel(services[0:i]...)
			return err
		}
	}
	return nil
}

// WaitAllStarted ожидает запуска всех сервисов.
func WaitAllStarted(services ...Service) {
	<-WaitAllStartedC(services...)
}

// WaitAllStartedC возвращает канал для ожидания запуска сервисов.
func WaitAllStartedC(services ...Service) <-chan struct{} {
	allStarted := make(chan struct{})

	go func() {
		defer close(allStarted)
		for _, service := range services {
			<-service.Started()
		}
	}()

	return allStarted
}

func (s *StartStop) StartRun(
	ctx context.Context,
	run func(ctx context.Context) error,
) error {
	ctx, shouldStart, _, stopped := s.StartInit(ctx)
	if !shouldStart {
		return nil
	}

	// гарантируем закрытия при любом исходе
	defer stopped()

	// если модуль “стартанулся” сразу — вызывает started() в нужный момент
	// либо можно вызвать started() сразу здесь, если тебе так надо по семантике
	err := run(ctx)
	if err != nil {
		// started() всё равно закроется в stopped()
		return err
	}
	// если успешный старт означает “сервис запущен”, а run блокирует до stop — started() лучше закрывать раньше:
	// started()
	return nil
}
