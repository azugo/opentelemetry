// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"strings"

	"azugo.io/opentelemetry/internal/semconvutil"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// ScopeName is the instrumentation scope name.
	ScopeName = "azugo.io/opentelemetry"
)

const otelParentSpanContext = "__otelParentSpanContext"

// middleware sets up a handler to start tracing the incoming
// requests.  The service parameter should describe the name of the
// (virtual) server handling the request.
func middleware(opts ...Option) func(azugo.RequestHandler) azugo.RequestHandler {
	cfg := traceConfig(opts...)

	tracer := cfg.TracerProvider.Tracer(
		ScopeName+"/router",
		trace.WithInstrumentationVersion(Version()),
		trace.WithInstrumentationAttributes(semconv.TelemetrySDKLanguageGo),
	)

	return func(h azugo.RequestHandler) azugo.RequestHandler {
		t := traceware{
			tracer:                 tracer,
			propagators:            cfg.Propagators,
			routeSpanNameFormatter: cfg.routeSpanNameFormatter,
			instrSpanNameFormatter: cfg.instrSpanNameFormatter,
			publicEndpoint:         cfg.PublicEndpoint,
			publicEndpointFn:       cfg.PublicEndpointFn,
			filters:                cfg.Filters,
		}

		return t.handle(h)
	}
}

type traceware struct {
	tracer                 trace.Tracer
	propagators            propagation.TextMapPropagator
	routeSpanNameFormatter func(ctx *azugo.Context, routeName string) string
	instrSpanNameFormatter func(ctx context.Context, op string, args ...interface{}) string
	publicEndpoint         bool
	publicEndpointFn       func(ctx *azugo.Context) bool
	filters                []Filter
}

// defaultRouteSpanNameFunc just reuses the route name as the span name.
func defaultRouteSpanNameFunc(ctx *azugo.Context, routeName string) string {
	var s strings.Builder

	s.WriteString(ctx.Method())
	s.WriteByte(' ')
	s.WriteString(routeName)

	return s.String()
}

// handle implements the azugo.RequestHandler interface. It does the actual
// tracing of the request.
func (tw traceware) handle(next azugo.RequestHandler) func(ctx *azugo.Context) {
	return func(ctx *azugo.Context) {
		if val, ok := ctx.UserValue("__log_request").(bool); !ok || !val {
			// If the request is not to be logged, simply pass through to the handler
			next(ctx)

			return
		}

		for _, f := range tw.filters {
			if !f(ctx) {
				// Simply pass through to the handler if a filter rejects the request
				next(ctx)

				return
			}
		}

		c := tw.propagators.Extract(ctx, azugoHeaderCarrier(ctx))
		if ac, ok := c.(*azugo.Context); ok {
			ctx = ac
		}

		opts := []trace.SpanStartOption{
			trace.WithAttributes(semconvutil.HTTPServerRequest(ctx)...),
			trace.WithSpanKind(trace.SpanKindServer),
		}

		if tw.publicEndpoint || (tw.publicEndpointFn != nil && tw.publicEndpointFn(ctx)) {
			opts = append(opts, trace.WithNewRoot())
			// Linking incoming span context if any for public endpoint.
			if s := trace.SpanContextFromContext(c); s.IsValid() && s.IsRemote() {
				opts = append(opts, trace.WithLinks(trace.Link{SpanContext: s}))
			}
		}

		routeStr := ctx.RouterPath()
		if routeStr == "" {
			routeStr = "route not found"
		} else {
			rAttr := semconv.HTTPRoute(routeStr)
			opts = append(opts, trace.WithAttributes(rAttr))
		}

		spanName := tw.routeSpanNameFormatter(ctx, routeStr)
		c, span := tw.tracer.Start(c, spanName, opts...)

		ctx.SetUserValue(otelParentSpanContext, c)

		next(ctx)

		status := ctx.Response().StatusCode()
		if status > 0 {
			span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		}

		span.SetStatus(semconvutil.HTTPServerStatus(status))

		span.End()
	}
}
