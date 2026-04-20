// Copyright 2026 Azugo
// SPDX-License-Identifier: Apache-2.0

// Package nethttp provides OpenTelemetry tracing instrumentation for standard library net/http clients.
package nethttp

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

// Transport wraps an http.RoundTripper with OpenTelemetry tracing instrumentation.
// Spans are named "METHOD scheme://host" (e.g. "GET https://api.example.com"),
// consistent with azugo HTTP client tracing. The span name formatter can be
// overridden via opts.
func Transport(r http.RoundTripper, opts ...otelhttp.Option) *otelhttp.Transport {
	allOpts := make([]otelhttp.Option, 0, len(opts)+3)

	// Default options
	allOpts = append(allOpts, otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		if r.URL == nil {
			return r.Method
		}

		return r.Method + " " + r.URL.Scheme + "://" + r.URL.Host
	}))

	allOpts = append(allOpts, opts...)

	// Required options
	allOpts = append(allOpts,
		otelhttp.WithPropagators(otel.GetTextMapPropagator()),
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
	)

	return otelhttp.NewTransport(r, allOpts...)
}
