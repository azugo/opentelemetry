// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package semconvutil

import (
	"net"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const unixNetwork = "unix"

// NetTransport returns a trace attribute describing the transport protocol of the
// passed network. See the net.Dial for information about acceptable network
// values.
func NetTransport(network string) attribute.KeyValue {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return semconv.NetworkTransportTCP
	case "udp", "udp4", "udp6":
		return semconv.NetworkTransportUDP
	case unixNetwork, "unixgram", "unixpacket":
		return semconv.NetworkTransportUnix
	default:
		// "ip:*", "ip4:*", and "ip6:*" all are considered other.
		return semconv.NetworkTransportKey.String("other")
	}
}

func netServerAttrs(addr string) []attribute.KeyValue {
	host, port := splitHostPort(addr)
	if host == "" {
		return nil
	}

	kvs := []attribute.KeyValue{
		semconv.ServerAddress(host),
	}
	if port >= 0 {
		kvs = append(kvs, semconv.ServerPort(port))
	}

	return kvs
}

func family(network, address string) string {
	switch network {
	case unixNetwork, "unixgram", "unixpacket":
		return unixNetwork
	default:
		if ip := net.ParseIP(address); ip != nil {
			if ip.To4() == nil {
				return "inet6"
			}

			return "inet"
		}
	}

	return ""
}

func networkPeerAttrs(addr string) []attribute.KeyValue {
	host, port := splitHostPort(addr)
	if host == "" {
		return nil
	}

	kvs := []attribute.KeyValue{
		semconv.NetworkPeerAddress(host),
	}

	if port >= 0 {
		kvs = append(kvs, semconv.NetworkPeerPort(port))
	}

	return kvs
}

// splitHostPort splits a network address hostport of the form "host",
// "host%zone", "[host]", "[host%zone], "host:port", "host%zone:port",
// "[host]:port", "[host%zone]:port", or ":port" into host or host%zone and
// port.
//
// An empty host is returned if it is not provided or unparsable. A negative
// port is returned if it is not provided or unparsable.
func splitHostPort(hostport string) (string, int) {
	if strings.HasPrefix(hostport, "[") {
		addrEnd := strings.LastIndex(hostport, "]")
		if addrEnd < 0 {
			// Invalid hostport.
			return "", -1
		}

		if i := strings.LastIndex(hostport[addrEnd:], ":"); i < 0 {
			return hostport[1:addrEnd], -1
		}
	} else {
		if i := strings.LastIndex(hostport, ":"); i < 0 {
			return hostport, -1
		}
	}

	host, pStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return host, -1
	}

	p, err := strconv.ParseUint(pStr, 10, 16)
	if err != nil {
		return host, -1
	}

	if p > 65535 {
		return host, -1
	}

	return host, int(p)
}

func netProtocol(proto string) (string, string) {
	name, version, _ := strings.Cut(proto, "/")
	name = strings.ToLower(name)

	return name, version
}
