// Copyright 2024 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"fmt"

	"azugo.io/opentelemetry/internal/semconvutil"

	"azugo.io/azugo"
	"azugo.io/core/instrumenter"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

func defaultInstrSpanNameFormatter(_ context.Context, _ string, _ ...any) string {
	return ""
}

type namedRecorder struct {
	Name     string
	Recorder InstrumentationRecorderFunc
}

func instr(opts ...Option) instrumenter.Instrumenter {
	cfg := traceConfig(opts...)

	tracers := make(map[string]trace.Tracer, len(cfg.instrRecorders))
	recorders := make(map[string][]namedRecorder, len(cfg.instrRecorders))

	for _, r := range cfg.instrRecorders {
		if _, ok := tracers[r.Name]; !ok {
			tracers[r.Name] = cfg.TracerProvider.Tracer(
				ScopeName+"/"+r.Name,
				trace.WithInstrumentationVersion(Version()),
				trace.WithInstrumentationAttributes(semconv.TelemetrySDKLanguageGo),
			)
		}

		for _, op := range r.Ops {
			recorders[op] = append(recorders[op], namedRecorder{
				Name:     r.Name,
				Recorder: r.Recorder,
			})
		}

		if len(r.Ops) == 0 {
			recorders[""] = append(recorders[""], namedRecorder{
				Name:     r.Name,
				Recorder: r.Recorder,
			})
		}
	}

	return func(ctx context.Context, op string, args ...interface{}) func(err error) {
		// Special handling for panic handler
		if op == azugo.InstrumentationPanic {
			return func(err error) {
				c := FromContext(ctx)

				span := trace.SpanFromContext(c)

				if span.SpanContext().IsValid() && span.IsRecording() {
					span.SetStatus(codes.Error, fmt.Sprintf("%v", err))

					if err != nil {
						span.RecordError(err, trace.WithStackTrace(true))
					}
				}

				actx, ok := ctx.(*azugo.Context)
				if ok {
					status := actx.Response().StatusCode()
					if status > 0 {
						span.SetAttributes(semconv.HTTPResponseStatusCode(status))
					}

					span.SetStatus(semconvutil.HTTPServerStatus(status))
				}

				span.End()
			}
		}

		for _, r := range recorders[op] {
			f, handled := r.Recorder(ctx, tracers[r.Name], cfg.Propagators, cfg.instrSpanNameFormatter, op, args...)
			if handled {
				return f
			}
		}

		for _, r := range recorders[""] {
			f, handled := r.Recorder(ctx, tracers[r.Name], cfg.Propagators, cfg.instrSpanNameFormatter, op, args...)
			if handled {
				return f
			}
		}

		return func(_ error) {}
	}
}
