// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package semconvutil

import (
	"net"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	NetworkFamilyIPv4 = "inet"
	NetworkFamilyIPv6 = "inet6"
	NetworkFamilyUnix = "unix"
)

// NetTransport returns a trace attribute describing the transport protocol of the
// passed network. See the net.Dial for information about acceptable network
// values.
func NetTransport(network string) attribute.KeyValue {
	return nc.Transport(network)
}

// netConv are the network semantic convention attributes defined for a version
// of the OpenTelemetry specification.
type netConv struct {
	ServerAddressKey       attribute.Key
	ServerPortKey          attribute.Key
	ClientAddressKey       attribute.Key
	ClientPortKey          attribute.Key
	NetworkPeerAddressKey  attribute.Key
	NetworkPeerPortKey     attribute.Key
	NetworkProtocolName    attribute.Key
	NetworkProtocolVersion attribute.Key
	NetworkTransportUnix   attribute.KeyValue
	NetworkTransportTCP    attribute.KeyValue
	NetworkTransportUDP    attribute.KeyValue
	NetworkTransportOther  attribute.KeyValue
}

var nc = &netConv{
	ServerAddressKey:       semconv.ServerAddressKey,
	ServerPortKey:          semconv.ServerPortKey,
	ClientAddressKey:       semconv.ClientAddressKey,
	ClientPortKey:          semconv.ClientPortKey,
	NetworkPeerAddressKey:  semconv.NetworkPeerAddressKey,
	NetworkPeerPortKey:     semconv.NetworkPeerPortKey,
	NetworkProtocolName:    semconv.NetworkProtocolNameKey,
	NetworkProtocolVersion: semconv.NetworkProtocolVersionKey,
	NetworkTransportUnix:   semconv.NetworkTransportUnix,
	NetworkTransportTCP:    semconv.NetworkTransportTCP,
	NetworkTransportUDP:    semconv.NetworkTransportUDP,
	NetworkTransportOther:  semconv.NetworkTransportKey.String("other"),
}

func (c *netConv) Transport(network string) attribute.KeyValue {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return c.NetworkTransportTCP
	case "udp", "udp4", "udp6":
		return c.NetworkTransportUDP
	case "unix", "unixgram", "unixpacket":
		return c.NetworkTransportUnix
	default:
		// "ip:*", "ip4:*", and "ip6:*" all are considered other.
		return c.NetworkTransportOther
	}
}

func (c *netConv) server(addr string) []attribute.KeyValue {
	host, port := splitHostPort(addr)
	if host == "" {
		return nil
	}

	kvs := []attribute.KeyValue{
		c.ServerAddress(host),
	}
	if port >= 0 {
		kvs = append(kvs, c.ServerPort(port))
	}

	return kvs
}

func (c *netConv) ServerAddress(name string) attribute.KeyValue {
	return c.ServerAddressKey.String(name)
}

func (c *netConv) ServerPort(port int) attribute.KeyValue {
	return c.ServerPortKey.Int(port)
}

func (c *netConv) ClientAddress(addr string) attribute.KeyValue {
	return c.ClientAddressKey.String(addr)
}

func (c *netConv) ClientPort(port int) attribute.KeyValue {
	return c.ClientPortKey.Int(port)
}

func family(network, address string) string {
	switch network {
	case "unix", "unixgram", "unixpacket":
		return NetworkFamilyUnix
	default:
		if ip := net.ParseIP(address); ip != nil {
			if ip.To4() == nil {
				return NetworkFamilyIPv6
			}

			return NetworkFamilyIPv4
		}
	}

	return ""
}

func (c *netConv) NetworkPeerAddress(addr string) attribute.KeyValue {
	return c.NetworkPeerAddressKey.String(addr)
}

func (c *netConv) NetworkPeerPort(port int) attribute.KeyValue {
	return c.NetworkPeerPortKey.Int(port)
}

func (c *netConv) networkPeer(addr string) []attribute.KeyValue {
	host, port := splitHostPort(addr)
	if host == "" {
		return nil
	}

	kvs := []attribute.KeyValue{
		nc.NetworkPeerAddress(host),
	}

	if port >= 0 {
		kvs = append(kvs, nc.NetworkPeerPort(port))
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

	return host, int(p)
}

func netProtocol(proto string) (string, string) {
	name, version, _ := strings.Cut(proto, "/")
	name = strings.ToLower(name)

	return name, version
}
