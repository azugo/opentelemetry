package opentelemetry

import (
	"context"
	"sync"
	"time"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/trace"
)

const deferredUserValueKey = "__otelDeferredSpans"

// deferredSpan is a (sub)span whose End was deferred together with the captured
// operation end time.
type deferredSpan struct {
	span trace.Span
	end  time.Time
}

// deferredSpans collects instrumentation (sub)spans created during a request,
// so the request can decide whether to export them.
type deferredSpans struct {
	mu    sync.Mutex
	spans []deferredSpan
}

func (d *deferredSpans) add(span trace.Span, end time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.spans = append(d.spans, deferredSpan{span: span, end: end})
}

// finalize ends the collected spans when keep is true; when keep is false the
// spans are left unended and therefore never exported.
func (d *deferredSpans) finalize(keep bool) {
	d.mu.Lock()
	spans := d.spans
	d.spans = nil
	d.mu.Unlock()

	if !keep {
		return
	}

	for i := range spans {
		spans[i].span.End(trace.WithTimestamp(spans[i].end))
	}
}

func deferredFrom(ctx context.Context) *deferredSpans {
	rc := azugo.RequestContext(ctx)
	if rc == nil {
		return nil
	}

	ds, _ := rc.UserValue(deferredUserValueKey).(*deferredSpans)

	return ds
}

// noopSpan is a non-recording span returned to callers when the request is not
// being traced.
var noopSpan = trace.SpanFromContext(context.Background())

// Span wraps an OpenTelemetry span whose End is deferred to the end of the
// request. It is dropped together with the request if the request opts out of
// tracing.
//
// Create one with StartSpan.
type Span struct {
	trace.Span

	ctx context.Context
}

// End completes the Span. The Span is considered complete and ready to be
// delivered through the rest of the telemetry pipeline after this method is called.
// Therefore, updates to the Span are not allowed after this method has been called.
func (s Span) End(...trace.SpanEndOption) {
	if s.ctx != nil {
		if ds := deferredFrom(s.ctx); ds != nil {
			ds.add(s.Span, time.Now())

			return
		}
	}

	s.Span.End()
}

// StartSpan starts a span with tracer as a child of the active request span,
// and returns it wrapped so that End is deferred. Use it from custom
// instrumentation so the spans you create are dropped together with the request
// just like the built-in ones:
//
//	ctx, span := opentelemetry.StartSpan(ctx, otel.Tracer("myapp"), "work")
//	defer span.End()
func StartSpan(ctx context.Context, tracer trace.Tracer, name string, opts ...trace.SpanStartOption) (context.Context, Span) {
	if !Recording(ctx) {
		return ctx, Span{Span: noopSpan}
	}

	//nolint:spancheck
	c, span := tracer.Start(FromContext(ctx), name, opts...)

	//nolint:spancheck
	return c, Span{Span: span, ctx: ctx}
}
