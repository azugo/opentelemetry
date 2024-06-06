// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// otelcfg is used to configure the mux middleware.
type otelcfg struct {
	TracerProvider         oteltrace.TracerProvider
	Propagators            propagation.TextMapPropagator
	routeSpanNameFormatter RouteSpanNameFormatter
	instrSpanNameFormatter InstrumentationSpanNameFormatter
	instrRecorders         []instrRecorder
	PublicEndpoint         bool
	PublicEndpointFn       PublicEndpointFilter
	Filters                []Filter
}

// Option specifies instrumentation configuration options.
type Option interface {
	apply(c *otelcfg)
}

type optionFunc func(*otelcfg)

func (o optionFunc) apply(c *otelcfg) {
	o(c)
}

// Filter is a predicate used to determine whether a given HTTP request should
// be traced. A Filter must return true if the request should be traced.
//
// Multiple filters can be proviaded and are applied in the order they are added.
// If no filters are provided then all requests are traced.
// Filters will be invoked for each processed request, it is advised to make them
// simple and fast.
type Filter func(ctx *azugo.Context) bool

func (f Filter) apply(c *otelcfg) {
	c.Filters = append(c.Filters, f)
}

// PublicEndpoint configures the Handler to link the span with an incoming
// span context. If this option is not provided, then the association is a child
// association instead of a link.
type PublicEndpoint bool

func (p PublicEndpoint) apply(c *otelcfg) {
	c.PublicEndpoint = bool(p)
}

// PublicEndpointFilter runs with every request, and allows conditionnally
// configuring the Handler to link the span with an incoming span context. If
// this option is not provided or returns false, then the association is a
// child association instead of a link.
// Note: PublicEndpoint takes precedence over PublicEndpointFilter.
type PublicEndpointFilter func(ctx *azugo.Context) bool

func (f PublicEndpointFilter) apply(c *otelcfg) {
	c.PublicEndpointFn = f
}

// TextMapPropagator specifies propagators to use for extracting
// information from the HTTP requests. If none are specified, global
// ones will be used.
func TextMapPropagator(propagators propagation.TextMapPropagator) Option {
	return optionFunc(func(cfg *otelcfg) {
		if propagators != nil {
			cfg.Propagators = propagators
		}
	})
}

// TracerProvider specifies a tracer provider to use for creating a tracer.
// If none is specified, the global provider is used.
func TracerProvider(provider oteltrace.TracerProvider) Option {
	return optionFunc(func(cfg *otelcfg) {
		if provider != nil {
			cfg.TracerProvider = provider
		}
	})
}

// RouteSpanNameFormatter specifies a function to use for generating a custom span
// name. By default, the route name (path template or regexp) is used. The route
// name is provided so you can use it in the span name without needing to
// duplicate the logic for extracting it from the request.
type RouteSpanNameFormatter func(ctx *azugo.Context, routeName string) string

func (f RouteSpanNameFormatter) apply(c *otelcfg) {
	c.routeSpanNameFormatter = f
}

// InstrumentationSpanNameFormatter specifies a function to use for generating a custom span
// name. By default, the span name is formatted based on the operation type and the arguments.
// If the provided function returns an empty string, the default span name will be used.
type InstrumentationSpanNameFormatter func(ctx context.Context, op string, args ...interface{}) string

func (f InstrumentationSpanNameFormatter) apply(c *otelcfg) {
	c.instrSpanNameFormatter = f
}

// InstrumentationRecorderFunc specifies a function to use for handling instrumentation events.
// The function should return a function that can be used to finish the span and a boolean
// indicating if specific instrumentation has been recorded.
type InstrumentationRecorderFunc func(ctx context.Context, tracer oteltrace.Tracer, propagator propagation.TextMapPropagator, spfmt InstrumentationSpanNameFormatter, op string, args ...any) (func(err error), bool)

type instrRecorder struct {
	Name     string
	Recorder InstrumentationRecorderFunc
	Ops      []string
}

func (f instrRecorder) apply(c *otelcfg) {
	c.instrRecorders = append(c.instrRecorders, f)
}

// InstrumentationRecorder specifies a function to use for handling instrumentation events for specific
// instrumentation operations.
// Multiple recorders can be provided and are applied in the order they are added.
func InstrumentationRecorder(name string, recorder InstrumentationRecorderFunc, ops ...string) Option {
	return instrRecorder{
		Name:     name,
		Recorder: recorder,
		Ops:      ops,
	}
}
