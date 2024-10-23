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
	"azugo.io/core/system"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"
)

type setup struct {
	app         *azugo.App
	config      *Configuration
	shutdownFns []func(context.Context) error
}

func (setup) Name() string {
	return "Open Telemetry"
}

func (setup) Start(context.Context) error {
	return nil
}

func (s *setup) Stop() {
	ctx := s.app.BackgroundContext()

	var err error

	for _, fn := range s.shutdownFns {
		err = errors.Join(err, fn(ctx))
	}

	s.shutdownFns = nil

	s.app.Log().Warn("Open Telemetry shutdown error", zap.Error(err))
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

func newTraceProvider(app *azugo.App, config *Configuration) (*trace.TracerProvider, error) {
	opt := make([]otlptracehttp.Option, 0, 1)

	if config.Endpoint != "" {
		u, err := url.Parse(config.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("parsing OTLP endpoint: %w", err)
		}

		switch u.Scheme {
		case "http":
			opt = append(opt, otlptracehttp.WithEndpoint(u.Host), otlptracehttp.WithInsecure())
		case "https":
			opt = append(opt, otlptracehttp.WithEndpoint(u.Host))
		default:
			return nil, fmt.Errorf("invalid OTLP endpoint scheme: %s", u.Scheme)
		}
	}

	if config.ElasticAPMSecretToken != "" {
		opt = append(opt, otlptracehttp.WithHeaders(map[string]string{
			"Authorization": "ApiKey " + config.ElasticAPMSecretToken,
		}))
	}

	opt = append(opt, otlptracehttp.WithTLSClientConfig(&tls.Config{
		//nolint:gosec
		InsecureSkipVerify: config.InsecureSkipVerify,
	}))

	// TODO: support for GRPC
	exporter, err := otlptrace.New(app.BackgroundContext(), otlptracehttp.NewClient(opt...))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	attrs := make([]attribute.KeyValue, 0, 4)

	serviceName := config.ServiceName
	if serviceName == "" {
		serviceName = app.AppName
	}

	attrs = append(attrs,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(app.AppVer),
		semconv.DeploymentEnvironment(strings.ToLower(string(app.Env()))),
	)

	// Add system information attributes.
	sysattrs, instanceID := sysinfoAttrs()

	if instanceID != "" {
		attrs = append(attrs, semconv.ServiceInstanceID(instanceID))
	}

	attrs = append(attrs, sysattrs...)

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(
			exporter,
		),

		trace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			attrs...,
		)),
	)

	return traceProvider, nil
}

func traceConfig(opts ...Option) *otelcfg {
	cfg := otelcfg{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	if cfg.TracerProvider == nil {
		cfg.TracerProvider = otel.GetTracerProvider()
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
			Recorder: cacheRecorder,
			Ops:      []string{cache.InstrumentationGet, cache.InstrumentationSet, cache.InstrumentationDelete},
		},
	)

	return &cfg
}
