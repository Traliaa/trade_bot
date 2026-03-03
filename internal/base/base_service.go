package base

import (
	"context"
	"errors"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// ErrStop ...
var ErrStop = errors.New("сервис остановлен")

// Service - обобщенный интерфейс для сервиса с функциями запуска и остановки
type Service interface {
	Start(ctx context.Context) error
	Started() <-chan struct{}
	Stop()
}

// Base — структура, предназначенная для встраивания в "модули"
type Base struct {
	StartStop
	Logger *zap.Logger

	// Name — имя модуля
	Name string

	IsStartNow bool
}

// New ...
func New(moduleName string, root *zap.Logger, isStartNow bool) Base {
	var l *zap.Logger
	if root == nil {
		l = zap.NewNop()
	} else {
		l = root.With(zap.String("component", moduleName))
	}
	return Base{
		Name:       moduleName,
		Logger:     l,
		IsStartNow: isStartNow,
	}

	//b.Init(ctx, b)

}

// NewForTest ...
func NewForTest(moduleName string) Base {
	// zaptest.NewLogger дает нормальный логгер для тестов (пишет в test output)
	l := zaptest.NewLogger(nil).With(zap.String("component", moduleName))

	return Base{
		Name:   moduleName,
		Logger: l,
	}
}

// GetBaseService ...
func (s *Base) GetBaseService() *Base { return s }
