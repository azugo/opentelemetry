// Copyright 2024 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"strings"

	"azugo.io/opentelemetry/internal/semconvutil"

	"azugo.io/core/http"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func httpClientRecorder(ctx context.Context, tracer oteltrace.Tracer, propagator propagation.TextMapPropagator, spfmt InstrumentationSpanNameFormatter, op string, args ...any) (func(err error), bool) {
	c := FromContext(ctx)

	req, resp, ok := http.InstrRequest(op, args...)
	if !ok {
		return nil, false
	}

	opts := []oteltrace.SpanStartOption{
		oteltrace.WithAttributes(
			semconvutil.HTTPClientRequest(req)...,
		),
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
	}

	spanName := spfmt(ctx, op, args...)
	if spanName == "" {
		var s strings.Builder

		_, _ = s.Write(req.Header.Method())

		if baseURL := req.BaseURL(); baseURL != "" {
			_, _ = s.WriteRune(' ')
			_, _ = s.WriteString(baseURL)
		}

		spanName = s.String()
	}

	//nolint:spancheck
	c, span := tracer.Start(c, spanName, opts...)

	propagator.Inject(c, (*headerCarrier)(req))

	//nolint:spancheck
	return func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())

			span.RecordError(err, oteltrace.WithStackTrace(true))

			span.End()

			return
		}

		span.SetAttributes(semconvutil.HTTPClientResponse(resp)...)

		span.SetStatus(semconvutil.HTTPServerStatus(resp.StatusCode()))

		span.End()
	}, true
}
