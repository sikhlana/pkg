package configloader_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/sikhlana/pkg/config/configloader"
	"github.com/sikhlana/pkg/config/configvalidator"
)

func TestNameIsChecked(t *testing.T) {
	for _, n := range []string{"", "../etc", "my svc", "my.svc", "1password-sync"} {
		t.Run(n, func(t *testing.T) {
			_, err := configloader.Load(configloader.Options[testConfig]{Name: n})
			require.ErrorIs(t, err, configloader.ErrInvalidName)
		})
	}
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

func TestInvalidValueIsAViolation(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PORT", "80")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Defaults: testConfig{Hosts: []string{"a"}},
	})
	require.ErrorContains(t, err, "validating config: http.port: fails min=1024")

	violation, ok := errors.AsType[*configvalidator.Error](err)
	require.True(t, ok)
	assert.Equal(t, "http.port", violation.Key)
	assert.Zero(t, cfg)
}

func TestEveryViolationIsReported(t *testing.T) {
	isolate(t)

	type config struct {
		Host string `mapstructure:"host" validate:"required"`
		Port int    `mapstructure:"port" validate:"min=1024"`
	}

	_, err := configloader.Load(configloader.Options[config]{Name: name})

	violations, ok := errors.AsType[configvalidator.Errors](err)
	require.True(t, ok)
	assert.Len(t, violations, 2)
}

type MessagingConfig struct {
	Mode string     `mapstructure:"mode" validate:"oneof=file amqp"`
	AMQP AMQPConfig `mapstructure:"amqp" validate:"-"`
}

type AMQPConfig struct {
	URL string `mapstructure:"url" validate:"required,url"`
}

func TestDormantModeStructLoads(t *testing.T) {
	isolate(t)

	type config struct {
		Messaging MessagingConfig `mapstructure:"messaging"`
	}

	cfg, err := configloader.Load(configloader.Options[config]{
		Name:     name,
		Filename: configFile(t, "messaging:\n  mode: file\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "file", cfg.Messaging.Mode)
}

func TestIndependentLoads(t *testing.T) {
	isolate(t)

	type other struct {
		Port int `mapstructure:"port"`
	}

	t.Setenv("MY_SVC_HTTP_PORT", "9191")
	t.Setenv("OTHER_PORT", "7070")

	first, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Defaults: testConfig{HTTP: HTTPConfig{Host: "localhost"}},
	})
	require.NoError(t, err)

	second, err := configloader.Load(configloader.Options[other]{Name: "other"})
	require.NoError(t, err)

	again, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)

	assert.Equal(t, HTTPConfig{Host: "localhost", Port: 9191}, first.HTTP)
	assert.Equal(t, other{Port: 7070}, second)
	assert.Equal(t, HTTPConfig{Port: 9191}, again.HTTP)
}

func TestNoRecordHoldsAValue(t *testing.T) {
	isolate(t)

	records, logger := newCapture()

	t.Setenv("MY_SVC_HTTP_HOST", "planted-env")
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", writeFile(t, t.TempDir(), "password", "planted-secret\n"))

	_, err := configloader.Load(configloader.Options[testConfig]{
		Name:             name,
		Filename:         configFile(t, "labels:\n  team: planted-file\nhtpp: planted-unknown\n"),
		AllowUnknownKeys: true,
		Logger:           logger,
	})
	require.NoError(t, err)
	require.NotEmpty(t, records.all())
	assert.NotContains(t, records.rendered(), "planted")
}

func TestFailureIsReturnedNotLogged(t *testing.T) {
	isolate(t)

	records, logger := newCapture()
	path := configFile(t, "http:\n  port: 80\n")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path, Logger: logger})
	require.ErrorContains(t, err, "http.port: fails min=1024")
	assert.Equal(t, []record{{
		Level:   slog.LevelInfo,
		Message: "loaded config file",
		Attrs:   map[string]any{"path": path},
	}}, records.all())
}

func TestNoLoggerLoadsTheSame(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PORT", "9191")

	options := configloader.Options[testConfig]{
		Name:             name,
		Filename:         configFile(t, "htpp: 1\nhosts: [a]\n"),
		AllowUnknownKeys: true,
	}

	without, err := configloader.Load(options)
	require.NoError(t, err)

	records, logger := newCapture()
	options.Logger = logger

	with, err := configloader.Load(options)
	require.NoError(t, err)
	require.NotEmpty(t, records.all())
	assert.Equal(t, with, without)
}
