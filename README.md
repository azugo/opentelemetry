# Azugo OpenTelemetry

[![status-badge](https://ci.azugo.io/api/badges/azugo/opentelemetry/status.svg)](https://ci.azugo.io/azugo/opentelemetry)

Azugo framework [OpenTelemetry](https://opentelemetry.io) support.

## Features

* Tracing support for router handlers, HTTP client and cache.
* **Log correlation with traces** - automatically includes trace and span IDs in log entries for correlation in Elastic Kibana.

## Usage

```go
 t, err := opentelemetry.Use(app, config)
 if err != nil {
  panic(err)
 }

 app.AddTask(t)
```

If tracing context needs to be used to get current span from `*azugo.Context` use special helper to access it:

```go
span := trace.SpanFromContext(opentelemetry.FromContext(ctx))
```

## Environment variables used by the Azugo framework

### Special

* `OTEL_SDK_DISABLED` - Disable tracing.
* `OTEL_EXPORTER_OTLP_INSECURE_SKIP_VERIFY` - Insecure skip verify HTTPS certificates.
* `ELASTIC_APM_SECRET_TOKEN` - Support Elastic APM server authentification secret token.
* `ELASTIC_APM_SECRET_TOKEN_FILE` - Read Elastic APM secret token from specified file.
* `OTEL_TRACE_LOGGING` - Enable trace logging (default: `true`). If enabled, azugo logs will include trace and span IDs in log entries and will be sent to OpenTelemetry collector. This is necessary for correlation in Elastic Kibana or other OpenTelemetry compatible log management systems.

### Default

* `OTEL_EXPORTER_OTLP_ENDPOINT` - OpenTelemetry server endpoint address. If endpoint is not provided tracing will be disabled.
* `OTEL_SERVICE_NAME` - Override default service name defined in Azugo app.

For other configuration environment variables see [OpenTelemetry documentation](https://opentelemetry.io/docs/languages/sdk-configuration/).
