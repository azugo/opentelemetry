// Copyright 2024 Azugo
// SPDX-License-Identifier: Apache-2.0
package opentelemetry

import (
	"context"

	"azugo.io/core/cache"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

func cacheRecorder(ctx context.Context, tr trace.Tracer, _ propagation.TextMapPropagator, spfmt InstrumentationSpanNameFormatter, op string, args ...interface{}) (func(err error), bool) {
	var (
		name   string
		method string
		ok     bool
	)

	switch op {
	case cache.InstrumentationGet:
		name, ok = cache.InstrGet(op, args...)
		if !ok {
			return nil, false
		}

		method = "GET "
	case cache.InstrumentationSet:
		name, ok = cache.InstrSet(op, args...)
		if !ok {
			return nil, false
		}

		method = "SET "
	case cache.InstrumentationDelete:
		name, ok = cache.InstrDelete(op, args...)
		if !ok {
			return nil, false
		}

		method = "DELETE "
	default:
		return nil, false
	}

	c := FromContext(ctx)

	spanName := spfmt(ctx, op, args...)
	if spanName == "" {
		spanName = method + name
	}

	opts := []trace.SpanStartOption{
		trace.WithAttributes(
			semconv.PeerService("cache"),
		),
		trace.WithSpanKind(trace.SpanKindInternal),
	}

	//nolint:spancheck
	_, span := tr.Start(c, spanName, opts...)

	//nolint:spancheck
	return func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())

			span.RecordError(err, trace.WithStackTrace(true))
		}

		span.End()
	}, true
}
