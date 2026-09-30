// Copyright 2026 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"context"
	"sync"
	"testing"
	"unsafe"

	"github.com/go-quicktest/qt"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// logRecorder keeps emitted records the way the batch processor queues them
// for export: cloned, but still sharing string memory with the emitter.
type logRecorder struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (*logRecorder) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (r *logRecorder) OnEmit(_ context.Context, record *sdklog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.records = append(r.records, record.Clone())

	return nil
}

func (*logRecorder) Shutdown(context.Context) error   { return nil }
func (*logRecorder) ForceFlush(context.Context) error { return nil }

// TestLogValuesSurviveBufferReuse verifies that the log bridge copies strings
// before handing them to the SDK. Azugo request log fields alias fasthttp
// buffers that the next request reuses before the batch processor exports.
func TestLogValuesSurviveBufferReuse(t *testing.T) {
	rec := &logRecorder{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(rec))
	defer func() { _ = lp.Shutdown(context.Background()) }()

	logger := zap.New(newLogCore(context.Background(), lp, "test", zapcore.InfoLevel))

	// A string over a reused buffer, the way azugo exposes the request method.
	buf := []byte("DELETE")
	method := unsafe.String(&buf[0], len(buf))

	logger.With(zap.String("with", method)).Info(method,
		zap.String("string", method),
		zap.Strings("strings", []string{method}),
		zap.Any("map", map[string]string{"key": method}),
	)

	// The next request is parsed into the same buffer.
	copy(buf, "GET")

	qt.Assert(t, qt.HasLen(rec.records, 1))

	record := rec.records[0]
	qt.Check(t, qt.Equals(record.Body().AsString(), "DELETE"))

	attrs := map[string]attribute.Value{}
	record.WalkAttributes(func(kv attribute.KeyValue) bool {
		attrs[string(kv.Key)] = kv.Value

		return true
	})

	qt.Check(t, qt.Equals(attrs["with"].AsString(), "DELETE"))
	qt.Check(t, qt.Equals(attrs["string"].AsString(), "DELETE"))
	qt.Assert(t, qt.HasLen(attrs["strings"].AsSlice(), 1))
	qt.Check(t, qt.Equals(attrs["strings"].AsSlice()[0].AsString(), "DELETE"))
	qt.Assert(t, qt.HasLen(attrs["map"].AsMap(), 1))
	qt.Check(t, qt.Equals(attrs["map"].AsMap()[0].Value.AsString(), "DELETE"))
}
