package configloader_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/config/configloader"
)

func TestProvideLoadsFromTheSuppliedPath(t *testing.T) {
	isolate(t)

	var cfg testConfig

	app := fxtest.New(t,
		configloader.Provide(configloader.Options[testConfig]{
			Name:     name,
			Filename: configFile(t, "http:\n  port: 1111\n"),
		}),
		configloader.WithFilename(configFile(t, "http:\n  port: 9090\n")),
		fx.Populate(&cfg),
	)
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, 9090, cfg.HTTP.Port)
}

func TestProvideFallsBackToTheOptionsFilename(t *testing.T) {
	isolate(t)

	var cfg testConfig

	app := fxtest.New(t,
		configloader.Provide(configloader.Options[testConfig]{
			Name:     name,
			Filename: configFile(t, "http:\n  port: 1111\n"),
		}),
		configloader.WithFilename(""),
		fx.Populate(&cfg),
	)
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, 1111, cfg.HTTP.Port)
}

func TestProvideTakesAFilenameConstructor(t *testing.T) {
	isolate(t)

	var cfg testConfig

	path := configFile(t, "http:\n  port: 9090\n")

	app := fxtest.New(t,
		configloader.Provide(configloader.Options[testConfig]{Name: name}),
		fx.Provide(configloader.AsFilename(func() string { return path })),
		fx.Populate(&cfg),
	)
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, 9090, cfg.HTTP.Port)
}
