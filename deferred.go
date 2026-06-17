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

// endSpan ends span, deferring the actual End to the end of the request when
// the request is being traced.
func endSpan(ctx context.Context, span trace.Span) {
	if ds := deferredFrom(ctx); ds != nil {
		ds.add(span, time.Now())

		return
	}

	span.End()
}
