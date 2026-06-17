// Copyright 2025 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/trace"
)

// FromContext returns a context carrying the active span for the request,
// suitable as the parent for new spans.
func FromContext(ctx context.Context) context.Context {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ctx
	}

	if c := azugo.RequestContext(ctx); c != nil {
		return trace.ContextWithSpan(c.Context(), span)
	}

	return ctx
}

// Recording reports whether a span started for ctx would be part of active
// tracing, and therefore eventually exported. Use it to guard custom
// instrumentation so it does not emit orphan spans for untraced requests:
//
//	if opentelemetry.Recording(ctx) {
//		ctx, span := tracer.Start(ctx, "my-operation")
//		defer span.End()
//		// ...
//	}
//
// Outside of a request context it always returns true.
func Recording(ctx context.Context) bool {
	rc := azugo.RequestContext(ctx)
	if rc == nil {
		return true
	}

	return !rc.IsSkipRequestLog() && trace.SpanFromContext(ctx).SpanContext().IsValid()
}
