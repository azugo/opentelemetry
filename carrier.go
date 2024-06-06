// Copyright 2024 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"net/http"

	"azugo.io/azugo"
	"go.opentelemetry.io/otel/propagation"
)

func azugoHeaderCarrier(ctx *azugo.Context) propagation.TextMapCarrier {
	h := make(http.Header)

	ctx.Request().Header.VisitAll(func(k, v []byte) {
		h.Add(string(k), string(v))
	})

	return propagation.HeaderCarrier(h)
}
