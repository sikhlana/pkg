package configloader_test

import (
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configloader"
)

func TestDefaultOnly(t *testing.T) {
	isolate(t)

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Defaults: testConfig{HTTP: HTTPConfig{Port: 8080}},
	})
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.HTTP.Port)
}

func TestFileOverDefault(t *testing.T) {
	isolate(t)

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "http:\n  port: 9090\n"),
		Defaults: testConfig{HTTP: HTTPConfig{Host: "localhost", Port: 8080}},
	})
	require.NoError(t, err)
	assert.Equal(t, HTTPConfig{Host: "localhost", Port: 9090}, cfg.HTTP)
}

func TestEnvironmentOverFile(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PORT", "9191")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "http:\n  port: 9090\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, 9191, cfg.HTTP.Port)
}

func TestSliceIsReplaced(t *testing.T) {
	isolate(t)

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "hosts: [x]\n"),
		Defaults: testConfig{Hosts: []string{"a", "b", "c"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"x"}, cfg.Hosts)
}

func TestMapEntriesMerge(t *testing.T) {
	isolate(t)

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "labels:\n  tier: gold\n  zone: eu\n"),
		Defaults: testConfig{Labels: map[string]string{"team": "core", "tier": "silver"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"team": "core", "tier": "gold", "zone": "eu"}, cfg.Labels)
}

func TestNilDefaultSectionStaysNil(t *testing.T) {
	isolate(t)

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Nil(t, cfg.Cache)
}

func TestNestedLeafWithNoDefaultOrFileEntry(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_CACHE_TTL", "60s")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	require.NotNil(t, cfg.Cache)
	assert.Equal(t, time.Minute, cfg.Cache.TTL)
}

func TestSquashedLeafBindsAtTheParentsKey(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_BROADCAST_HTTP_HOST", "example.com")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, "example.com", cfg.Broadcast.HTTP.Host)
}

func TestTextDecodedLeavesFromTheEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_CUTOFF", "2026-01-02T03:04:05Z")
	t.Setenv("MY_SVC_TRUSTED", "10.0.0.0/8")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), cfg.Cutoff)
	assert.Equal(t, netip.MustParsePrefix("10.0.0.0/8"), cfg.Trusted)
}

func TestTextDecodedDefaultsSurvive(t *testing.T) {
	isolate(t)

	cutoff := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	trusted := netip.MustParsePrefix("10.0.0.0/8")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Defaults: testConfig{Cutoff: cutoff, Trusted: trusted},
	})
	require.NoError(t, err)
	assert.Equal(t, cutoff, cfg.Cutoff)
	assert.Equal(t, trusted, cfg.Trusted)
}

func TestMapEntryFromTheFileIsOverridden(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DATABASES_REPORTING_PASSWORD", "from-env")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "databases:\n  reporting:\n    host: db\n    password: from-file\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, DBConfig{Host: "db", Password: "from-env"}, cfg.Databases["reporting"])
}

func TestMapEntryGainsAFieldTheFileOmits(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DATABASES_REPORTING_PASSWORD", "from-env")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "databases:\n  reporting:\n    host: db\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, DBConfig{Host: "db", Password: "from-env"}, cfg.Databases["reporting"])
}

func TestMapEntryFromDefaultsIsOverridden(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_LABELS_TEAM", "platform")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Defaults: testConfig{Labels: map[string]string{"team": "core"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"team": "platform"}, cfg.Labels)
}

func TestMapEntryCantBeCreatedFromTheEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DATABASES_AUDIT_PASSWORD", "from-env")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "databases:\n  reporting:\n    host: db\n"),
	})
	require.NoError(t, err)
	assert.NotContains(t, cfg.Databases, "audit")
}

func TestSectionNamedVariableShadowsNothing(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP", "shadow")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "http:\n  port: 9090\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.HTTP.Port)
}

func TestUnknownPrefixedVariableChangesNothing(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PROT", "9191")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "http:\n  port: 9090\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, HTTPConfig{Port: 9090}, cfg.HTTP)
}

func TestFileVariableStripsTrailingNewlines(t *testing.T) {
	for _, tc := range []struct{ contents, want string }{
		{"s3cret\n", "s3cret"},
		{" pass word \r\n", " pass word "},
		{"a\n\n", "a"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			isolate(t)
			t.Setenv("MY_SVC_DB_PASSWORD_FILE", writeFile(t, t.TempDir(), "password", tc.contents))

			cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.DB.Password)
		})
	}
}

func TestFileVariableOverTheConfigFile(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", writeFile(t, t.TempDir(), "password", "from-secret\n"))

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "db:\n  password: from-file\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "from-secret", cfg.DB.Password)
}

func TestFileVariableForANonStringField(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PORT_FILE", writeFile(t, t.TempDir(), "port", "8080\n"))

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.HTTP.Port)
}

func TestVariableAndFileVariableBothSet(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DB_PASSWORD", "from-env")
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", writeFile(t, t.TempDir(), "password", "from-secret"))

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.ErrorIs(t, err, configloader.ErrEnvAndFileSet)
	require.ErrorContains(t, err, "MY_SVC_DB_PASSWORD and MY_SVC_DB_PASSWORD_FILE")
}

func TestEmptyFileVariableCountsAsUnset(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DB_PASSWORD", "from-env")
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", "")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, "from-env", cfg.DB.Password)
}

func TestFileVariableNamesAMissingFile(t *testing.T) {
	isolate(t)

	path := filepath.Join(t.TempDir(), "password")
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", path)

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorContains(t, err, "MY_SVC_DB_PASSWORD_FILE")
	require.ErrorContains(t, err, path)
}

func TestFileVariableNamesAnOversizedFile(t *testing.T) {
	isolate(t)

	planted := "hunter2"
	path := writeFile(t, t.TempDir(), "password", strings.Repeat(planted, (1<<20)/len(planted)+1))
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", path)

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.ErrorIs(t, err, configloader.ErrSecretFileTooLarge)
	require.ErrorContains(t, err, "MY_SVC_DB_PASSWORD_FILE")
	require.ErrorContains(t, err, path)
	assert.NotContains(t, err.Error(), planted)
}

func TestFileVariableOfExactlyTheLimitIsRead(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", writeFile(t, t.TempDir(), "password", strings.Repeat("a", 1<<20)))

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Len(t, cfg.DB.Password, 1<<20)
}

func TestSecretStaysOutOfTheEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_DB_PASSWORD_FILE", writeFile(t, t.TempDir(), "password", "s3cret\n"))

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)

	_, set := os.LookupEnv("MY_SVC_DB_PASSWORD")
	assert.False(t, set)
}

func TestNullSectionInTheFileDoesntHideTheEnvironment(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PORT", "9191")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "http:\nlabels:\n"),
		Defaults: testConfig{HTTP: HTTPConfig{Port: 8080}},
	})
	require.NoError(t, err)
	assert.Equal(t, 9191, cfg.HTTP.Port)
}

func TestLoadedConfigSharesNothingWithDefaults(t *testing.T) {
	isolate(t)

	type config struct {
		Extra any              `mapstructure:"extra"`
		Hosts []string         `mapstructure:"hosts"`
		Tags  map[string][]int `mapstructure:"tags"`
	}

	o := configloader.Options[config]{
		Name: name,
		Defaults: config{
			Extra: map[string]any{"a": []any{1}},
			Hosts: []string{"a.example.com"},
			Tags:  map[string][]int{"x": {1}},
		},
	}

	first, err := configloader.Load(o)
	require.NoError(t, err)

	first.Extra.(map[string]any)["a"].([]any)[0] = 99
	first.Hosts[0] = "changed.example.com"
	first.Tags["x"][0] = 99

	second, err := configloader.Load(o)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": []any{1}}, second.Extra)
	assert.Equal(t, []string{"a.example.com"}, second.Hosts)
	assert.Equal(t, map[string][]int{"x": {1}}, second.Tags)
	assert.Equal(t, map[string]any{"a": []any{1}}, o.Defaults.Extra)
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
