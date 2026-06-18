package opentelemetry

import (
	"context"

	"azugo.io/core/ratelimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

func ratelimitRecorder(ctx context.Context, tr trace.Tracer, _ propagation.TextMapPropagator, spfmt InstrumentationSpanNameFormatter, op string, args ...any) (func(err error), bool) {
	var (
		key    string
		method string

		res *ratelimit.Result
		ok  bool
	)

	switch op {
	case ratelimit.InstrumentationAllow:
		key, res, ok = ratelimit.InstrAllow(op, args...)
		method = "ALLOW "
	case ratelimit.InstrumentationPeek:
		key, res, ok = ratelimit.InstrPeek(op, args...)
		method = "PEEK "
	case ratelimit.InstrumentationWait:
		key, ok = ratelimit.InstrWait(op, args...)
		method = "WAIT "
	case ratelimit.InstrumentationReset:
		key, ok = ratelimit.InstrReset(op, args...)
		method = "RESET "
	default:
		return nil, false
	}

	if !ok {
		return nil, false
	}

	if !Recording(ctx) {
		return nil, false
	}

	spanName := spfmt(ctx, op, args...)
	if spanName == "" {
		spanName = method + key
	}

	opts := []trace.SpanStartOption{
		trace.WithAttributes(
			semconv.ServicePeerName("ratelimit"),
		),
		trace.WithSpanKind(trace.SpanKindInternal),
	}

	_, span := StartSpan(ctx, tr, spanName, opts...)

	return func(err error) {
		if res != nil {
			span.SetAttributes(
				attribute.Bool("ratelimit.allowed", res.Allowed),
				attribute.Int("ratelimit.remaining", res.Remaining),
			)

			if res.RetryAfter > 0 {
				span.SetAttributes(attribute.Int64("ratelimit.retry_after_ms", res.RetryAfter.Milliseconds()))
			}
		}

		if err != nil {
			span.SetStatus(codes.Error, err.Error())

			span.RecordError(err, trace.WithStackTrace(true))
		}

		span.End()
	}, true
}
