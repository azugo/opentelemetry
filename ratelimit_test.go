package opentelemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	"azugo.io/azugo"
	"azugo.io/azugo/config"
	"azugo.io/azugo/middleware"
	"github.com/go-quicktest/qt"
	"github.com/valyala/fasthttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func rateLimitConfig() *config.RateLimit {
	return &config.RateLimit{
		Enabled:  true,
		Strategy: "fixed-window",
		Limit:    100,
		Window:   time.Minute,
	}
}

// TestRateLimitTracing verifies the rate limiter Allow check is recorded as a
// child span of the request span when the request is traced.
func TestRateLimitTracing(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)
	// Rate limit middleware runs after the tracing middleware so the limiter
	// span attaches to the active request span.
	app.Use(middleware.RateLimit(rateLimitConfig()))

	app.Start(t)
	defer app.Stop()

	app.Get("/work", func(ctx *azugo.Context) {
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/work")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(resp.StatusCode(), fasthttp.StatusNoContent))

	ended := sr.Ended()
	qt.Assert(t, qt.HasLen(ended, 2))

	var server, rl sdktrace.ReadOnlySpan

	for _, s := range ended {
		switch {
		case s.SpanKind() == trace.SpanKindServer:
			server = s
		case s.SpanKind() == trace.SpanKindInternal && strings.HasPrefix(s.Name(), "ALLOW "):
			rl = s
		}
	}

	qt.Assert(t, qt.IsNotNil(server))
	qt.Assert(t, qt.IsNotNil(rl))

	// The rate limiter span is a child of the request span in the same trace.
	qt.Check(t, qt.Equals(rl.SpanContext().TraceID(), server.SpanContext().TraceID()))
	qt.Check(t, qt.Equals(rl.Parent().SpanID(), server.SpanContext().SpanID()))

	// The limiter result is recorded on the span.
	allowed, ok := boolAttr(rl, "ratelimit.allowed")
	qt.Check(t, qt.IsTrue(ok))
	qt.Check(t, qt.IsTrue(allowed))
	qt.Check(t, qt.IsTrue(hasAttr(rl, "ratelimit.remaining")))
}

// TestRateLimitSkipInHandlerNoOrphan covers the healthz pattern: the request is
// rate limited (so the limiter produces a span before the handler runs) and the
// handler then calls SkipRequestLog. The deferred limiter span must be dropped
// together with the request span — no orphan ALLOW span is exported.
func TestRateLimitSkipInHandlerNoOrphan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)
	app.Use(middleware.RateLimit(rateLimitConfig()))

	app.Start(t)
	defer app.Stop()

	app.Get("/healthz", func(ctx *azugo.Context) {
		// Runs after the rate limiter already performed its Allow check.
		ctx.SkipRequestLog()
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/healthz")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	qt.Check(t, qt.HasLen(sr.Ended(), 0))
}

// boolAttr returns the value of a boolean span attribute.
func boolAttr(s sdktrace.ReadOnlySpan, key string) (bool, bool) {
	for _, a := range s.Attributes() {
		if string(a.Key) == key {
			return a.Value.AsBool(), true
		}
	}

	return false, false
}

func hasAttr(s sdktrace.ReadOnlySpan, key string) bool {
	for _, a := range s.Attributes() {
		if string(a.Key) == key {
			return true
		}
	}

	return false
}

// TestGlobalRateLimitTracing verifies the ordering used by server.New +
// opentelemetry.Use: the rate limiter is a regular middleware while tracing is a
// priority middleware, so tracing wraps the limiter and records its Allow check
// as a child span.
func TestGlobalRateLimitTracing(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	opts := []Option{TracerProvider(tp)}

	app := azugo.NewTestApp()
	app.Instrumentation(instr(opts...))
	// Simulate server.New: request logging is a priority middleware and the
	// global rate limiter is a regular middleware.
	app.UsePriority(logRequest)
	app.Use(middleware.RateLimit(rateLimitConfig()))
	// opentelemetry.Use registers tracing as a priority middleware (after
	// logRequest) so it wraps the regular rate limiter.
	app.UsePriority(tracingMiddleware(opts...))

	app.Start(t)
	defer app.Stop()

	app.Get("/work", func(ctx *azugo.Context) {
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/work")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	ended := sr.Ended()
	qt.Assert(t, qt.HasLen(ended, 2))

	var server, rl sdktrace.ReadOnlySpan

	for _, s := range ended {
		switch {
		case s.SpanKind() == trace.SpanKindServer:
			server = s
		case s.SpanKind() == trace.SpanKindInternal && strings.HasPrefix(s.Name(), "ALLOW "):
			rl = s
		}
	}

	qt.Assert(t, qt.IsNotNil(server))
	qt.Assert(t, qt.IsNotNil(rl))
	qt.Check(t, qt.Equals(rl.Parent().SpanID(), server.SpanContext().SpanID()))
}

// TestRateLimitTracingSkipped verifies that when a request is marked
// non-loggable before the rate limit middleware runs, the limiter still applies
// but emits no span (and neither does the request).
func TestRateLimitTracingSkipped(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)

	// Disable tracing before the rate limit middleware so the limiter sees a
	// non-recording request when it performs the Allow check.
	app.Use(func(next azugo.RequestHandler) azugo.RequestHandler {
		return func(ctx *azugo.Context) {
			ctx.SkipRequestLog()
			next(ctx)
		}
	})
	app.Use(middleware.RateLimit(rateLimitConfig()))

	app.Start(t)
	defer app.Stop()

	app.Get("/healthz", func(ctx *azugo.Context) {
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/healthz")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	qt.Check(t, qt.HasLen(sr.Ended(), 0))
}
