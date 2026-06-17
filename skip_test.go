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
)

const unreachableUpstream = "http://127.0.0.1:1"

// skipApp builds a test app wired with the tracing middleware and HTTP client
// instrumentation backed by sr. logRequest runs before the tracing middleware,
// marking the request loggable the way the RequestLogger middleware would.
func skipApp(t *testing.T, sr *tracetest.SpanRecorder, opts ...Option) *azugo.TestApp {
	t.Helper()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	opts = append([]Option{TracerProvider(tp)}, opts...)

	app := azugo.NewTestApp()
	app.Instrumentation(instr(opts...))
	app.Use(logRequest)
	app.Use(tracingMiddleware(opts...))

	return app
}

// TestSkipRequestLogInHandlerDropsTrace reproduces the healthz pattern: the
// span is already started by the middleware when the handler calls
// SkipRequestLog. Neither the request span nor the subtrace created afterwards
// must be exported.
func TestSkipRequestLogInHandlerDropsTrace(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)

	app.Start(t)
	defer app.Stop()

	app.Get("/healthz", func(ctx *azugo.Context) {
		ctx.SkipRequestLog()

		// A subtrace that would otherwise be recorded as a child span.
		_, _ = ctx.HTTPClient().Get(unreachableUpstream)

		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/healthz")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(resp.StatusCode(), fasthttp.StatusNoContent))

	qt.Check(t, qt.HasLen(sr.Ended(), 0))
}

// TestSkipRequestLogBeforeMiddlewareDropsTrace covers the early path where
// tracing is disabled before the tracing middleware runs, so the span is never
// started.
func TestSkipRequestLogBeforeMiddlewareDropsTrace(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)

	app.Use(func(next azugo.RequestHandler) azugo.RequestHandler {
		return func(ctx *azugo.Context) {
			ctx.SkipRequestLog()
			next(ctx)
		}
	})

	app.Start(t)
	defer app.Stop()

	app.Get("/skip", func(ctx *azugo.Context) {
		_, _ = ctx.HTTPClient().Get(unreachableUpstream)
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/skip")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	qt.Check(t, qt.HasLen(sr.Ended(), 0))
}

// TestFilterDropsTraceAndSubtraces verifies that a Filter rejecting the request
// suppresses both the request span and any subtraces.
func TestFilterDropsTraceAndSubtraces(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr, Filter(func(*azugo.Context) bool { return false }))

	app.Start(t)
	defer app.Stop()

	app.Get("/filtered", func(ctx *azugo.Context) {
		_, _ = ctx.HTTPClient().Get(unreachableUpstream)
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/filtered")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	qt.Check(t, qt.HasLen(sr.Ended(), 0))
}

// TestRecording exercises the exported Recording helper directly.
func TestRecording(t *testing.T) {
	// Outside of a request the caller owns the span hierarchy.
	qt.Check(t, qt.IsTrue(Recording(context.Background())))

	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)

	app.Start(t)
	defer app.Stop()

	var traced, skipped bool

	app.Get("/r", func(ctx *azugo.Context) {
		// Span started by the middleware and request not skipped.
		traced = Recording(ctx)

		ctx.SkipRequestLog()
		// Disabled after the fact via SkipRequestLog.
		skipped = Recording(ctx)

		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/r")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	qt.Check(t, qt.IsTrue(traced))
	qt.Check(t, qt.IsFalse(skipped))
}

// TestTracedRequestRecordsServerAndSubtrace is the positive control: a normal
// traced request records the server span and the HTTP client subtrace as its
// child within the same trace.
func TestTracedRequestRecordsServerAndSubtrace(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)

	app.Start(t)
	defer app.Stop()

	app.Get("/work", func(ctx *azugo.Context) {
		_, _ = ctx.HTTPClient().Get(unreachableUpstream)
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/work")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	ended := sr.Ended()
	qt.Assert(t, qt.HasLen(ended, 2))

	var server, client sdktrace.ReadOnlySpan

	for _, s := range ended {
		switch s.SpanKind() {
		case trace.SpanKindServer:
			server = s
		case trace.SpanKindClient:
			client = s
		}
	}

	qt.Assert(t, qt.IsNotNil(server))
	qt.Assert(t, qt.IsNotNil(client))

	// The client subtrace is a child of the request span in the same trace.
	qt.Check(t, qt.Equals(client.SpanContext().TraceID(), server.SpanContext().TraceID()))
	qt.Check(t, qt.Equals(client.Parent().SpanID(), server.SpanContext().SpanID()))
}
