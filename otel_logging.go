// Copyright 2024 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdkLog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// otelLogging is a zap core logger that integrates OpenTelemetry logging with the original zap core.
type otelLogging struct {
	zapcore.Core
	logger log.Logger
}

func (oc *otelLogging) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// Write to original core (console/file)
	if err := oc.Core.Write(entry, fields); err != nil {
		return err
	}

	oc.sendOTel(entry, fields)

	return nil
}

func (oc *otelLogging) sendOTel(entry zapcore.Entry, fields []zapcore.Field) {
	ctx := context.Background()

	if spanCtx := getCurrentSpanContext(); spanCtx.IsValid() {
		ctx = trace.ContextWithSpanContext(ctx, spanCtx)
	}

	logData := map[string]any{
		"message": entry.Message,
		"level":   entry.Level.String(),
		"logger":  entry.LoggerName,
	}

	for _, field := range fields {
		switch field.Type {
		case zapcore.StringType:
			logData[field.Key] = field.String
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type,
			zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type,
			zapcore.UintptrType:
			logData[field.Key] = field.Integer
		case zapcore.BoolType:
			logData[field.Key] = field.Integer == 1
		case zapcore.Float64Type:
			value := math.Float64frombits(uint64(field.Integer))
			logData[field.Key] = value
		case zapcore.Float32Type:
			if field.Integer < 0 || field.Integer > math.MaxUint32 {
				// Should not happen with zapcore.Float32Type
				continue
			}

			value := math.Float32frombits(uint32(field.Integer))
			logData[field.Key] = value
		case zapcore.DurationType:
			value := time.Duration(field.Integer).String()
			logData[field.Key] = value
		case zapcore.TimeType:
			t := time.Unix(0, field.Integer)

			if field.Interface != nil {
				if loc, ok := field.Interface.(*time.Location); ok {
					t = t.In(loc)
				}
			}

			logData[field.Key] = t
		case zapcore.TimeFullType:
			if field.Interface != nil {
				logData[field.Key] = field.Interface
			}
		case zapcore.ErrorType:
			if field.Interface != nil {
				if err, ok := field.Interface.(error); ok {
					errorData := map[string]interface{}{
						"message": err.Error(),
						"type":    fmt.Sprintf("%T", err),
					}

					if stackTracer, ok := err.(interface{ StackTrace() interface{} }); ok {
						errorData["stack_trace"] = fmt.Sprintf("%+v", stackTracer.StackTrace())
					}

					logData[field.Key] = errorData
				} else {
					logData[field.Key] = fmt.Sprintf("%v", field.Interface)
				}
			}
		case zapcore.ReflectType, zapcore.ArrayMarshalerType, zapcore.ObjectMarshalerType, zapcore.InlineMarshalerType:
			if field.Interface != nil {
				logData[field.Key] = field.Interface
			}
		case zapcore.BinaryType:
			if field.Interface != nil {
				logData[field.Key] = fmt.Sprintf("%x", field.Interface)
			}
		case zapcore.ByteStringType:
			if field.Interface != nil {
				if val, ok := field.Interface.([]byte); ok {
					logData[field.Key] = string(val)
				}
			}
		case zapcore.Complex64Type, zapcore.Complex128Type:
			if field.Interface != nil {
				logData[field.Key] = fmt.Sprintf("%v", field.Interface)
			}
		case zapcore.StringerType:
			if field.Interface != nil {
				if stringer, ok := field.Interface.(fmt.Stringer); ok {
					logData[field.Key] = stringer.String()
				}
			}
		case zapcore.UnknownType:
			if field.String != "" {
				logData[field.Key] = field.String
			}
		case zapcore.NamespaceType, zapcore.SkipType:
			// Do nothing for these types
		default:
			if field.String != "" {
				logData[field.Key] = field.String
			}
		}
	}

	bodyBytes, err := json.Marshal(logData)
	if err != nil {
		return
	}

	var record log.Record

	record.SetTimestamp(entry.Time)
	record.SetSeverity(otelSeverity(entry.Level))
	record.SetSeverityText(entry.Level.String())
	record.SetBody(log.StringValue(string(bodyBytes)))

	oc.logger.Emit(ctx, record)
}

func otelSeverity(level zapcore.Level) log.Severity {
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
		return log.SeverityError
	case zapcore.PanicLevel:
		return log.SeverityFatal
	case zapcore.FatalLevel:
		return log.SeverityFatal
	case zapcore.InvalidLevel:
		return log.SeverityInfo
	default:
		return log.SeverityInfo
	}
}

func (oc *otelLogging) With(fields []zapcore.Field) zapcore.Core {
	return &otelLogging{
		Core:   oc.Core.With(fields),
		logger: oc.logger,
	}
}

func (oc *otelLogging) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if oc.Enabled(entry.Level) {
		return ce.AddCore(entry, oc)
	}

	return ce
}

// setupOTelLogging configures the OpenTelemetry logging for the Azugo application.
func setupOTelLogging(app *azugo.App) {
	logger := global.GetLoggerProvider().Logger("azugo")

	currentLogger := app.Log()

	otelLogger := currentLogger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		ol := &otelLogging{
			Core:   &TraceCore{Core: core},
			logger: logger,
		}

		return ol
	}))

	if err := app.ReplaceLogger(otelLogger); err != nil {
		app.Log().Error("Failed to replace logger with OpenTelemetry logger", zap.Error(err))
	}
}

// TraceCore is a zap core that adds OpenTelemetry trace context to log entries.
type TraceCore struct {
	zapcore.Core
}

func (tc *TraceCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	spanCtx := getCurrentSpanContext()

	if spanCtx.IsValid() {
		fields = append(fields,
			zapcore.Field{Key: "trace.id", Type: zapcore.StringType, String: spanCtx.TraceID().String()},
			zapcore.Field{Key: "span.id", Type: zapcore.StringType, String: spanCtx.SpanID().String()},
		)
	}

	return tc.Core.Write(entry, fields)
}

func (tc *TraceCore) With(fields []zapcore.Field) zapcore.Core {
	return &TraceCore{
		Core: tc.Core.With(fields),
	}
}

func (tc *TraceCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if tc.Enabled(entry.Level) {
		return ce.AddCore(entry, tc)
	}

	return ce
}

// newLogProvider creates a new OpenTelemetry log provider.
func newLogProvider(app *azugo.App, config *Configuration) (*sdkLog.LoggerProvider, error) {
	opt := make([]otlploghttp.Option, 0, 1)

	if config.Endpoint != "" {
		u, err := url.Parse(config.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("parsing OTLP endpoint: %w", err)
		}

		switch u.Scheme {
		case "http":
			opt = append(opt, otlploghttp.WithEndpoint(u.Host), otlploghttp.WithInsecure())
		case "https":
			opt = append(opt, otlploghttp.WithEndpoint(u.Host))
		default:
			return nil, fmt.Errorf("invalid OTLP endpoint scheme: %s", u.Scheme)
		}
	}

	if config.ElasticAPMSecretToken != "" {
		opt = append(opt, otlploghttp.WithHeaders(map[string]string{
			"Authorization": "ApiKey " + config.ElasticAPMSecretToken,
		}))
	}

	opt = append(opt, otlploghttp.WithTLSClientConfig(&tls.Config{
		//nolint:gosec // skip `G402: TLS InsecureSkipVerify may be true`, because this is based on config and developer should be aware of the implications.
		InsecureSkipVerify: config.InsecureSkipVerify,
	}))

	exporter, err := otlploghttp.New(app.BackgroundContext(), opt...)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP log exporter: %w", err)
	}

	attrs := make([]attribute.KeyValue, 0, 4)

	serviceName := config.ServiceName
	if serviceName == "" {
		serviceName = app.AppName
	}

	attrs = append(attrs,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(app.AppVer),
		semconv.DeploymentEnvironmentName(strings.ToLower(string(app.Env()))),
	)

	sysattrs, instanceID := sysinfoAttrs()
	if instanceID != "" {
		attrs = append(attrs, semconv.ServiceInstanceID(instanceID))
	}

	attrs = append(attrs, sysattrs...)

	logProvider := sdkLog.NewLoggerProvider(
		sdkLog.WithProcessor(sdkLog.NewBatchProcessor(exporter)),
		sdkLog.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			attrs...,
		)),
	)

	return logProvider, nil
}
