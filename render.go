package opentelemetry

import (
	"context"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const templRenderOp = "templ-render"

func renderRecorder(ctx context.Context, tr trace.Tracer, _ propagation.TextMapPropagator, spfmt InstrumentationSpanNameFormatter, op string, args ...any) (func(err error), bool) {
	if op != templRenderOp {
		return nil, false
	}

	if !Recording(ctx) {
		return nil, false
	}

	spanName := spfmt(ctx, op, args...)
	if spanName == "" {
		spanName = "render"

		if len(args) > 0 {
			if name, _ := args[0].(string); name != "" {
				spanName = "render " + name
			}
		}
	}

	_, span := StartSpan(ctx, tr, spanName, trace.WithSpanKind(trace.SpanKindInternal))

	return func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())

			span.RecordError(err, trace.WithStackTrace(true))
		}

		span.End()
	}, true
}
