// Copyright 2025 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/trace"
)

type azugoContext struct{}

type traceExtendedContextKeyType int

const currentExtendedContextKey traceExtendedContextKeyType = iota

func (azugoContext) Context(ctx context.Context) context.Context {
	pctx := FromContext(ctx)
	if pctx == nil {
		return nil
	}

	// If the parent context is the same as the current context or marked, avoid recursion.
	if pctx == ctx || pctx.Value(currentExtendedContextKey) != nil {
		return nil
	}

	// Prevent recursion by marking the context.
	pctx = context.WithValue(pctx, currentExtendedContextKey, struct{}{})

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
