// Copyright 2026 Azugo
// SPDX-License-Identifier: Apache-2.0

//nolint:testpackage
package semconvutil

import (
	"testing"
	"unsafe"

	"azugo.io/core/http"
	"github.com/go-quicktest/qt"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func TestHTTPRequestMethodAttr(t *testing.T) {
	t.Parallel()

	qt.Check(t, qt.Equals(httpRequestMethodAttr(""), semconv.HTTPRequestMethodGet))

	for _, method := range []string{"GET", "DELETE", "QUERY", "PROPFIND"} {
		// Server request methods alias fasthttp buffers that are reused by the
		// next request, so the attribute must not keep a reference to them.
		buf := []byte(method)
		attr := httpRequestMethodAttr(http.Method(unsafe.String(&buf[0], len(buf))))

		clear(buf)

		qt.Check(t, qt.Equals(attr, semconv.HTTPRequestMethodKey.String(method)), qt.Commentf("method %q", method))
	}
}
