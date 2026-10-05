// Copyright 2026 Azugo
// SPDX-License-Identifier: Apache-2.0

//nolint:testpackage
package semconvutil

import (
	"testing"

	"azugo.io/core/http"
	"github.com/go-quicktest/qt"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func TestHTTPRequestMethodAttr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method http.Method
		want   attribute.KeyValue
	}{
		{"", semconv.HTTPRequestMethodGet},
		{http.MethodGet, semconv.HTTPRequestMethodGet},
		{http.MethodDelete, semconv.HTTPRequestMethodDelete},
		{http.MethodQuery, semconv.HTTPRequestMethodQuery},
		{"get", semconv.HTTPRequestMethodOther},
		{"PROPFIND", semconv.HTTPRequestMethodOther},
	}

	for _, tt := range tests {
		qt.Check(t, qt.Equals(httpRequestMethodAttr(tt.method), tt.want), qt.Commentf("method %q", tt.method))
	}
}
