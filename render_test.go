package opentelemetry

import (
	"testing"

	"azugo.io/azugo"
	"github.com/go-quicktest/qt"
	"github.com/valyala/fasthttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func emitRender(ctx *azugo.Context, name string) {
	var finish func(error)
	if name == "" {
		finish = ctx.App().Instrumenter().Observe(ctx, templRenderOp)
	} else {
		finish = ctx.App().Instrumenter().Observe(ctx, templRenderOp, name)
	}

	finish(nil)
}

// TestTemplRenderTracing verifies the built-in render recorder records the
// render as a child span of the request span, named with the component name.
func TestTemplRenderTracing(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)

	app.Start(t)
	defer app.Stop()

	app.Get("/page", func(ctx *azugo.Context) {
		emitRender(ctx, "home.page")
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/page")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	ended := sr.Ended()
	qt.Assert(t, qt.HasLen(ended, 2))

	var server, render sdktrace.ReadOnlySpan

	for _, s := range ended {
		switch s.SpanKind() {
		case trace.SpanKindServer:
			server = s
		case trace.SpanKindInternal:
			render = s
		}
	}

	qt.Assert(t, qt.IsNotNil(server))
	qt.Assert(t, qt.IsNotNil(render))

	// Named, and a child of the request span.
	qt.Check(t, qt.Equals(render.Name(), "render home.page"))
	qt.Check(t, qt.Equals(render.Parent().SpanID(), server.SpanContext().SpanID()))
}

// TestTemplRenderSkipInHandlerNoOrphan verifies deferral parity: a render that
// happens before the handler calls SkipRequestLog is dropped together with the
// request span — no orphan render span is exported.
func TestTemplRenderSkipInHandlerNoOrphan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)

	app.Start(t)
	defer app.Stop()

	app.Get("/healthz", func(ctx *azugo.Context) {
		emitRender(ctx, "home.page")
		ctx.SkipRequestLog()
		ctx.StatusCode(fasthttp.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/healthz")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))

	qt.Check(t, qt.HasLen(sr.Ended(), 0))
}
