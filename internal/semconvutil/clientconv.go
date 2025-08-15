// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package semconvutil

import (
	"bytes"
	"strings"

	"azugo.io/core/http"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

var (
	redactedCredentials = []byte("REDACTED")
	redactedHeaderValue = "****"
)

// ClientRequest returns attributes for an HTTP request sent by client.
//
// The following attributes are always returned: "http.request.method", "url.scheme",
// "url.full", "server.address", "network.protocol.name", "network.protocol.version",
// "network.transport". The following attributes are returned if they
// related values are defined in req: "server.port", "user_agent.original".
func HTTPClientRequest(req *http.Request) []attribute.KeyValue {
	return cc.ClientRequest(req)
}

// ClientResponse returns attributes for an HTTP response received by client.
//
// The following attributes are always returned: "http.response.status_code".
func HTTPClientResponse(resp *http.Response) []attribute.KeyValue {
	return cc.ClientResponse(resp)
}

// httpConv are the HTTP semantic convention attributes defined for a version
// of the OpenTelemetry specification.
type clientConv struct {
	NetConv *netConv

	redactedHeaders map[string]struct{}

	ServerAddressKey                   attribute.Key
	ServerPortKey                      attribute.Key
	HTTPRequestMethodKey               attribute.Key
	URLFullKey                         attribute.Key
	URLSchemeHTTP                      attribute.KeyValue
	URLSchemeHTTPS                     attribute.KeyValue
	UserAgentOriginalKey               attribute.Key
	NetworkTransportTCP                attribute.KeyValue
	NetworkProtocolNameHTTP            attribute.KeyValue
	NetworkProtocolVersion11           attribute.KeyValue
	UserAgentNameKey                   attribute.Key
	HTTPRequestHeaderContentLengthKey  attribute.Key
	HTTPResponseStatusCodeKey          attribute.Key
	HTTPResponseHeaderContentLengthKey attribute.Key
}

var cc = &clientConv{
	NetConv: nc,

	redactedHeaders: map[string]struct{}{
		"authorization":       {},
		"www-authenticate":    {},
		"x-api-key":           {},
		"proxy-authenticate":  {},
		"proxy-authorization": {},
		"cookie":              {},
		"set-cookie":          {},
	},

	HTTPRequestMethodKey:               semconv.HTTPRequestMethodKey,
	URLFullKey:                         semconv.URLFullKey,
	URLSchemeHTTP:                      semconv.URLScheme("http"),
	URLSchemeHTTPS:                     semconv.URLScheme("https"),
	UserAgentOriginalKey:               semconv.UserAgentOriginalKey,
	NetworkTransportTCP:                semconv.NetworkTransportTCP,
	NetworkProtocolNameHTTP:            semconv.NetworkProtocolName("http"),
	NetworkProtocolVersion11:           semconv.NetworkProtocolVersion("1.1"),
	UserAgentNameKey:                   semconv.UserAgentNameKey,
	HTTPRequestHeaderContentLengthKey:  attribute.Key("http.request.header.content-length"),
	HTTPResponseStatusCodeKey:          semconv.HTTPResponseStatusCodeKey,
	HTTPResponseHeaderContentLengthKey: attribute.Key("http.response.header.content-length"),
}

// ClientRequest returns attributes for an HTTP request sent by client.
//
// The following attributes are always returned: "http.request.method", "url.scheme",
// "url.full", "server.address", "network.protocol.name", "network.protocol.version",
// "network.transport". The following attributes are returned if they
// related values are defined in req: "server.port", "user_agent.original".
func (c *clientConv) ClientRequest(req *http.Request) []attribute.KeyValue {
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

	attrs = append(attrs, c.method(string(req.Header.Method())))
	attrs = append(attrs, c.scheme(isTLS))
	attrs = append(attrs, c.NetConv.ServerAddress(host))
	attrs = append(attrs, c.URLFullKey.String(uri.String()))
	// HTTP client supports only HTTP/1.1 over TCP.
	attrs = append(attrs, c.NetworkTransportTCP)
	attrs = append(attrs, c.NetworkProtocolNameHTTP)
	attrs = append(attrs, c.NetworkProtocolVersion11)

	if hostPort > 0 {
		attrs = append(attrs, c.NetConv.ServerPort(hostPort))
	}

	if useragent != "" {
		attrs = append(attrs, c.UserAgentOriginalKey.String(useragent))
	}

	if contentLen > 0 {
		attrs = append(attrs, c.HTTPRequestHeaderContentLengthKey.Int(contentLen))
	}

	for k, v := range req.Header.All() {
		key := strings.ToLower(string(k))
		// Skip user agent and content length as they are already handled.
		if key == "user-agent" || key == "content-length" {
			continue
		}

		val := string(v)
		if _, ok := c.redactedHeaders[key]; ok {
			val = redactedHeaderValue
		}

		attrs = append(attrs, attribute.String("http.request.header."+key, val))
	}

	return attrs
}

func (c *clientConv) method(method string) attribute.KeyValue {
	if method == "" {
		return c.HTTPRequestMethodKey.String(fasthttp.MethodGet)
	}

	return c.HTTPRequestMethodKey.String(method)
}

func (c *clientConv) scheme(https bool) attribute.KeyValue {
	if https {
		return c.URLSchemeHTTPS
	}

	return c.URLSchemeHTTP
}

// ClientResponse returns attributes for an HTTP response received by client.
//
// The following attributes are always returned: "http.response.status_code".
func (c *clientConv) ClientResponse(resp *http.Response) []attribute.KeyValue {
	n := 1 // Response status code.

	contentLen := resp.Header.ContentLength()
	if contentLen > 0 {
		n++
	}

	attrs := make([]attribute.KeyValue, 0, n+resp.Header.Len())

	attrs = append(attrs, c.HTTPResponseStatusCodeKey.Int(resp.StatusCode()))

	if contentLen > 0 {
		attrs = append(attrs, c.HTTPResponseHeaderContentLengthKey.Int(contentLen))
	}

	for k, v := range resp.Header.All() {
		key := strings.ToLower(string(k))

		// Skip content length as it is already handled.
		if key == "content-length" {
			continue
		}

		val := string(v)
		if _, ok := c.redactedHeaders[key]; ok {
			val = redactedHeaderValue
		}

		attrs = append(attrs, attribute.String("http.response.header."+key, val))
	}

	return attrs
}
