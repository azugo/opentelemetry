// Copyright 2024 Azugo
// SPDX-License-Identifier: Apache-2.0

package opentelemetry

import (
	"os"

	"azugo.io/core/config"
	"azugo.io/core/validation"
	"github.com/spf13/viper"
)

// Configuration section for OpenTracing.
type Configuration struct {
	Disabled              bool   `mapstructure:"disabled"`
	Endpoint              string `mapstructure:"endpoint"`
	InsecureSkipVerify    bool   `mapstructure:"insecure_skip_verify"`
	ServiceName           string `mapstructure:"service_name"`
	ElasticAPMSecretToken string `mapstructure:"elastic_apm_secret_token"`
	ResourceAttributes    string `mapstructure:"resource_attributes"`
}

// Validate OpenTracing configuration section.
func (c *Configuration) Validate(valid *validation.Validate) error {
	return valid.Struct(c)
}

// Bind OpenTracing configuration section.
func (c *Configuration) Bind(prefix string, v *viper.Viper) {
	st, _ := config.LoadRemoteSecret("ELASTIC_APM_SECRET_TOKEN")

	v.SetDefault(prefix+".disabled", false)
	v.SetDefault(prefix+".insecure_skip_verify", false)
	v.SetDefault(prefix+".elastic_apm_secret_token", st)

	_ = v.BindEnv(prefix+".resource_attributes", "OTEL_RESOURCE_ATTRIBUTES")
	_ = v.BindEnv(prefix+".disabled", "OTEL_SDK_DISABLED")
	_ = v.BindEnv(prefix+".endpoint", "OTEL_EXPORTER_OTLP_ENDPOINT")
	_ = v.BindEnv(prefix+".insecure_skip_verify", "OTEL_EXPORTER_OTLP_INSECURE_SKIP_VERIFY")
	_ = v.BindEnv(prefix+".service_name", "OTEL_SERVICE_NAME")
	_ = v.BindEnv(prefix+".elastic_apm_secret_token", "ELASTIC_APM_SECRET_TOKEN")
}

// IsDisabled returns true if the tracing is disabled.
func (c *Configuration) IsDisabled() bool {
	return c.Disabled || (c.Endpoint == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "")
}
