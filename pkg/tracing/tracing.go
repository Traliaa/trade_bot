package tracing

import (
	"fmt"
	"trade_bot/pkg/logger"

	"github.com/opentracing/opentracing-go"
	jCfg "github.com/uber/jaeger-client-go/config"
	"github.com/uber/jaeger-lib/metrics"
)

type ctxKey string

const (
	TraceIDKey ctxKey = "trace_id"
	SpanIDKey  ctxKey = "span_id"
)

var (
	// Неверное не самое элегантное решение, но лучше чем выносить константу в отдельный пакет
	// лучше инициализирвоать при инстанцировании через аргументы.
	serviceName = "default"
)

func SetServiceName(newName string) string {
	oldName := serviceName
	serviceName = newName

	return oldName
}

type Config struct {
	Host string
	Port int
}

func InitTracer(conf Config) (opentracing.Tracer, func(), error) {
	cfg := &jCfg.Configuration{
		ServiceName: serviceName,
		Sampler: &jCfg.SamplerConfig{
			Type:  "const",
			Param: 1,
		},
		Reporter: &jCfg.ReporterConfig{
			LogSpans:           true,
			LocalAgentHostPort: fmt.Sprintf("%s:%d", conf.Host, conf.Port),
		},
	}

	jMetricsFactory := metrics.NullFactory
	tracer, closer, err := cfg.NewTracer(
		jCfg.Metrics(jMetricsFactory),
	)
	if err != nil {
		return nil, nil, err
	}

	opentracing.SetGlobalTracer(tracer)
	return tracer, func() {
		if err := closer.Close(); err != nil {
			logger.Fatal("Error closing Jaeger tracer: %v", err)
		}
	}, nil
}
