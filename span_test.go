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

func startSpanApp(t *testing.T, sr *tracetest.SpanRecorder) (*azugo.TestApp, trace.Tracer) {
	t.Helper()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	opts := []Option{TracerProvider(tp)}

	app := azugo.NewTestApp()
	app.Instrumentation(instr(opts...))
	app.Use(logRequest)
	app.Use(tracingMiddleware(opts...))

	return app, tp.Tracer("test")
}

// TestStartSpanChild verifies a span created with StartSpan is recorded as a
// child of the request span.
func TestStartSpanChild(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app, tr := startSpanApp(t, sr)

	app.Start(t)
	defer app.Stop()

	app.Get("/work", func(ctx *azugo.Context) {
		_, span := StartSpan(ctx, tr, "custom")
		span.End()

		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/work")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	ended := sr.Ended()
	qt.Assert(t, qt.HasLen(ended, 2))

	var server, custom sdktrace.ReadOnlySpan

	for _, s := range ended {
		if s.Name() == "custom" {
			custom = s
		} else {
			server = s
		}
	}

	qt.Assert(t, qt.IsNotNil(server))
	qt.Assert(t, qt.IsNotNil(custom))
	qt.Check(t, qt.Equals(custom.Parent().SpanID(), server.SpanContext().SpanID()))
}

// TestStartSpanSkipDrops verifies a StartSpan span is dropped together with the
// request when the handler opts out of tracing after creating it.
func TestStartSpanSkipDrops(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app, tr := startSpanApp(t, sr)

	app.Start(t)
	defer app.Stop()

	app.Get("/healthz", func(ctx *azugo.Context) {
		_, span := StartSpan(ctx, tr, "custom")
		span.End()

		ctx.SkipRequestLog()
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/healthz")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	qt.Check(t, qt.HasLen(sr.Ended(), 0))
}
