package configloader_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestProvideSearchesWithoutAPath(t *testing.T) {
	isolate(t)

	var cfg testConfig

	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", "http:\n  port: 9090\n")

	app := fxtest.New(t,
		configloader.Provide(configloader.Options[testConfig]{Name: name, SearchPaths: []string{dir}}),
		fx.Populate(&cfg),
	)
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, 9090, cfg.HTTP.Port)
}

func TestProvideFailsTheAppWhenTheLoadFails(t *testing.T) {
	isolate(t)

	var cfg testConfig

	app := fx.New(
		fx.NopLogger,
		configloader.Provide(configloader.Options[testConfig]{Name: name}),
		configloader.WithFilename(configFile(t, "htpp:\n  port: 9090\n")),
		fx.Populate(&cfg),
	)

	require.ErrorIs(t, app.Err(), configloader.ErrUnknownKeys)
	require.ErrorIs(t, app.Start(t.Context()), configloader.ErrUnknownKeys)
}

type fannedOut struct {
	fx.Out

	HTTP HTTPConfig `mapstructure:"http"`
	DB   DBConfig   `mapstructure:"db"`
}

func TestProvideFansOutAConfigEmbeddingFxOut(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DB_HOST", "db")

	var (
		http HTTPConfig
		db   DBConfig
	)

	app := fxtest.New(t,
		configloader.Provide(configloader.Options[fannedOut]{Name: name}),
		configloader.WithFilename(configFile(t, "http:\n  port: 9090\n")),
		fx.Populate(&http, &db),
	)
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, HTTPConfig{Port: 9090}, http)
	assert.Equal(t, DBConfig{Host: "db"}, db)
}

func TestProvideReportsThroughTheAppsLogger(t *testing.T) {
	isolate(t)

	var cfg testConfig

	records, logger := newCapture()
	path := configFile(t, "http:\n  port: 9090\n")

	app := fxtest.New(t,
		configloader.Provide(configloader.Options[testConfig]{Name: name, Logger: slog.New(slog.DiscardHandler)}),
		configloader.WithFilename(path),
		fx.Supply(logger),
		fx.Populate(&cfg),
	)
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, []record{{
		Level:   slog.LevelInfo,
		Message: "loaded config file",
		Attrs:   map[string]any{"path": path},
	}}, records.all())
}

func TestProvideOptionReusedInAnotherApp(t *testing.T) {
	isolate(t)

	var first, second testConfig

	provide := configloader.Provide(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "http:\n  port: 1111\n"),
	})

	app := fxtest.New(
		t,
		provide,
		configloader.WithFilename(configFile(t, "http:\n  port: 9090\n")),
		fx.Populate(&first),
	)
	app.RequireStart()
	app.RequireStop()

	app = fxtest.New(t, provide, fx.Populate(&second))
	app.RequireStart()
	app.RequireStop()

	assert.Equal(t, 9090, first.HTTP.Port)
	assert.Equal(t, 1111, second.HTTP.Port)
}
