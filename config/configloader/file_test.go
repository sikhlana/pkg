package configloader_test

import (
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configloader"
)

func TestEnvNameOfANestedKey(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_READ_TIMEOUT", "30s")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, cfg.HTTP.ReadTimeout)
}

func TestNameIsChecked(t *testing.T) {
	for _, n := range []string{"", "../etc", "my svc", "my.svc"} {
		t.Run(n, func(t *testing.T) {
			_, err := configloader.Load(configloader.Options[testConfig]{Name: n})
			require.ErrorIs(t, err, configloader.ErrInvalidName)
		})
	}
}

func TestExplicitFileMissing(t *testing.T) {
	isolate(t)

	path := filepath.Join(t.TempDir(), "config.yaml")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.ErrorContains(t, err, path)
}

func TestExplicitFileMalformed(t *testing.T) {
	isolate(t)

	path := configFile(t, "http: [\n")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
	require.Error(t, err)
	assert.ErrorContains(t, err, path)
}

func TestExplicitFileUnsupportedExtension(t *testing.T) {
	isolate(t)

	path := writeFile(t, t.TempDir(), "config.xyz", "http.port = 9090\n")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
	require.Error(t, err)
	assert.ErrorContains(t, err, path)
}

func TestExplicitFileIsRead(t *testing.T) {
	isolate(t)

	path := configFile(t, "http:\n  port: 9090\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.HTTP.Port)
}

func TestSearchTriesExtraDirectoriesFirst(t *testing.T) {
	isolate(t)

	extra := t.TempDir()
	writeFile(t, extra, "config.toml", "[http]\nport = 1111\n")
	writeFile(t, ".", "config.toml", "[http]\nport = 2222\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name, SearchPaths: []string{extra}})
	require.NoError(t, err)
	assert.Equal(t, 1111, cfg.HTTP.Port)
}

func TestSearchFallsBackToTheWorkingDirectory(t *testing.T) {
	isolate(t)

	writeFile(t, ".", "config.toml", "[http]\nport = 2222\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name, SearchPaths: []string{t.TempDir()}})
	require.NoError(t, err)
	assert.Equal(t, 2222, cfg.HTTP.Port)
}

func TestSearchFindsTheHomeDirectory(t *testing.T) {
	isolate(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, home, ".my-svc/config.yaml", "http:\n  port: 3333\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, 3333, cfg.HTTP.Port)
}

func TestNoFileAnywhereLoadsFromTheEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PORT", "9191")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name, SearchPaths: []string{t.TempDir()}})
	require.NoError(t, err)
	assert.Equal(t, 9191, cfg.HTTP.Port)
}

func TestFoundFileMalformed(t *testing.T) {
	isolate(t)

	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", "http: [\n")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, SearchPaths: []string{dir}})
	require.Error(t, err)
	assert.ErrorContains(t, err, path)
}
