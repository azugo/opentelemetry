// Copyright 2025 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/trace"
)

type azugoContext struct{}

func (azugoContext) Context(ctx context.Context) context.Context {
	pctx := FromContext(ctx)
	// If the parent context is the same as the current context avoid recursion.
	if pctx == nil || pctx == ctx {
		return nil
	}

	span := trace.SpanFromContext(pctx)
	if !span.SpanContext().IsValid() {
		return nil
	}

	rctx := azugo.RequestContext(ctx)
	if rctx == nil || rctx.Context() == nil {
		return nil
	}

	return trace.ContextWithSpan(rctx.Context(), span)
}

// FromContext returns the parent span context stored in the Azugo request context, if any.
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
