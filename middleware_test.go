// Copyright 2026 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"testing"

	"azugo.io/azugo"
	"azugo.io/core/http"
	"github.com/go-quicktest/qt"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// withHostHeader sends host in the Host header while keeping the request on
// the test client's keep-alive connection, which is picked by the URI host.
func withHostHeader(host string) azugo.TestClientOption {
	return func(_ *azugo.TestClient, r *fasthttp.Request) {
		r.UseHostHeader = true
		r.Header.SetHost(host)
	}
}

// TestRequestAttributesSurviveConnectionReuse verifies that server span
// attributes are not corrupted by later requests on the same keep-alive
// connection. Azugo exposes request values as strings over fasthttp buffers
// that are reused for every request on the connection, while the batch span
// processor reads the attributes only after the handler has returned.
func TestRequestAttributesSurviveConnectionReuse(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	app := azugo.NewTestApp()
	app.Use(logRequest)
	app.Use(tracingMiddleware(TracerProvider(tp)))

	app.Start(t)
	defer app.Stop()

	noContent := func(ctx *azugo.Context) {
		ctx.StatusCode(http.StatusNoContent)
	}

	app.Get("/items/{id}", noContent)
	app.Get("/items/{id}/details", noContent)
	app.Delete("/items/{id}", noContent)
	app.Get("/healthz", func(ctx *azugo.Context) {
		ctx.SkipRequestLog()
		noContent(ctx)
	})

	type request struct {
		method    http.Method
		path      string
		host      string
		userAgent string
	}

	traced := []request{
		{http.MethodGet, "/items/42", "api.example", "client/1"},
		{http.MethodGet, "/items/42/details", "api.example.com", "client/22"},
		{http.MethodDelete, "/items/42", "admin.example.com", "client/333"},
	}

	// A single client keeps one keep-alive connection, so every request is
	// parsed into the same server-side buffers.
	client := app.TestClient()

	send := func(r request) {
		t.Helper()

		resp, err := client.Call(r.method, r.path, nil,
			withHostHeader(r.host),
			client.WithHeader(http.HeaderUserAgent, r.userAgent),
		)
		qt.Assert(t, qt.IsNil(err))
		qt.Check(t, qt.Equals(resp.StatusCode(), http.StatusNoContent))
		fasthttp.ReleaseResponse(resp)
	}

	for _, r := range traced {
		send(r)
	}

	// An untraced request reuses the buffers once more before the export.
	send(request{http.MethodGet, "/healthz", "hc", "probe"})

	qt.Assert(t, qt.IsNil(tp.ForceFlush(context.Background())))

	spans := exp.GetSpans()
	qt.Assert(t, qt.HasLen(spans, len(traced)))

	for i, want := range traced {
		attrs := attribute.NewSet(spans[i].Attributes...)
		value := func(k attribute.Key) string {
			v, _ := attrs.Value(k)

			return v.AsString()
		}
		comment := qt.Commentf("%s %s", want.method, want.path)

		qt.Check(t, qt.Equals(value(semconv.HTTPRequestMethodKey), want.method.String()), comment)
		qt.Check(t, qt.Equals(value(semconv.URLPathKey), want.path), comment)
		qt.Check(t, qt.Equals(value(semconv.ServerAddressKey), want.host), comment)
		qt.Check(t, qt.Equals(value(semconv.UserAgentOriginalKey), want.userAgent), comment)
	}
}
