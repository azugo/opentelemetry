// Copyright 2024 Azugo
// SPDX-License-Identifier: Apache-2.0

// Package opentelemetry provides OpenTelemetry integration for Azugo applications.
package opentelemetry

import (
	"context"

	"azugo.io/core/cache"
	"azugo.io/core/instrumenter"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	attrCacheBackend  = "azugo.cache.backend"
	attrCacheInstance = "azugo.cache.instance"
)

// cacheAttrs returns backend and instance attributes from event args.
func cacheAttrs(args ...any) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)

	if backend, ok := cache.InstrBackend(args...); ok {
		attrs = append(attrs, attribute.String(attrCacheBackend, string(backend)))
	}

	if instance, ok := cache.InstrInstance(args...); ok {
		attrs = append(attrs, attribute.String(attrCacheInstance, instance))
	}

	return attrs
}

// countCache increments counter with the backend and instance attributes from
// event args.
func countCache(ctx context.Context, counter metric.Int64Counter, args ...any) {
	if counter == nil {
		return
	}

	opts := make([]metric.AddOption, 0, 1)
	if attrs := cacheAttrs(args...); len(attrs) > 0 {
		opts = append(opts, metric.WithAttributes(attrs...))
	}

	counter.Add(ctx, 1, opts...)
}

func newCacheRecorder(mp metric.MeterProvider) InstrumentationRecorderFunc {
	meter := mp.Meter(
		ScopeName+"/cache",
		metric.WithInstrumentationVersion(Version()),
	)

	gets, err := meter.Int64Counter("azugo.cache.gets",
		metric.WithDescription("Cache get operations."),
		metric.WithUnit("{operation}"))
	if err != nil {
		otel.Handle(err)
	}

	hits, err := meter.Int64Counter("azugo.cache.hits",
		metric.WithDescription("Cache get operations served from in-process memory."),
		metric.WithUnit("{operation}"))
	if err != nil {
		otel.Handle(err)
	}

	return func(ctx context.Context, tr trace.Tracer, _ propagation.TextMapPropagator, spfmt InstrumentationSpanNameFormatter, op string, args ...any) (func(err error), bool) {
		var (
			name   string
			method string
			ok     bool
		)

		switch op {
		case cache.InstrumentationGetHit:
			// Hits are aggregated as a counter; a span per local hit would be noise.
			countCache(ctx, hits, args...)

			return instrumenter.NullFinish, true
		case cache.InstrumentationGet:
			countCache(ctx, gets, args...)

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

		if backend, ok := cache.InstrBackend(args...); ok && backend == cache.MemoryCache {
			return instrumenter.NullFinish, true
		}

		if !Recording(ctx) {
			return nil, false
		}

		spanName := spfmt(ctx, op, args...)
		if spanName == "" {
			spanName = method + name
		}

		opts := []trace.SpanStartOption{
			trace.WithAttributes(append(cacheAttrs(args...), semconv.ServicePeerName("cache"))...),
			trace.WithSpanKind(trace.SpanKindInternal),
		}

		_, span := StartSpan(ctx, tr, spanName, opts...)

		return func(err error) {
			if err != nil {
				span.SetStatus(codes.Error, err.Error())

				span.RecordError(err, trace.WithStackTrace(true))
			}

			span.End()
		}, true
	}
}
