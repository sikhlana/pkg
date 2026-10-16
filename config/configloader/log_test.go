package configloader_test

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configloader"
)

func TestReportsTheFileTheSearchFound(t *testing.T) {
	isolate(t)

	records, logger := newCapture()
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", "http:\n  port: 9090\n")

	_, err := configloader.Load(
		configloader.Options[testConfig]{Name: name, SearchPaths: []string{dir}, Logger: logger},
	)
	require.NoError(t, err)
	assert.Equal(t, []record{{
		Level:   slog.LevelInfo,
		Message: "loaded config file",
		Attrs:   map[string]any{"path": path},
	}}, records.all())
}

func TestReportsTheExplicitFile(t *testing.T) {
	isolate(t)

	records, logger := newCapture()
	path := configFile(t, "http:\n  port: 9090\n")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path, Logger: logger})
	require.NoError(t, err)
	assert.Equal(t, []record{{
		Level:   slog.LevelInfo,
		Message: "loaded config file",
		Attrs:   map[string]any{"path": path},
	}}, records.all())
}

func TestReportsThatNoFileWasFound(t *testing.T) {
	isolate(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	records, logger := newCapture()
	dir := t.TempDir()

	_, err := configloader.Load(
		configloader.Options[testConfig]{Name: name, SearchPaths: []string{dir}, Logger: logger},
	)
	require.NoError(t, err)
	assert.Equal(t, []record{{
		Level:   slog.LevelInfo,
		Message: "no config file found",
		Attrs: map[string]any{
			"searched": []string{
				filepath.Join(dir, "config.*"),
				".my-svc.*",
				filepath.Join(home, ".config", "my-svc", "config.*"),
				filepath.Join(home, ".my-svc", "config.*"),
				filepath.Join("/etc", "my-svc", "config.*"),
			},
		},
	}}, records.all())
}

func TestReportsAKeyFromTheEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PORT", "9191")

	records, logger := newCapture()

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Logger: logger})
	require.NoError(t, err)
	assert.Contains(t, records.all(), record{
		Level:   slog.LevelDebug,
		Message: "config key set from environment",
		Attrs:   map[string]any{"key": "http.port", "env": "MY_SVC_HTTP_PORT"},
	})
	assert.NotContains(t, records.rendered(), "9191")
}

func TestReportsAKeyFromASecretFile(t *testing.T) {
	isolate(t)

	records, logger := newCapture()
	path := writeFile(t, t.TempDir(), "password", "s3cret\n")
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", path)

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Logger: logger})
	require.NoError(t, err)
	assert.Contains(t, records.all(), record{
		Level:   slog.LevelDebug,
		Message: "config key read from file",
		Attrs:   map[string]any{"key": "db.password", "env": "MY_SVC_DB_PASSWORD_FILE", "path": path},
	})
	assert.NotContains(t, records.rendered(), "s3cret")
}

func TestReportsAnIgnoredUnknownKey(t *testing.T) {
	isolate(t)

	records, logger := newCapture()
	path := configFile(t, "htpp:\n  port: 9090\n")

	_, err := configloader.Load(configloader.Options[testConfig]{
		Name:             name,
		Filename:         path,
		AllowUnknownKeys: true,
		Logger:           logger,
	})
	require.NoError(t, err)
	assert.Contains(t, records.all(), record{
		Level:   slog.LevelWarn,
		Message: "ignoring unknown config key",
		Attrs:   map[string]any{"key": "htpp.port"},
	})
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
