// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"time"

	"go.opentelemetry.io/otel/log"
	"go.uber.org/zap/zapcore"
)

var (
	_ zapcore.ObjectEncoder = (*logObjectEncoder)(nil)
	_ zapcore.ArrayEncoder  = (*logArrayEncoder)(nil)
)

type namespace struct {
	name  string
	attrs []log.KeyValue
	next  *namespace
}

// logObjectEncoder implements zapcore.ObjectEncoder.
// It encodes given fields to OTel key-values.
type logObjectEncoder struct {
	// root is a pointer to the default namespace
	root *namespace
	// cur is a pointer to the namespace we're currently writing to.
	cur *namespace
}

func newLogObjectEncoder(n int) *logObjectEncoder {
	keyval := make([]log.KeyValue, 0, n)
	m := &namespace{
		attrs: keyval,
	}

	return &logObjectEncoder{
		root: m,
		cur:  m,
	}
}

// It iterates to the end of the linked list and appends namespace data.
// Run this function before accessing complete result.
func (m *logObjectEncoder) calculate(o *namespace) {
	if o.next == nil {
		return
	}

	m.calculate(o.next)

	o.attrs = append(o.attrs, log.Map(o.next.name, o.next.attrs...))
}

func (m *logObjectEncoder) AddArray(key string, v zapcore.ArrayMarshaler) error {
	arr := newArrayEncoder()
	err := v.MarshalLogArray(arr)
	m.cur.attrs = append(m.cur.attrs, log.Slice(key, arr.elems...))

	return err
}

func (m *logObjectEncoder) AddObject(k string, v zapcore.ObjectMarshaler) error {
	// Similar to console_encoder which uses capacity of 2:
	// https://github.com/uber-go/zap/blob/bd0cf0447951b77aa98dcfc1ac19e6f58d3ee64f/zapcore/console_encoder.go#L33.
	newobj := newLogObjectEncoder(2)
	err := v.MarshalLogObject(newobj)
	newobj.calculate(newobj.root)
	m.cur.attrs = append(m.cur.attrs, log.Map(k, newobj.root.attrs...))

	return err
}

func (m *logObjectEncoder) AddBinary(k string, v []byte) {
	m.cur.attrs = append(m.cur.attrs, log.Bytes(k, v))
}

func (m *logObjectEncoder) AddByteString(k string, v []byte) {
	m.cur.attrs = append(m.cur.attrs, log.String(k, string(v)))
}

func (m *logObjectEncoder) AddBool(k string, v bool) {
	m.cur.attrs = append(m.cur.attrs, log.Bool(k, v))
}

func (m *logObjectEncoder) AddDuration(k string, v time.Duration) {
	m.AddInt64(k, v.Nanoseconds())
}

func (m *logObjectEncoder) AddComplex128(k string, v complex128) {
	r := log.Float64("r", real(v))
	i := log.Float64("i", imag(v))
	m.cur.attrs = append(m.cur.attrs, log.Map(k, r, i))
}

func (m *logObjectEncoder) AddFloat64(k string, v float64) {
	m.cur.attrs = append(m.cur.attrs, log.Float64(k, v))
}

func (m *logObjectEncoder) AddInt64(k string, v int64) {
	m.cur.attrs = append(m.cur.attrs, log.Int64(k, v))
}

func (m *logObjectEncoder) AddInt(k string, v int) {
	m.cur.attrs = append(m.cur.attrs, log.Int(k, v))
}

func (m *logObjectEncoder) AddString(k string, v string) {
	m.cur.attrs = append(m.cur.attrs, log.String(k, v))
}

func (m *logObjectEncoder) AddUint64(k string, v uint64) {
	m.cur.attrs = append(m.cur.attrs,
		log.KeyValue{
			Key:   k,
			Value: assignUintValue(v),
		})
}

func (m *logObjectEncoder) AddReflected(k string, v any) error {
	m.cur.attrs = append(m.cur.attrs,
		log.KeyValue{
			Key:   k,
			Value: convertValue(v),
		})

	return nil
}

// OpenNamespace opens an isolated namespace where all subsequent fields will
// be added.
func (m *logObjectEncoder) OpenNamespace(k string) {
	keyValue := make([]log.KeyValue, 0, 5)
	s := &namespace{
		name:  k,
		attrs: keyValue,
	}
	m.cur.next = s
	m.cur = s
}

func (m *logObjectEncoder) AddComplex64(k string, v complex64) {
	m.AddComplex128(k, complex128(v))
}

func (m *logObjectEncoder) AddTime(k string, v time.Time) {
	m.AddInt64(k, v.UnixNano())
}

func (m *logObjectEncoder) AddFloat32(k string, v float32) {
	m.AddFloat64(k, float64(v))
}

func (m *logObjectEncoder) AddInt32(k string, v int32) {
	m.AddInt64(k, int64(v))
}

func (m *logObjectEncoder) AddInt16(k string, v int16) {
	m.AddInt64(k, int64(v))
}

func (m *logObjectEncoder) AddInt8(k string, v int8) {
	m.AddInt64(k, int64(v))
}

func (m *logObjectEncoder) AddUint(k string, v uint) {
	m.AddUint64(k, uint64(v))
}

func (m *logObjectEncoder) AddUint32(k string, v uint32) {
	m.AddInt64(k, int64(v))
}

func (m *logObjectEncoder) AddUint16(k string, v uint16) {
	m.AddInt64(k, int64(v))
}

func (m *logObjectEncoder) AddUint8(k string, v uint8) {
	m.AddInt64(k, int64(v))
}

func (m *logObjectEncoder) AddUintptr(k string, v uintptr) {
	m.AddUint64(k, uint64(v))
}

func assignUintValue(v uint64) log.Value {
	const maxInt64 = ^uint64(0) >> 1
	if v > maxInt64 {
		return log.Float64Value(float64(v))
	}

	return log.Int64Value(int64(v))
}

// logArrayEncoder implements [zapcore.ArrayEncoder].
type logArrayEncoder struct {
	elems []log.Value
}

func newArrayEncoder() *logArrayEncoder {
	return &logArrayEncoder{
		// Similar to console_encoder which uses capacity of 2:
		// https://github.com/uber-go/zap/blob/bd0cf0447951b77aa98dcfc1ac19e6f58d3ee64f/zapcore/console_encoder.go#L33.
		elems: make([]log.Value, 0, 2),
	}
}

func (a *logArrayEncoder) AppendArray(v zapcore.ArrayMarshaler) error {
	arr := newArrayEncoder()
	err := v.MarshalLogArray(arr)
	a.elems = append(a.elems, log.SliceValue(arr.elems...))

	return err
}

func (a *logArrayEncoder) AppendObject(v zapcore.ObjectMarshaler) error {
	// Similar to console_encoder which uses capacity of 2:
	// https://github.com/uber-go/zap/blob/bd0cf0447951b77aa98dcfc1ac19e6f58d3ee64f/zapcore/console_encoder.go#L33.
	m := newLogObjectEncoder(2)
	err := v.MarshalLogObject(m)
	m.calculate(m.root)
	a.elems = append(a.elems, log.MapValue(m.root.attrs...))

	return err
}

func (a *logArrayEncoder) AppendReflected(v any) error {
	a.elems = append(a.elems, convertValue(v))

	return nil
}

func (a *logArrayEncoder) AppendByteString(v []byte) {
	a.elems = append(a.elems, log.StringValue(string(v)))
}

func (a *logArrayEncoder) AppendBool(v bool) {
	a.elems = append(a.elems, log.BoolValue(v))
}

func (a *logArrayEncoder) AppendFloat64(v float64) {
	a.elems = append(a.elems, log.Float64Value(v))
}

func (a *logArrayEncoder) AppendFloat32(v float32) {
	a.AppendFloat64(float64(v))
}

func (a *logArrayEncoder) AppendInt(v int) {
	a.elems = append(a.elems, log.IntValue(v))
}

func (a *logArrayEncoder) AppendInt64(v int64) {
	a.elems = append(a.elems, log.Int64Value(v))
}

func (a *logArrayEncoder) AppendString(v string) {
	a.elems = append(a.elems, log.StringValue(v))
}

func (a *logArrayEncoder) AppendComplex128(v complex128) {
	r := log.Float64("r", real(v))
	i := log.Float64("i", imag(v))
	a.elems = append(a.elems, log.MapValue(r, i))
}

func (a *logArrayEncoder) AppendUint64(v uint64) {
	a.elems = append(a.elems, assignUintValue(v))
}

func (a *logArrayEncoder) AppendComplex64(v complex64)    { a.AppendComplex128(complex128(v)) }
func (a *logArrayEncoder) AppendDuration(v time.Duration) { a.AppendInt64(v.Nanoseconds()) }
func (a *logArrayEncoder) AppendInt32(v int32)            { a.AppendInt64(int64(v)) }
func (a *logArrayEncoder) AppendInt16(v int16)            { a.AppendInt64(int64(v)) }
func (a *logArrayEncoder) AppendInt8(v int8)              { a.AppendInt64(int64(v)) }
func (a *logArrayEncoder) AppendTime(v time.Time)         { a.AppendInt64(v.UnixNano()) }
func (a *logArrayEncoder) AppendUint(v uint)              { a.AppendUint64(uint64(v)) }
func (a *logArrayEncoder) AppendUint32(v uint32)          { a.AppendInt64(int64(v)) }
func (a *logArrayEncoder) AppendUint16(v uint16)          { a.AppendInt64(int64(v)) }
func (a *logArrayEncoder) AppendUint8(v uint8)            { a.AppendInt64(int64(v)) }
func (a *logArrayEncoder) AppendUintptr(v uintptr)        { a.AppendUint64(uint64(v)) }
