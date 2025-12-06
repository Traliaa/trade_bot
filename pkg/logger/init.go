package logger

import (
	"log"
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var once = sync.Once{}

func init() {
	once.Do(func() {
		if err := initLoggers(); err != nil {
			log.Fatalf("failed to init global logger: %v", err)
		}
	})
}

func initLoggers() error {
	var err error

	InfoLogger, err = initLogger(zapcore.InfoLevel)
	if err != nil {
		return err
	}

	FatalLogger, err = initLogger(zapcore.FatalLevel)
	if err != nil {
		return err
	}

	return nil
}

func initLogger(logLevel zapcore.Level) (*zap.Logger, error) {
	logConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	writer := zapcore.AddSync(os.Stdout)
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(logConfig),
		writer,
		logLevel,
	)

	logger := zap.New(core)
	return logger, nil
}
