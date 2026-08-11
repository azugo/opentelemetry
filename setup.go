// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"azugo.io/azugo"
	"azugo.io/core/cache"
	"azugo.io/core/http"
	"azugo.io/core/ratelimit"
	"azugo.io/core/system"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	compsemconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.uber.org/zap"
	"google.golang.org/grpc/credentials"
)

type setup struct {
	app         *azugo.App
	config      *Configuration
	shutdownFns []func(context.Context) error
}

func (*setup) Name() string {
	return "Open Telemetry"
}

func (*setup) Start(context.Context) error {
	return nil
}

func (s *setup) Stop() {
	ctx := context.WithoutCancel(s.app.BackgroundContext())

	var err error

	for _, fn := range s.shutdownFns {
		err = errors.Join(err, fn(ctx))
	}

	s.shutdownFns = nil

	if err != nil {
		s.app.Log().Warn("Open Telemetry shutdown error", zap.Error(err))
	}
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func sysinfoAttrs() ([]attribute.KeyValue, string) {
	var instanceID string

	sysinfo := system.CollectInfo()

	attrs := make([]attribute.KeyValue, 0, 5)

	switch runtime.GOOS {
	case "linux":
		attrs = append(attrs, semconv.OSTypeLinux)
	case "darwin":
		attrs = append(attrs, semconv.OSTypeDarwin)
	case "windows":
		attrs = append(attrs, semconv.OSTypeWindows)
	case "freebsd":
		attrs = append(attrs, semconv.OSTypeFreeBSD)
	case "openbsd":
		attrs = append(attrs, semconv.OSTypeOpenBSD)
	case "netbsd":
		attrs = append(attrs, semconv.OSTypeNetBSD)
	case "dragonfly":
		attrs = append(attrs, semconv.OSTypeDragonflyBSD)
	case "solaris":
		attrs = append(attrs, semconv.OSTypeSolaris)
	case "aix":
		attrs = append(attrs, semconv.OSTypeAIX)
	case "zos":
		attrs = append(attrs, semconv.OSTypeZOS)
	}

	switch runtime.GOARCH {
	case "386":
		attrs = append(attrs, semconv.HostArchX86)
	case "amd64":
		attrs = append(attrs, semconv.HostArchAMD64)
	case "arm":
		attrs = append(attrs, semconv.HostArchARM32)
	case "arm64":
		attrs = append(attrs, semconv.HostArchARM64)
	case "ppc":
		attrs = append(attrs, semconv.HostArchPPC32)
	case "ppc64":
		attrs = append(attrs, semconv.HostArchPPC64)
	case "s390x":
		attrs = append(attrs, semconv.HostArchS390x)
	}

	if sysinfo.Hostname != "" {
		attrs = append(attrs, semconv.HostNameKey.String(sysinfo.Hostname))
		instanceID = sysinfo.Hostname
	}

	if sysinfo.IsContainer() {
		if sysinfo.IsKubernetes() {
			attrs = append(attrs,
				semconv.K8SPodUID(sysinfo.Container.Kubernetes.PodUID),
				semconv.K8SPodName(sysinfo.Container.Kubernetes.PodName),
			)

			if sysinfo.Container.Kubernetes.Namespace != "" {
				attrs = append(attrs, semconv.K8SNamespaceName(sysinfo.Container.Kubernetes.Namespace))
			}

			if sysinfo.Container.Kubernetes.NodeName != "" {
				attrs = append(attrs, semconv.K8SNodeName(sysinfo.Container.Kubernetes.NodeName))
			}

			instanceID = sysinfo.Container.Kubernetes.PodUID
		} else {
			attrs = append(attrs, semconv.ContainerIDKey.String(sysinfo.Container.ID))
			instanceID = sysinfo.Container.ID
		}
	}

	return attrs, instanceID
}

func customAttrs(resourceAttrs []string) []attribute.KeyValue {
	if len(resourceAttrs) == 0 {
		return nil
	}

	attrs := make([]attribute.KeyValue, 0, len(resourceAttrs))

	// Parse "key1=value1" format
	for _, attr := range resourceAttrs {
		key, value, ok := strings.Cut(attr, "=")
		if ok {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			attrs = append(attrs, attribute.String(key, value))
		}
	}

	return attrs
}

func newResource(app *azugo.App, config *Configuration) (*resource.Resource, error) {
	serviceName := config.ServiceName
	if serviceName == "" {
		serviceName = app.AppName
	}

	attrs := make([]attribute.KeyValue, 0, 4)

	attrs = append(attrs,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(app.AppVer),
		// For compatibility with older OTEL versions
		compsemconv.DeploymentEnvironment(strings.ToLower(string(app.Env()))),
	)

	switch env := app.Env(); {
	case env.IsProduction():
		attrs = append(attrs, semconv.DeploymentEnvironmentNameProduction)
	case env.IsStaging():
		attrs = append(attrs, semconv.DeploymentEnvironmentNameStaging)
	case env.IsDevelopment():
		attrs = append(attrs, semconv.DeploymentEnvironmentNameDevelopment)
	default:
		attrs = append(attrs, semconv.DeploymentEnvironmentNameTest)
	}

	// Add system information attributes.
	sysattrs, instanceID := sysinfoAttrs()

	if instanceID != "" {
		attrs = append(attrs, semconv.ServiceInstanceID(instanceID))
	}

	attrs = append(attrs, sysattrs...)

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		attrs...,
	)

	// Parse and add custom OTEL_RESOURCE_ATTRIBUTES
	custom := resource.NewWithAttributes(
		semconv.SchemaURL,
		customAttrs(config.ResourceAttributes)...,
	)

	res, err := resource.Merge(res, custom)
	if err != nil {
		return nil, fmt.Errorf("merging resources: %w", err)
	}

	return res, nil
}

// OTLP transport protocol values as defined by OTEL_EXPORTER_OTLP_PROTOCOL.
const (
	ProtocolGRPC         = "grpc"
	ProtocolHTTPProtobuf = "http/protobuf"
)

type otlpTarget struct {
	GRPC     bool
	Host     string
	Insecure bool
	TLS      *tls.Config
	Headers  map[string]string
}

func otlpTargetFromConfig(config *Configuration) (*otlpTarget, error) {
	t := &otlpTarget{}

	switch config.Protocol {
	case "", ProtocolHTTPProtobuf:
	case ProtocolGRPC:
		t.GRPC = true
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol: %s", config.Protocol)
	}

	if config.Endpoint != "" {
		u, err := url.Parse(config.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("parsing OTLP endpoint: %w", err)
		}

		switch u.Scheme {
		case "http":
			t.Host = u.Host
			t.Insecure = true
		case "https":
			t.Host = u.Host
			t.TLS = &tls.Config{
				//nolint:gosec
				InsecureSkipVerify: config.InsecureSkipVerify,
			}
		default:
			return nil, fmt.Errorf("invalid OTLP endpoint scheme: %s", u.Scheme)
		}
	}

	if config.ElasticAPMSecretToken != "" {
		t.Headers = map[string]string{
			http.HeaderAuthorization: "ApiKey " + config.ElasticAPMSecretToken,
		}
	}

	return t, nil
}

func newTraceProvider(app *azugo.App, config *Configuration) (*trace.TracerProvider, error) {
	target, err := otlpTargetFromConfig(config)
	if err != nil {
		return nil, err
	}

	var client otlptrace.Client

	if target.GRPC {
		opt := make([]otlptracegrpc.Option, 0, 3)

		if target.Host != "" {
			opt = append(opt, otlptracegrpc.WithEndpoint(target.Host))

			if target.Insecure {
				opt = append(opt, otlptracegrpc.WithInsecure())
			} else {
				opt = append(opt, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(target.TLS)))
			}
		}

		if target.Headers != nil {
			opt = append(opt, otlptracegrpc.WithHeaders(target.Headers))
		}

		client = otlptracegrpc.NewClient(opt...)
	} else {
		opt := make([]otlptracehttp.Option, 0, 3)

		if target.Host != "" {
			opt = append(opt, otlptracehttp.WithEndpoint(target.Host))

			if target.Insecure {
				opt = append(opt, otlptracehttp.WithInsecure())
			} else {
				opt = append(opt, otlptracehttp.WithTLSClientConfig(target.TLS))
			}
		}

		if target.Headers != nil {
			opt = append(opt, otlptracehttp.WithHeaders(target.Headers))
		}

		client = otlptracehttp.NewClient(opt...)
	}

	exporter, err := otlptrace.New(app.BackgroundContext(), client)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	res, err := newResource(app, config)
	if err != nil {
		return nil, err
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(
			exporter,
		),

		trace.WithResource(
			res,
		),
	)

	return traceProvider, nil
}

//nolint:dupl
func newLogProvider(app *azugo.App, config *Configuration) (*log.LoggerProvider, error) {
	target, err := otlpTargetFromConfig(config)
	if err != nil {
		return nil, err
	}

	var exporter log.Exporter

	if target.GRPC {
		opt := make([]otlploggrpc.Option, 0, 3)

		if target.Host != "" {
			opt = append(opt, otlploggrpc.WithEndpoint(target.Host))

			if target.Insecure {
				opt = append(opt, otlploggrpc.WithInsecure())
			} else {
				opt = append(opt, otlploggrpc.WithTLSCredentials(credentials.NewTLS(target.TLS)))
			}
		}

		if target.Headers != nil {
			opt = append(opt, otlploggrpc.WithHeaders(target.Headers))
		}

		exporter, err = otlploggrpc.New(app.BackgroundContext(), opt...)
	} else {
		opt := make([]otlploghttp.Option, 0, 3)

		if target.Host != "" {
			opt = append(opt, otlploghttp.WithEndpoint(target.Host))

			if target.Insecure {
				opt = append(opt, otlploghttp.WithInsecure())
			} else {
				opt = append(opt, otlploghttp.WithTLSClientConfig(target.TLS))
			}
		}

		if target.Headers != nil {
			opt = append(opt, otlploghttp.WithHeaders(target.Headers))
		}

		exporter, err = otlploghttp.New(app.BackgroundContext(), opt...)
	}

	if err != nil {
		return nil, fmt.Errorf("creating OTLP log exporter: %w", err)
	}

	res, err := newResource(app, config)
	if err != nil {
		return nil, err
	}

	return log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(exporter)),
		log.WithResource(res),
	), nil
}

//nolint:dupl
func newMeterProvider(app *azugo.App, config *Configuration) (*metric.MeterProvider, error) {
	target, err := otlpTargetFromConfig(config)
	if err != nil {
		return nil, err
	}

	var exporter metric.Exporter

	if target.GRPC {
		opt := make([]otlpmetricgrpc.Option, 0, 3)

		if target.Host != "" {
			opt = append(opt, otlpmetricgrpc.WithEndpoint(target.Host))

			if target.Insecure {
				opt = append(opt, otlpmetricgrpc.WithInsecure())
			} else {
				opt = append(opt, otlpmetricgrpc.WithTLSCredentials(credentials.NewTLS(target.TLS)))
			}
		}

		if target.Headers != nil {
			opt = append(opt, otlpmetricgrpc.WithHeaders(target.Headers))
		}

		exporter, err = otlpmetricgrpc.New(app.BackgroundContext(), opt...)
	} else {
		opt := make([]otlpmetrichttp.Option, 0, 3)

		if target.Host != "" {
			opt = append(opt, otlpmetrichttp.WithEndpoint(target.Host))

			if target.Insecure {
				opt = append(opt, otlpmetrichttp.WithInsecure())
			} else {
				opt = append(opt, otlpmetrichttp.WithTLSClientConfig(target.TLS))
			}
		}

		if target.Headers != nil {
			opt = append(opt, otlpmetrichttp.WithHeaders(target.Headers))
		}

		exporter, err = otlpmetrichttp.New(app.BackgroundContext(), opt...)
	}

	if err != nil {
		return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
	}

	res, err := newResource(app, config)
	if err != nil {
		return nil, err
	}

	return metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(exporter)),
		metric.WithResource(res),
	), nil
}

func traceConfig(opts ...Option) *otelcfg {
	cfg := otelcfg{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	if cfg.TracerProvider == nil {
		cfg.TracerProvider = otel.GetTracerProvider()
	}

	if cfg.MeterProvider == nil {
		cfg.MeterProvider = otel.GetMeterProvider()
	}

	if cfg.Propagators == nil {
		cfg.Propagators = otel.GetTextMapPropagator()
	}

	if cfg.routeSpanNameFormatter == nil {
		cfg.routeSpanNameFormatter = defaultRouteSpanNameFunc
	}

	if cfg.instrSpanNameFormatter == nil {
		cfg.instrSpanNameFormatter = defaultInstrSpanNameFormatter
	}

	cfg.instrRecorders = append(cfg.instrRecorders,
		instrRecorder{
			Name:     "http-client",
			Recorder: httpClientRecorder,
			Ops:      []string{http.InstrumentationRequest},
		},
		instrRecorder{
			Name:     "cache",
			Recorder: newCacheRecorder(cfg.MeterProvider),
			Ops: []string{
				cache.InstrumentationGet,
				cache.InstrumentationGetHit,
				cache.InstrumentationSet,
				cache.InstrumentationDelete,
			},
		},
		instrRecorder{
			Name:     "ratelimit",
			Recorder: ratelimitRecorder,
			Ops: []string{
				ratelimit.InstrumentationAllow,
				ratelimit.InstrumentationPeek,
				ratelimit.InstrumentationWait,
				ratelimit.InstrumentationReset,
			},
		},
		instrRecorder{
			Name:     "templ",
			Recorder: renderRecorder,
			Ops:      []string{templRenderOp},
		},
	)

	return &cfg
}
