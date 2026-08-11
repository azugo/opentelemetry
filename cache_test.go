package opentelemetry

import (
	"context"
	"testing"

	"azugo.io/azugo"
	"azugo.io/core/cache"
	"azugo.io/core/http"
	"github.com/go-quicktest/qt"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestCacheMetrics verifies cache get and hit events are counted with the
// backend attribute.
func TestCacheMetrics(t *testing.T) {
	rdr := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	i := instr(TracerProvider(tp), MeterProvider(mp))

	ctx := context.Background()

	i(ctx, cache.InstrumentationGet, "test:key", string(cache.RedisCache), "test")(nil)
	i(ctx, cache.InstrumentationGetHit, "test:key", string(cache.RedisCache), "test")(nil)
	i(ctx, cache.InstrumentationGet, "test:key", string(cache.MemoryCache), "test")(nil)
	// Non-get events are not counted.
	i(ctx, cache.InstrumentationSet, "test:key", string(cache.RedisCache), "test")(nil)

	var rm metricdata.ResourceMetrics

	qt.Assert(t, qt.IsNil(rdr.Collect(ctx, &rm)))

	// Metric name -> backend/instance attributes -> value.
	sums := make(map[string]map[string]int64)

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}

			for _, dp := range sum.DataPoints {
				backend, _ := dp.Attributes.Value(attribute.Key(attrCacheBackend))
				instance, _ := dp.Attributes.Value(attribute.Key(attrCacheInstance))

				if sums[m.Name] == nil {
					sums[m.Name] = make(map[string]int64)
				}

				sums[m.Name][backend.AsString()+"/"+instance.AsString()] += dp.Value
			}
		}
	}

	qt.Check(t, qt.Equals(sums["azugo.cache.gets"]["redis/test"], int64(1)))
	qt.Check(t, qt.Equals(sums["azugo.cache.gets"]["memory/test"], int64(1)))
	qt.Check(t, qt.Equals(sums["azugo.cache.hits"]["redis/test"], int64(1)))
	qt.Check(t, qt.Equals(sums["azugo.cache.hits"]["memory/test"], int64(0)))
}

// TestCacheSpanAttributes verifies Redis cache operation spans carry backend
// and instance attributes.
func TestCacheSpanAttributes(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	i := instr(TracerProvider(tp))

	i(context.Background(), cache.InstrumentationSet, "spanattr:key", string(cache.RedisCache), "spanattr")(nil)

	ended := sr.Ended()
	qt.Assert(t, qt.HasLen(ended, 1))

	set := ended[0]
	qt.Check(t, qt.Equals(set.Name(), "SET spanattr:key"))

	backend, ok := strAttr(set, attrCacheBackend)
	qt.Check(t, qt.IsTrue(ok))
	qt.Check(t, qt.Equals(backend, string(cache.RedisCache)))

	instance, ok := strAttr(set, attrCacheInstance)
	qt.Check(t, qt.IsTrue(ok))
	qt.Check(t, qt.Equals(instance, "spanattr"))
}

// TestMemoryCacheSpanSkipped verifies memory cache operations emit no spans
// while the request itself is still traced.
func TestMemoryCacheSpanSkipped(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	app := skipApp(t, sr)

	app.Start(t)
	defer app.Stop()

	i, err := cache.Create[string](app.App.Cache(), "spanattr")
	qt.Assert(t, qt.IsNil(err))

	app.Get("/work", func(ctx *azugo.Context) {
		if err := i.Set(ctx, "key", "value"); err != nil {
			ctx.Error(err)

			return
		}

		if _, err := i.Get(ctx, "key"); err != nil {
			ctx.Error(err)

			return
		}

		ctx.StatusCode(http.StatusNoContent)
	})

	resp, err := app.TestClient().Get("/work")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(resp.StatusCode(), http.StatusNoContent))

	ended := sr.Ended()
	qt.Assert(t, qt.HasLen(ended, 1))
	qt.Check(t, qt.Equals(ended[0].SpanKind(), trace.SpanKindServer))
}

// strAttr returns the value of a string span attribute.
func strAttr(s sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, a := range s.Attributes() {
		if string(a.Key) == key {
			return a.Value.AsString(), true
		}
	}

	return "", false
}
