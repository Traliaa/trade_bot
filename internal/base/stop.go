package base

import (
	"sync"
)

// Stop - автоматическая реализация метода Stop интерфейса Service.
func (s *StartStop) Stop() {
	shouldStop, stopped, finalizeStop := s.StopInit()
	if !shouldStop {
		return
	}

	<-stopped
	finalizeStop(true)
}

// StopInit предоставляет способ создания кастомной реализации Stop.
func (s *StartStop) StopInit() (bool, <-chan struct{}, func(didStop bool)) {
	s.mu.Lock()

	if !s.isRunning {
		s.mu.Unlock()
		return false, nil, func(_ bool) {}
	}

	s.cancelFunc(ErrStop)

	return true, s.stopped, func(didStop bool) {
		defer s.mu.Unlock()
		if didStop {
			s.isRunning = false
			s.started = nil
			s.stopped = nil
		}
	}
}

// Stopped возвращает канал для ожидания остановки сервиса.
func (s *StartStop) Stopped() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped == nil {
		s.stopped = make(chan struct{})
	}

	return s.stopped
}

// StoppedUnsafe - небезопасная версия Stopped без блокировок.
func (s *StartStop) StoppedUnsafe() <-chan struct{} { return s.stopped }

// StopAllParallel останавливает сервисы параллельно.
func StopAllParallel(services ...Service) {
	var wg sync.WaitGroup
	wg.Add(len(services))

	for i := range services {
		go func(s Service) {
			defer wg.Done()
			s.Stop()
		}(services[i])
	}
	wg.Wait()
}
