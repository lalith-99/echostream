package observ

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var TraceContext = propagation.TraceContext{}

// ConfigureTracing enables batched OTLP/HTTP export only when an endpoint is configured.
func ConfigureTracing(ctx context.Context) (func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(attribute.String("service.name", "echostream")))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	TraceContext.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// StartLinked restores the parent span carried by an outbox row or Redis envelope.
func StartLinked(ctx context.Context, parent, name string) (context.Context, trace.Span) {
	ctx = TraceContext.Extract(ctx, propagation.MapCarrier{"traceparent": parent})
	return otel.Tracer("echostream").Start(ctx, name)
}
