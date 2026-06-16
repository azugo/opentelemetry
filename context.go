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
