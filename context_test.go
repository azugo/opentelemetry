// Copyright 2025 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"testing"

	"azugo.io/azugo"
	"github.com/go-quicktest/qt"
	"github.com/valyala/fasthttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// testTxContext mimics a database transaction context (as in jsondb) layered on
// top of the azugo request context: it implements azugo.Contexter and embeds a
// context.WithValue that delegates Value up the chain.
type testTxContext struct {
	context.Context

	parent context.Context
}

func (c *testTxContext) RequestContext() context.Context { return c.parent }

type testTxKeyType struct{}

func wrapTestTx(ctx context.Context) *testTxContext {
	t := &testTxContext{parent: ctx}
	t.Context = context.WithValue(ctx, testTxKeyType{}, t)

	return t
}

// logRequest marks the request as loggable so the tracing middleware traces it.
func logRequest(next azugo.RequestHandler) azugo.RequestHandler {
	return func(ctx *azugo.Context) {
		ctx.SetUserValue("__log_request", true)
		next(ctx)
	}
}

// TestMiddlewareSpanPropagation verifies that the tracing middleware installs
// the request span via Context.SetContext, so it is recoverable both directly
// and through a transaction-style wrapper passed to other libraries.
func TestMiddlewareSpanPropagation(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	app := azugo.NewTestApp()
	app.Use(logRequest)
	app.Use(middleware(TracerProvider(tp)))

	app.Start(t)
	defer app.Stop()

	var (
		handlerTraceID trace.TraceID
		spanValid      bool
		txSpanMatches  bool
		reqCtxMatches  bool
	)

	app.Get("/test", func(ctx *azugo.Context) {
		// The middleware installed the span on the request context.
		span := trace.SpanFromContext(ctx)
		spanValid = span.SpanContext().IsValid()
		handlerTraceID = span.SpanContext().TraceID()

		// Recover both the azugo context and the span through a transaction
		// wrapper, the way a database layer would when tracing a query.
		tx := wrapTestTx(ctx)
		reqCtxMatches = azugo.RequestContext(tx) == ctx

		got := FromContext(tx)
		gotSpan := trace.SpanFromContext(got)
		txSpanMatches = gotSpan.SpanContext().IsValid() &&
			gotSpan.SpanContext().TraceID() == handlerTraceID

		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/test")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(resp.StatusCode(), fasthttp.StatusNoContent))

	qt.Check(t, qt.IsTrue(spanValid))
	qt.Check(t, qt.IsTrue(reqCtxMatches))
	qt.Check(t, qt.IsTrue(txSpanMatches))

	// The server span was recorded and ended.
	ended := sr.Ended()
	qt.Assert(t, qt.Equals(len(ended), 1))
	qt.Check(t, qt.Equals(ended[0].SpanContext().TraceID(), handlerTraceID))
}

// TestFromContextWithoutSpan verifies FromContext is a no-op passthrough when
// the context carries no span.
func TestFromContextWithoutSpan(t *testing.T) {
	ctx := context.Background()
	qt.Check(t, qt.Equals(FromContext(ctx), ctx))
}

// TestConvertLogFieldSpanContext verifies the log driver reconstructs a
// span-correlated context from the span context smuggled in the otel log field.
func TestConvertLogFieldSpanContext(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc, 0xd, 0xe, 0xf, 0x10},
		SpanID:     trace.SpanID{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
		TraceFlags: trace.FlagsSampled,
	})
	qt.Assert(t, qt.IsTrue(sc.IsValid()))

	fields := []zapcore.Field{
		{Key: otelContextFieldKey, Type: zapcore.SkipType, Interface: sc},
		zap.String("key", "value"),
	}

	l := &logDriver{ctx: context.Background()}
	ctx, attrs := l.convertLogField(fields)

	qt.Assert(t, qt.IsNotNil(ctx))
	got := trace.SpanContextFromContext(ctx)
	qt.Check(t, qt.Equals(got.TraceID(), sc.TraceID()))
	qt.Check(t, qt.Equals(got.SpanID(), sc.SpanID()))
	// The non-sentinel field is still converted to a log attribute.
	qt.Check(t, qt.Equals(len(attrs), 1))
}
