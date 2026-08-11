package opentelemetry

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"azugo.io/azugo"
	"azugo.io/core/cache"
	corehttp "azugo.io/core/http"
	"github.com/go-quicktest/qt"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// receivedTelemetry reports whether the collector internal metrics page shows
// a non-zero value for the metric received over the given transport (name is
// matched by prefix to cover the optional _total suffix).
func receivedTelemetry(body, name, transport string) bool {
	for line := range strings.Lines(body) {
		if !strings.HasPrefix(line, name) || !strings.Contains(line, `transport="`+transport+`"`) {
			continue
		}

		fields := strings.Fields(line)

		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err == nil && v > 0 {
			return true
		}
	}

	return false
}

func fetchTelemetry(t *testing.T, url string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	qt.Assert(t, qt.IsNil(err))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	qt.Assert(t, qt.IsNil(err))

	return string(body)
}

// TestOTLPIntegration exercises a real app wired with opentelemetry.Use
// against an OTLP collector and verifies traces, logs and metrics arrive.
func TestOTLPIntegration(t *testing.T) {
	endpoint := os.Getenv("OTEL_TEST_ENDPOINT")
	telemetry := os.Getenv("OTEL_TEST_TELEMETRY")

	if endpoint == "" || telemetry == "" {
		t.Skip("OTEL_TEST_ENDPOINT and OTEL_TEST_TELEMETRY are not set")
	}

	app := azugo.NewTestApp()

	// Mark requests loggable before Use registers the tracing middleware so
	// the request span is recorded (the RequestLogger middleware does this in
	// real apps).
	app.UsePriority(logRequest)

	config := &Configuration{
		Endpoint:    endpoint,
		Protocol:    os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"),
		ServiceName: "otel-integration",
	}

	tasker, err := Use(app.App, config)
	qt.Assert(t, qt.IsNil(err))

	i, err := cache.Create[string](app.App.Cache(), "otel-int")
	qt.Assert(t, qt.IsNil(err))

	app.Get("/work", func(ctx *azugo.Context) {
		if err := i.Set(ctx, "key", "value"); err != nil {
			ctx.Error(err)

			return
		}

		if _, err := i.Get(ctx, "key"); err != nil {
			ctx.Error(err)

			return
		}

		ctx.StatusCode(corehttp.StatusNoContent)
	})

	app.Start(t)

	// TestApp replaces the app logger with an observed test core, so emit a
	// log record through the same provider and zap core Use wires for the
	// otel log driver.
	lp, err := newLogProvider(app.App, config)
	qt.Assert(t, qt.IsNil(err))

	zap.New(newLogCore(context.Background(), lp, "otel-integration", zapcore.InfoLevel)).Info("otel integration test log")
	qt.Assert(t, qt.IsNil(lp.Shutdown(context.Background())))

	resp, err := app.TestClient().Get("/work")
	defer fasthttp.ReleaseResponse(resp)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(resp.StatusCode(), corehttp.StatusNoContent))

	app.Stop()
	// Shutting down the providers flushes all pending telemetry.
	tasker.Stop()

	// Collector labels received data by transport, so parallel runs over
	// different protocols verify only their own telemetry.
	transport := "http"
	if config.Protocol == ProtocolGRPC {
		transport = "grpc"
	}

	deadline := time.Now().Add(30 * time.Second)

	var spans, logs, metrics bool

	for time.Now().Before(deadline) {
		body := fetchTelemetry(t, telemetry)

		spans = receivedTelemetry(body, "otelcol_receiver_accepted_spans", transport)
		logs = receivedTelemetry(body, "otelcol_receiver_accepted_log_records", transport)
		metrics = receivedTelemetry(body, "otelcol_receiver_accepted_metric_points", transport)

		if spans && logs && metrics {
			break
		}

		time.Sleep(time.Second)
	}

	qt.Check(t, qt.IsTrue(spans), qt.Commentf("no spans received by collector"))
	qt.Check(t, qt.IsTrue(logs), qt.Commentf("no log records received by collector"))
	qt.Check(t, qt.IsTrue(metrics), qt.Commentf("no metric points received by collector"))
}
