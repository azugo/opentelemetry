// Copyright 2026 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"slices"

	"go.opentelemetry.io/otel/log"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.uber.org/zap/zapcore"
)

type logDriver struct {
	provider log.LoggerProvider
	logger   log.Logger
	opts     []log.LoggerOption
	attr     []log.KeyValue
	ctx      context.Context
	minLevel zapcore.Level
}

func (l *logDriver) clone() *logDriver {
	return &logDriver{
		provider: l.provider,
		opts:     l.opts,
		logger:   l.logger,
		attr:     slices.Clone(l.attr),
		ctx:      l.ctx,
		minLevel: l.minLevel,
	}
}

// Enabled decides whether a given logging level is enabled when logging a message.
func (l *logDriver) Enabled(level zapcore.Level) bool {
	if level < l.minLevel {
		return false
	}

	return l.logger.Enabled(l.ctx, log.EnabledParameters{Severity: convertLogLevel(level)})
}

// Check determines whether the supplied Entry should be logged.
// If the entry should be logged, the Core adds itself to the CheckedEntry and returns the result.
func (l *logDriver) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if l.Enabled(ent.Level) {
		return ce.AddCore(ent, l)
	}

	return ce
}

// With adds structured context to the Core.
func (l *logDriver) With(fields []zapcore.Field) zapcore.Core {
	cloned := l.clone()

	if len(fields) > 0 {
		ctx, attrbuf := convertLogField(fields)
		if ctx != nil {
			cloned.ctx = ctx
		}

		cloned.attr = append(cloned.attr, attrbuf...)
	}

	return cloned
}

// Write method encodes zap fields to OTel logs and emits them.
func (l *logDriver) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	r := log.Record{}
	r.SetTimestamp(ent.Time)
	r.SetBody(log.StringValue(ent.Message))
	r.SetSeverity(convertLogLevel(ent.Level))
	r.SetSeverityText(ent.Level.String())

	r.AddAttributes(l.attr...)

	if ent.Caller.Defined {
		r.AddAttributes(
			log.String(string(semconv.CodeFilePathKey), ent.Caller.File),
			log.Int(string(semconv.CodeLineNumberKey), ent.Caller.Line),
			log.String(string(semconv.CodeFunctionNameKey), ent.Caller.Function),
		)
	}

	if ent.Stack != "" {
		r.AddAttributes(log.String(string(semconv.CodeStacktraceKey), ent.Stack))
	}

	emitCtx := l.ctx

	if len(fields) > 0 {
		ctx, attrbuf := convertLogField(fields)
		if ctx != nil {
			emitCtx = FromContext(ctx)
		}

		r.AddAttributes(attrbuf...)
	}

	logger := l.logger

	if ent.LoggerName != "" {
		logger = l.provider.Logger(ent.LoggerName, l.opts...)
	}

	logger.Emit(emitCtx, r)

	return nil
}

// Sync flushes buffered logs (if any).
func (l *logDriver) Sync() error {
	return nil
}

func convertLogField(fields []zapcore.Field) (context.Context, []log.KeyValue) {
	var ctx context.Context

	enc := newLogObjectEncoder(len(fields))
	for _, field := range fields {
		if field.Key == otelContextFieldKey {
			if ctxFld, ok := field.Interface.(context.Context); ok {
				ctx = ctxFld //nolint:fatcontext
			}

			continue
		}

		field.AddTo(enc)
	}

	enc.calculate(enc.root)

	return ctx, enc.root.attrs
}

func convertLogLevel(level zapcore.Level) log.Severity {
	switch level {
	case zapcore.DebugLevel:
		return log.SeverityDebug
	case zapcore.InfoLevel:
		return log.SeverityInfo
	case zapcore.WarnLevel:
		return log.SeverityWarn
	case zapcore.ErrorLevel:
		return log.SeverityError
	case zapcore.DPanicLevel:
		return log.SeverityFatal1
	case zapcore.PanicLevel:
		return log.SeverityFatal2
	case zapcore.FatalLevel:
		return log.SeverityFatal3
	case zapcore.InvalidLevel:
		fallthrough
	default:
		return log.SeverityUndefined
	}
}

func newLogCore(ctx context.Context, provider log.LoggerProvider, appName string, minLevel zapcore.Level) zapcore.Core {
	opts := []log.LoggerOption{
		log.WithSchemaURL(semconv.SchemaURL),
		log.WithInstrumentationVersion(Version()),
	}

	return &logDriver{
		provider: provider,
		logger:   provider.Logger(appName, opts...),
		opts:     opts,
		ctx:      ctx,
		minLevel: minLevel,
	}
}
