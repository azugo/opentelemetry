// Copyright 2024 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"azugo.io/core/http"
)

type headerCarrier http.Request

func (h headerCarrier) Set(key, value string) {
	h.Header.Set(key, value)
}

func (h headerCarrier) Get(key string) string {
	return string(h.Header.Peek(key))
}

func (h headerCarrier) Keys() []string {
	keys := make([]string, 0, h.Header.Len())

	uniq := make(map[string]struct{})

	for k := range h.Header.All() {
		key := string(k)
		if _, ok := uniq[key]; ok {
			continue
		}

		keys = append(keys, key)
		uniq[key] = struct{}{}
	}

	return keys
}
