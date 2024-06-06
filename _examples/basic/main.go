package main

import (
	"azugo.io/azugo"
	"azugo.io/azugo/config"
	"azugo.io/azugo/server"
	"azugo.io/core/validation"
	"azugo.io/opentelemetry"
	"github.com/spf13/viper"
)

type Configuration struct {
	*config.Configuration `mapstructure:",squash"`

	Tracing *opentelemetry.Configuration `mapstructure:"tracing"`
}

func (c *Configuration) Bind(_ string, v *viper.Viper) {
	c.Configuration.Bind("", v)

	c.Tracing = config.Bind(c.Tracing, "tracing", v)
}

func (c *Configuration) Validate(validate *validation.Validate) error {
	if err := c.Tracing.Validate(validate); err != nil {
		return err
	}

	if err := validate.Struct(c); err != nil {
		return err
	}

	return nil
}

func main() {
	config := &Configuration{
		Configuration: config.New(),
	}

	app, err := server.New(nil, server.Options{
		AppName:       "azugo-otlp-test",
		AppVer:        "0.0.1",
		Configuration: config,
	})
	if err != nil {
		panic(err)
	}

	t, err := opentelemetry.Use(app, config.Tracing)
	if err != nil {
		panic(err)
	}

	app.AddTask(t)

	app.Get("/", func(ctx *azugo.Context) {
		resp, err := ctx.HTTPClient().WithBaseURL("http://localhost:8080").Get("/user/1")
		if err != nil {
			ctx.Error(err)

			return
		}

		ctx.JSON(struct {
			Content string `json:"content"`
		}{
			Content: string(resp),
		})
	})

	app.Get("/user/{id}", func(ctx *azugo.Context) {
		ctx.JSON(struct {
			ID string `json:"id"`
		}{
			ID: ctx.Params.String("id"),
		})
	})

	server.Run(app)
}
