// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package semconvutil

import (
	"fmt"

	"azugo.io/azugo"
	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// HTTPServerRequest returns trace attributes for an HTTP request received by a
// server.
//
// The server must be the primary server name if it is known. For example this
// would be the ServerName directive
// (https://httpd.apache.org/docs/2.4/mod/core.html#servername) for an Apache
// server, and the server_name directive
// (http://nginx.org/en/docs/http/ngx_http_core_module.html#server_name) for an
// nginx server. More generically, the primary server name would be the host
// header value that matches the default virtual host of an HTTP server. It
// should include the host identifier and if a port is used to route to the
// server that port identifier should be included as an appropriate port
// suffix.
//
// If the primary server name is not known, server should be an empty string.
// The req Host will be used to determine the server instead.
//
// The following attributes are always returned: "http.request.method", "url.scheme",
// "url.path", "url.full", "server.address". The following attributes are returned if they
// related values are defined in req: "server.port", "network.peer.address",
// "network.peer.port", "user_agent.original", "client.address",
// "network.protocol.name", "network.protocol.version".
func HTTPServerRequest(ctx *azugo.Context) []attribute.KeyValue {
	return hc.ServerRequest(ctx)
}

// HTTPServerStatus returns a span status code and message for an HTTP status code
// value returned by a server. Status codes in the 400-499 range are not
// returned as errors.
func HTTPServerStatus(code int) (codes.Code, string) {
	return hc.ServerStatus(code)
}

// httpConv are the HTTP semantic convention attributes defined for a version
// of the OpenTelemetry specification.
type httpConv struct {
	NetConv *netConv

	HTTPRequestMethodKey attribute.Key
	// HTTPRequestContentLengthKey  attribute.Key
	// HTTPResponseContentLengthKey attribute.Key
	HTTPRouteKey              attribute.Key
	URLSchemeHTTP             attribute.KeyValue
	URLSchemeHTTPS            attribute.KeyValue
	HTTPResponseStatusCodeKey attribute.Key
	URLPathKey                attribute.Key
	URLFullKey                attribute.Key
	UserAgentOriginalKey      attribute.Key
}

var hc = &httpConv{
	NetConv: nc,

	HTTPRequestMethodKey: semconv.HTTPRequestMethodKey,
	// HTTPRequestContentLengthKey:  semconv.HTTPRequestContentLengthKey,
	// HTTPResponseContentLengthKey: semconv.HTTPResponseContentLengthKey,
	HTTPRouteKey:              semconv.HTTPRouteKey,
	URLSchemeHTTP:             semconv.URLScheme("http"),
	URLSchemeHTTPS:            semconv.URLScheme("https"),
	HTTPResponseStatusCodeKey: semconv.HTTPResponseStatusCodeKey,
	URLPathKey:                semconv.URLPathKey,
	URLFullKey:                semconv.URLFullKey,
	UserAgentOriginalKey:      semconv.UserAgentOriginalKey,
}

// ServerRequest returns attributes for an HTTP request received by a server.
//
// The server must be the primary server name if it is known. For example this
// would be the ServerName directive
// (https://httpd.apache.org/docs/2.4/mod/core.html#servername) for an Apache
// server, and the server_name directive
// (http://nginx.org/en/docs/http/ngx_http_core_module.html#server_name) for an
// nginx server. More generically, the primary server name would be the host
// header value that matches the default virtual host of an HTTP server. It
// should include the host identifier and if a port is used to route to the
// server that port identifier should be included as an appropriate port
// suffix.
//
// If the primary server name is not known, server should be an empty string.
// The req Host will be used to determine the server instead.
//
// The following attributes are always returned: "http.request.method", "url.scheme",
// "url.path", "url.full", "server.address". The following attributes are returned if they
// related values are defined in req: "server.port", "network.peer.address",
// "network.peer.port", "user_agent.original", "client.address",
// "network.protocol.name", "network.protocol.version".
func (c *httpConv) ServerRequest(ctx *azugo.Context) []attribute.KeyValue {
	/*
		The following semantic conventions are returned if present:
		http.request.method        string
		url.scheme                 string
		server.address             string
		server.port                int
		network.peer.address       string
		network.peer.port          int
		user_agent.original        string
		client.address             string
		network.protocol.name      string Note: not set if the value is "http".
		network.protocol.version   string
		url.path                   string Note: doesn't include the query parameter.
		url.full                   string Note: doesn't include the query parameter.

		The following semantic conventions are not returned:
		http.response.status_code             This requires the response.
		http.request.header.content-length    This requires the len() of body, which can mutate it.
		http.response.header.content-length   This requires the response.
		http.route                            This is not available.
		network.local.address                 The request doesn't have access to the underlying socket.
		network.local.port                    The request doesn't have access to the underlying socket.
	*/
	n := 3 // Method, scheme and host name.
	host, p := splitHostPort(ctx.Host())

	hostPort := requiredHTTPPort(ctx.IsTLS(), p)
	if hostPort > 0 {
		n++
	}

	peer, peerPort := splitHostPort(ctx.Context().RemoteAddr().String())
	if peer != "" {
		n++
		if peerPort > 0 {
			n++
		}
	}

	useragent := ctx.UserAgent()
	if useragent != "" {
		n++
	}

	clientIP := ctx.IP().String()
	if clientIP != "" {
		n++
	}

	target := ctx.Path()
	if target != "" {
		n++
	}

	fullURL := ctx.BaseURL() + target
	if fullURL != "" {
		n++
	}

	protoName, protoVersion := netProtocol(string(ctx.Request().Header.Protocol()))
	if protoName != "" && protoName != "http" {
		n++
	}

	if protoVersion != "" {
		n++
	}

	user := ctx.User()
	if user != nil && user.Authorized() {
		n++
	}

	attrs := make([]attribute.KeyValue, 0, n)

	attrs = append(attrs, c.method(ctx.Method()))
	attrs = append(attrs, c.scheme(ctx.IsTLS()))
	attrs = append(attrs, c.NetConv.ServerAddress(host))

	if hostPort > 0 {
		attrs = append(attrs, c.NetConv.ServerPort(hostPort))
	}

	if user != nil && user.Authorized() {
		if id := user.ID(); id != "" {
			attrs = append(attrs, semconv.UserID(id))
		}
	}

	if peer != "" {
		// The Go HTTP server sets RemoteAddr to "IP:port", this will not be a
		// file-path that would be interpreted with a sock family.
		attrs = append(attrs, c.NetConv.NetworkPeerAddress(peer))
		if peerPort > 0 {
			attrs = append(attrs, c.NetConv.NetworkPeerPort(peerPort))
		}
	}

	if useragent != "" {
		attrs = append(attrs, c.UserAgentOriginalKey.String(useragent))
	}

	if clientIP != "" {
		attrs = append(attrs, c.NetConv.ClientAddress(clientIP))
	}

	if target != "" {
		attrs = append(attrs, c.URLPathKey.String(target))
	}

	if fullURL != "" {
		attrs = append(attrs, c.URLFullKey.String(fullURL))
	}

	if protoName != "" && protoName != "http" {
		attrs = append(attrs, c.NetConv.NetworkProtocolName.String(protoName))
	}

	if protoVersion != "" {
		attrs = append(attrs, c.NetConv.NetworkProtocolVersion.String(protoVersion))
	}

	return attrs
}

func (c *httpConv) method(method string) attribute.KeyValue {
	if method == "" {
		return c.HTTPRequestMethodKey.String(fasthttp.MethodGet)
	}

	return c.HTTPRequestMethodKey.String(method)
}

func (c *httpConv) scheme(https bool) attribute.KeyValue {
	if https {
		return c.URLSchemeHTTPS
	}

	return c.URLSchemeHTTP
}

func requiredHTTPPort(https bool, port int) int {
	if port > 0 {
		if https && port != 443 {
			return port
		}

		if !https && port != 80 {
			return port
		}
	}

	return -1
}

// ServerStatus returns a span status code and message for an HTTP status code
// value returned by a server. Status codes in the 400-499 range are not
// returned as errors.
func (c *httpConv) ServerStatus(code int) (codes.Code, string) {
	if code < 100 || code >= 600 {
		return codes.Error, fmt.Sprintf("Invalid HTTP status code %d", code)
	}

	if code >= 500 {
		return codes.Error, ""
	}

	return codes.Unset, ""
}
