// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"azugo.io/opentelemetry/internal/semconvutil"

	"azugo.io/azugo"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// goroutineTraceContexts is a thread-safe map to store trace contexts.
var goroutineTraceContexts sync.Map

const (
	// ScopeName is the instrumentation scope name.
	ScopeName = "azugo.io/opentelemetry"
)

const otelParentSpanContext = "__otelParentSpanContext"

// middleware sets up a handler to start tracing the incoming
// requests.  The service parameter should describe the name of the
// (virtual) server handling the request.
func middleware(config *Configuration, opts ...Option) func(azugo.RequestHandler) azugo.RequestHandler {
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
			config:                 config,
		}

		return t.handle(h)
	}
}

func panicHandler(ctx *azugo.Context, val any) {
	ctx.Log().Error("Unhandled error", zap.Any("error", val))

	c := FromContext(ctx)

	span := trace.SpanFromContext(c)

	var err error
	if e, ok := val.(error); ok {
		err = e
	} else if e, ok := val.(string); ok {
		err = errors.New(e)
	}

	if span.SpanContext().IsValid() && span.IsRecording() {
		span.SetAttributes(semconv.HTTPResponseStatusCode(fasthttp.StatusInternalServerError))
		span.SetStatus(codes.Error, fmt.Sprintf("%v", val))

		if err != nil {
			span.RecordError(err, trace.WithStackTrace(true))
		}

		span.End()
	}

	if err != nil {
		ctx.Error(err)
	} else {
		ctx.StatusCode(fasthttp.StatusInternalServerError)
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
	config                 *Configuration
}

// defaultRouteSpanNameFunc just reuses the route name as the span name.
func defaultRouteSpanNameFunc(ctx *azugo.Context, routeName string) string {
	var s strings.Builder

	s.WriteString(ctx.Method())
	s.WriteByte(' ')
	s.WriteString(routeName)

	return s.String()
}

func goroutineID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	b = b[:bytes.IndexByte(b, ' ')]
	n, _ := strconv.ParseUint(string(b), 10, 64)

	return n
}

func currentSpanCtx() trace.SpanContext {
	gid := goroutineID()

	if val, ok := goroutineTraceContexts.Load(gid); ok {
		if spanCtx, ok := val.(trace.SpanContext); ok && spanCtx.IsValid() {
			return spanCtx
		}

		goroutineTraceContexts.Delete(gid)
	}

	return trace.SpanContext{}
}

func clearTraceCtx() {
	gid := goroutineID()
	goroutineTraceContexts.Delete(gid)
}

func setTraceCtx(spanCtx trace.SpanContext) {
	if !spanCtx.IsValid() {
		return
	}

	gid := goroutineID()
	goroutineTraceContexts.Store(gid, spanCtx)
}

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

		if tw.config != nil && tw.config.TraceLogging {
			spanCtx := span.SpanContext()
			setTraceCtx(spanCtx)

			defer clearTraceCtx()
		}

		next(ctx)

		status := ctx.Response().StatusCode()
		if status > 0 {
			span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		}

		span.SetStatus(semconvutil.HTTPServerStatus(status))

		span.End()
	}
}
