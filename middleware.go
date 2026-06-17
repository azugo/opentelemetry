// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"strings"

	"azugo.io/opentelemetry/internal/semconvutil"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	// ScopeName is the instrumentation scope name.
	ScopeName = "azugo.io/opentelemetry"
)

const otelContextFieldKey = "__otelContext"

// tracingMiddleware builds the middleware that starts a span for each incoming
// request. The service parameter should describe the name of the (virtual)
// server handling the request.
func tracingMiddleware(opts ...Option) func(azugo.RequestHandler) azugo.RequestHandler {
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
	instrSpanNameFormatter func(ctx context.Context, op string, args ...any) string
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
		if ctx.IsSkipRequestLog() {
			// Request logging/tracing was already disabled before reaching the
			// tracing middleware — pass through without starting a span.
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

		c := tw.propagators.Extract(ctx.Context(), azugoHeaderCarrier(ctx))

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

		// Subtrace spans created during this request (rate limiter, cache, HTTP
		// client, ...) defer their End so they can be dropped together with the
		// request if the handler opts out of tracing.
		ds := &deferredSpans{}
		ctx.SetUserValue(deferredUserValueKey, ds)

		ctx.SetContext(c)

		_ = ctx.AddLogFields(
			zap.Field{Key: otelContextFieldKey, Type: zapcore.SkipType, Interface: span.SpanContext()},
			zap.String("span.id", span.SpanContext().SpanID().String()),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)

		next(ctx)

		// The handler may disable request tracing only after the span was
		// started (e.g. healthz via Context.SkipRequestLog). In that case drop
		// the request span (by not ending it) together with all deferred
		// subtrace spans, so none of them are exported.
		if ctx.IsSkipRequestLog() {
			ds.finalize(false)

			return
		}

		ds.finalize(true)

		status := ctx.Response().StatusCode()
		if status > 0 {
			span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		}

		span.SetStatus(semconvutil.HTTPServerStatus(status))

		span.End()
	}
}
