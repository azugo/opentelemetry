// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

// Package semconvutil provides utilities for OpenTelemetry semantic conventions.
package semconvutil

import (
	"bytes"
	"strconv"
	"strings"

	"azugo.io/core/http"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

var (
	redactedCredentials = []byte("REDACTED")
	redactedHeaderValue = "****"
)

// HTTPClientRequest returns attributes for an HTTP request sent by client.
//
// The following attributes are always returned: "http.request.method", "url.scheme",
// "url.full", "server.address", "network.protocol.name", "network.protocol.version",
// "network.transport". The following attributes are returned if they
// related values are defined in req: "server.port", "user_agent.original".
func HTTPClientRequest(req *http.Request) []attribute.KeyValue {
	/*
		The following semantic conventions are returned if present:
		http.request.method        string
		url.scheme                 string
		url.full                   string Note: doesn't include the query parameters.
		server.address             string
		server.port                int
		user_agent.original        string
		network.protocol.name      string Note: always set as "http".
		network.protocol.version   string Note: always set as "1.1".
		network.transport          string Note: always set as "tcp".

		The following semantic conventions are not returned:
		http.response.status_code             This requires the response.
		http.request.header.content-length    This requires the len() of body, which can mutate it.
		http.response.header.content-length   This requires the response.
		client.address                        The request doesn't have access to the underlying socket.
		client.port                           The request doesn't have access to the underlying socket.
	*/
	n := 7 // Method, scheme, host name, full URL and network protocol data.

	uri := fasthttp.AcquireURI()
	defer fasthttp.ReleaseURI(uri)

	req.URI().CopyTo(uri)

	// Redact the username and password from the URI.
	if len(uri.Username()) != 0 {
		uri.SetUsernameBytes(redactedCredentials)
	}

	if len(uri.Password()) != 0 {
		uri.SetPasswordBytes(redactedCredentials)
	}

	uri.SetHashBytes(nil)
	uri.QueryArgs().Reset()

	host, p := splitHostPort(string(uri.Host()))

	isTLS := bytes.Equal(uri.Scheme(), []byte("https"))

	hostPort := requiredHTTPPort(isTLS, p)
	if hostPort > 0 {
		n++
	}

	useragent := string(req.Header.UserAgent())
	if useragent != "" {
		n++
	}

	contentLen := req.Header.ContentLength()
	if contentLen > 0 {
		n++
	}

	attrs := make([]attribute.KeyValue, 0, n+req.Header.Len())

	attrs = append(attrs, httpRequestMethodAttr(string(req.Header.Method())))
	attrs = append(attrs, httpSchemeAttr(isTLS))
	attrs = append(attrs, semconv.ServerAddress(host))
	attrs = append(attrs, semconv.URLFull(uri.String()))
	// HTTP client supports only HTTP/1.1 over TCP.
	attrs = append(attrs, semconv.NetworkTransportTCP)
	attrs = append(attrs, semconv.NetworkProtocolName("http"))
	attrs = append(attrs, semconv.NetworkProtocolVersion("1.1"))

	if hostPort > 0 {
		attrs = append(attrs, semconv.ServerPort(hostPort))
	}

	if useragent != "" {
		attrs = append(attrs, semconv.UserAgentOriginal(useragent))
	}

	if contentLen > 0 {
		attrs = append(attrs, semconv.HTTPRequestHeader("content-length", strconv.Itoa(contentLen)))
	}

	for k, v := range req.Header.All() {
		key := strings.ToLower(string(k))
		// Skip user agent and content length as they are already handled.
		if key == "user-agent" || key == "content-length" {
			continue
		}

		val := string(v)
		if _, ok := redactedHeaders[key]; ok {
			val = redactedHeaderValue
		}

		attrs = append(attrs, semconv.HTTPRequestHeader(key, val))
	}

	return attrs
}

// HTTPClientResponse returns attributes for an HTTP response received by client.
//
// The following attributes are always returned: "http.response.status_code".
func HTTPClientResponse(resp *http.Response) []attribute.KeyValue {
	n := 1 // Response status code.

	contentLen := resp.Header.ContentLength()
	if contentLen > 0 {
		n++
	}

	attrs := make([]attribute.KeyValue, 0, n+resp.Header.Len())

	attrs = append(attrs, semconv.HTTPResponseStatusCode(resp.StatusCode()))

	if contentLen > 0 {
		attrs = append(attrs, semconv.HTTPResponseHeader("content-length", strconv.Itoa(contentLen)))
	}

	for k, v := range resp.Header.All() {
		key := strings.ToLower(string(k))

		// Skip content length as it is already handled.
		if key == "content-length" {
			continue
		}

		val := string(v)
		if _, ok := redactedHeaders[key]; ok {
			val = redactedHeaderValue
		}

		attrs = append(attrs, semconv.HTTPResponseHeader(key, val))
	}

	return attrs
}

var redactedHeaders = map[string]struct{}{
	"authorization":       {},
	"www-authenticate":    {},
	"x-api-key":           {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"cookie":              {},
	"set-cookie":          {},
}
