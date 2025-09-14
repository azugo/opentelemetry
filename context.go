package opentelemetry

import (
	"context"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/trace"
)

type azugoContext struct{}

func (azugoContext) Context(ctx context.Context) context.Context {
	pctx := FromContext(ctx)
	if pctx == nil {
		return nil
	}

	spanCtx := trace.SpanContextFromContext(pctx)
	if !spanCtx.IsValid() {
		return nil
	}

	c := trace.ContextWithSpanContext(ctx, spanCtx)
	if span := trace.SpanFromContext(pctx); span != nil {
		return trace.ContextWithSpan(c, span)
	}

	return c
}

// FromContext extracts the parent span context from the Azugo context.
func FromContext(ctx context.Context) context.Context {
	c := azugo.RequestContext(ctx)
	if c == nil {
		return ctx
	}

	val := c.UserValue(otelParentSpanContext)
	if val == nil {
		return ctx
	}

	sc, ok := val.(context.Context)
	if !ok {
		return ctx
	}

	return sc
}
