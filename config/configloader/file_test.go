package configloader_test

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configloader"
)

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
	writeFile(t, ".", ".my-svc.toml", "[http]\nport = 2222\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name, SearchPaths: []string{extra}})
	require.NoError(t, err)
	assert.Equal(t, 1111, cfg.HTTP.Port)
}

func TestSearchFindsTheWorkingDirectorysAppNamedFile(t *testing.T) {
	isolate(t)

	writeFile(t, ".", ".my-svc.toml", "[http]\nport = 2222\n")

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

func TestSearchIgnoresAGenericFileInTheWorkingDirectory(t *testing.T) {
	isolate(t)

	writeFile(t, ".", "config.toml", "[http]\nport = 2222\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Zero(t, cfg.HTTP.Port)
}

func TestSearchFindsTheUserConfigDirectory(t *testing.T) {
	isolate(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, home, ".config/my-svc/config.yaml", "http:\n  port: 4444\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, 4444, cfg.HTTP.Port)
}

func TestSearchPrefersTheMostSpecificPlace(t *testing.T) {
	isolate(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	local := writeFile(t, ".", ".my-svc.yaml", "http:\n  port: 1111\n")
	user := writeFile(t, home, ".config/my-svc/config.yaml", "http:\n  port: 2222\n")
	writeFile(t, home, ".my-svc/config.yaml", "http:\n  port: 3333\n")

	for _, want := range []int{1111, 2222, 3333} {
		cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
		require.NoError(t, err)
		assert.Equal(t, want, cfg.HTTP.Port)

		switch want {
		case 1111:
			require.NoError(t, os.Remove(local))
		case 2222:
			require.NoError(t, os.Remove(user))
		}
	}
}

func TestSearchOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	assert.Equal(t, []string{
		filepath.Join("/srv/extra", "config.*"),
		".my-svc.*",
		filepath.Join(home, ".config", "my-svc", "config.*"),
		filepath.Join(home, ".my-svc", "config.*"),
		filepath.Join("/etc", "my-svc", "config.*"),
	}, configloader.SearchPatterns(name, []string{"/srv/extra"}))
}

func TestSearchSkipsFormatsWithoutADecoder(t *testing.T) {
	isolate(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, home, ".config/my-svc/config.ini", "[http]\nport = 1\n")
	writeFile(t, home, ".my-svc/config.yaml", "http:\n  port: 3333\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, 3333, cfg.HTTP.Port)
}

func TestExplicitFileIsTheOnlyFileRead(t *testing.T) {
	isolate(t)

	writeFile(t, ".", ".my-svc.yaml", "http:\n  host: local.example.com\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "http:\n  port: 1111\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, 1111, cfg.HTTP.Port)
	assert.Empty(t, cfg.HTTP.Host)
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

func TestMalformedFileIsNeverQuoted(t *testing.T) {
	tests := []struct {
		file string
		body string
		want string
	}{
		{
			file: "config.json",
			body: "{\n  \"db\": {\"password\": \"hunter2\" x}\n}\n",
			want: "json syntax error at line 2, column",
		},
		{file: "config.toml", body: "[db]\npassword = \"hunter2\" x\n", want: "toml syntax error at line 2, column"},
		{file: "config.yaml", body: "db:\n  password: hunter2\n bad: [\n", want: "yaml syntax error at line 2"},
		{file: "config.env", body: "DB_PASSWORD hunter2\n", want: "env syntax error"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			isolate(t)

			path := writeFile(t, t.TempDir(), tt.file, tt.body)

			_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
			require.ErrorIs(t, err, configloader.ErrMalformedFile)
			require.ErrorContains(t, err, path)
			require.ErrorContains(t, err, tt.want)
			assert.NotContains(t, err.Error(), "hunter2")
		})
	}
}

func TestFileWithoutADecoder(t *testing.T) {
	isolate(t)

	path := writeFile(t, t.TempDir(), "config.ini", "[db]\npassword = hunter2\n")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
	require.Error(t, err)
	require.NotErrorIs(t, err, configloader.ErrMalformedFile)
	require.ErrorContains(t, err, path)
	require.ErrorContains(t, err, "no decoder for the ini format")
	assert.NotContains(t, err.Error(), "hunter2")
}

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
