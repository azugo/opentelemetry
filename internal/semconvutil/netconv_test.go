// Copyright 2024 The OpenTelemetry Authors, Azugo
// SPDX-License-Identifier: Apache-2.0

package semconvutil

import (
	"testing"

	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kr/pretty"

	"go.opentelemetry.io/otel/attribute"
)

const (
	addr = "127.0.0.1"
	port = 1834
)

func TestNetTransport(t *testing.T) {
	transports := map[string]attribute.KeyValue{
		"tcp":        attribute.String("network.transport", "tcp"),
		"tcp4":       attribute.String("network.transport", "tcp"),
		"tcp6":       attribute.String("network.transport", "tcp"),
		"udp":        attribute.String("network.transport", "udp"),
		"udp4":       attribute.String("network.transport", "udp"),
		"udp6":       attribute.String("network.transport", "udp"),
		"unix":       attribute.String("network.transport", "unix"),
		"unixgram":   attribute.String("network.transport", "unix"),
		"unixpacket": attribute.String("network.transport", "unix"),
		"ip:1":       attribute.String("network.transport", "other"),
		"ip:icmp":    attribute.String("network.transport", "other"),
		"ip4:proto":  attribute.String("network.transport", "other"),
		"ip6:proto":  attribute.String("network.transport", "other"),
	}

	for network, want := range transports {
		qt.Check(t, qt.Equals(NetTransport(network), want))
	}
}

func TestServer(t *testing.T) {
	testAddrs(t, []addrTest{
		{address: "", expected: nil},
		{address: "192.0.0.1", expected: []attribute.KeyValue{
			nc.ServerAddress("192.0.0.1"),
		}},
		{address: "192.0.0.1:9090", expected: []attribute.KeyValue{
			nc.ServerAddress("192.0.0.1"),
			nc.ServerPort(9090),
		}},
	}, nc.server)
}

func TestServerAddress(t *testing.T) {
	expected := attribute.Key("server.address").String(addr)
	qt.Check(t, qt.Equals(nc.ServerAddress(addr), expected))
}

func TestServerPort(t *testing.T) {
	expected := attribute.Key("server.port").Int(port)
	qt.Check(t, qt.Equals(nc.ServerPort(port), expected))
}

func TestNetworkPeer(t *testing.T) {
	testAddrs(t, []addrTest{
		{address: "", expected: nil},
		{address: "example.com", expected: []attribute.KeyValue{
			nc.NetworkPeerAddress("example.com"),
		}},
		{address: "/tmp/file", expected: []attribute.KeyValue{
			nc.NetworkPeerAddress("/tmp/file"),
		}},
		{address: "192.0.0.1", expected: []attribute.KeyValue{
			nc.NetworkPeerAddress("192.0.0.1"),
		}},
		{address: ":9090", expected: nil},
		{address: "192.0.0.1:9090", expected: []attribute.KeyValue{
			nc.NetworkPeerAddress("192.0.0.1"),
			nc.NetworkPeerPort(9090),
		}},
	}, nc.networkPeer)
}

func TestNetworkPeerAddress(t *testing.T) {
	expected := attribute.Key("network.peer.address").String(addr)
	qt.Check(t, qt.Equals(nc.NetworkPeerAddress(addr), expected))
}

func TestNetworkPeerPort(t *testing.T) {
	expected := attribute.Key("network.peer.port").Int(port)
	qt.Check(t, qt.Equals(nc.NetworkPeerPort(port), expected))
}

func TestNetFamily(t *testing.T) {
	tests := []struct {
		network string
		address string
		expect  string
	}{
		{"", "", ""},
		{"unix", "", "unix"},
		{"unix", "gibberish", "unix"},
		{"unixgram", "", "unix"},
		{"unixgram", "gibberish", "unix"},
		{"unixpacket", "gibberish", "unix"},
		{"tcp", "123.0.2.8", "inet"},
		{"tcp", "gibberish", ""},
		{"", "123.0.2.8", "inet"},
		{"", "gibberish", ""},
		{"tcp", "fe80::1", "inet6"},
		{"", "fe80::1", "inet6"},
	}

	for _, test := range tests {
		got := family(test.network, test.address)
		qt.Check(t, qt.Equals(got, test.expect), qt.Commentf(test.network+"/"+test.address))
	}
}

func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		hostport string
		host     string
		port     int
	}{
		{"", "", -1},
		{":8080", "", 8080},
		{"127.0.0.1", "127.0.0.1", -1},
		{"www.example.com", "www.example.com", -1},
		{"127.0.0.1%25en0", "127.0.0.1%25en0", -1},
		{"[]", "", -1}, // Ensure this doesn't panic.
		{"[fe80::1", "", -1},
		{"[fe80::1]", "fe80::1", -1},
		{"[fe80::1%25en0]", "fe80::1%25en0", -1},
		{"[fe80::1]:8080", "fe80::1", 8080},
		{"[fe80::1]::", "", -1}, // Too many colons.
		{"127.0.0.1:", "127.0.0.1", -1},
		{"127.0.0.1:port", "127.0.0.1", -1},
		{"127.0.0.1:8080", "127.0.0.1", 8080},
		{"www.example.com:8080", "www.example.com", 8080},
		{"127.0.0.1%25en0:8080", "127.0.0.1%25en0", 8080},
	}

	for _, test := range tests {
		h, p := splitHostPort(test.hostport)
		qt.Check(t, qt.Equals(h, test.host), qt.Commentf(test.hostport))
		qt.Check(t, qt.Equals(p, test.port), qt.Commentf(test.hostport))
	}
}

type addrTest struct {
	address  string
	expected []attribute.KeyValue
}

func testAddrs(t *testing.T, tests []addrTest, f func(string) []attribute.KeyValue) {
	t.Helper()

	for _, test := range tests {
		got := f(test.address)
		qt.Check(t, qt.Equals(cap(got), cap(test.expected)), qt.Commentf("slice capacity"))
		qt.Check(t, qt.CmpEquals(got, test.expected,
			cmpopts.SortSlices(func(x, y any) bool {
				return pretty.Sprint(x) < pretty.Sprint(y)
			}),
			cmpopts.EquateComparable(attribute.KeyValue{}),
		), qt.Commentf(test.address))
	}
}

func TestNetProtocol(t *testing.T) {
	type testCase struct {
		name, version string
	}
	tests := map[string]testCase{
		"HTTP/1.0":        {name: "http", version: "1.0"},
		"HTTP/1.1":        {name: "http", version: "1.1"},
		"HTTP/2":          {name: "http", version: "2"},
		"HTTP/3":          {name: "http", version: "3"},
		"SPDY":            {name: "spdy"},
		"SPDY/2":          {name: "spdy", version: "2"},
		"QUIC":            {name: "quic"},
		"unknown/proto/2": {name: "unknown", version: "proto/2"},
		"other":           {name: "other"},
	}

	for proto, want := range tests {
		name, version := netProtocol(proto)
		qt.Check(t, qt.Equals(name, want.name))
		qt.Check(t, qt.Equals(version, want.version))
	}
}
