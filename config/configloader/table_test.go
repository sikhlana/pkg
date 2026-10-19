package configloader_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configloader"
)

func TestKeyBesideItsFileTwinCollides(t *testing.T) {
	isolate(t)

	type config struct {
		TLS struct {
			Key     string `mapstructure:"key"`
			KeyFile string `mapstructure:"key_file"`
		} `mapstructure:"tls"`
	}

	_, err := configloader.Load(configloader.Options[config]{Name: name})
	require.ErrorIs(t, err, configloader.ErrEnvNameCollision)
	assert.ErrorContains(t, err, "tls.key and tls.key_file both use MY_SVC_TLS_KEY_FILE")
}

func TestUnderscoreAmbiguityCollides(t *testing.T) {
	isolate(t)

	type config struct {
		HTTP struct {
			ReadTimeout string `mapstructure:"read_timeout"`
		} `mapstructure:"http"`
		HTTPRead struct {
			Timeout string `mapstructure:"timeout"`
		} `mapstructure:"http_read"`
	}

	_, err := configloader.Load(configloader.Options[config]{Name: name})
	require.ErrorIs(t, err, configloader.ErrEnvNameCollision)
	assert.ErrorContains(t, err, "http.read_timeout and http_read.timeout both use MY_SVC_HTTP_READ_TIMEOUT")
}

func TestMapEntriesThatNormaliseAlikeCollide(t *testing.T) {
	isolate(t)

	type config struct {
		Tenants map[string]struct {
			URL string `mapstructure:"url"`
		} `mapstructure:"tenants"`
	}

	path := configFile(t, "tenants:\n  acme-corp:\n    url: a\n  acme_corp:\n    url: b\n")

	_, err := configloader.Load(configloader.Options[config]{Name: name, Filename: path})
	require.ErrorIs(t, err, configloader.ErrEnvNameCollision)
	assert.ErrorContains(
		t,
		err,
		"tenants.acme-corp.url and tenants.acme_corp.url both use MY_SVC_TENANTS_ACME_CORP_URL",
	)
}

func TestMapEntriesFromDefaultsCollide(t *testing.T) {
	isolate(t)

	_, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Defaults: testConfig{Labels: map[string]string{"a-b": "x", "a_b": "y"}},
	})
	require.ErrorIs(t, err, configloader.ErrEnvNameCollision)
	assert.ErrorContains(t, err, "labels.a-b and labels.a_b")
}

func TestUnknownKeyFailsTheLoad(t *testing.T) {
	isolate(t)

	path := configFile(t, "htpp:\n  port: 9090\nhttp:\n  prot: 1\n  port: 9090\n")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
	require.ErrorIs(t, err, configloader.ErrUnknownKeys)
	assert.ErrorContains(t, err, "unknown config keys: htpp.port, http.prot")
}

func TestUnknownKeyBeneathALeafFailsTheLoad(t *testing.T) {
	isolate(t)

	path := configFile(t, "http:\n  port:\n    number: 9090\n")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
	require.ErrorIs(t, err, configloader.ErrUnknownKeys)
	assert.ErrorContains(t, err, "http.port.number")
}

func TestUnknownKeysAllowed(t *testing.T) {
	isolate(t)

	path := configFile(t, "htpp:\n  port: 1\nhttp:\n  port: 9090\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path, AllowUnknownKeys: true})
	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.HTTP.Port)
}

func TestMapEntriesNeverCountAsUnknown(t *testing.T) {
	isolate(t)

	path := configFile(t, "labels:\n  team: core\n  anything: goes\ndatabases:\n  reporting:\n    host: db\n")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name, Filename: path})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"team": "core", "anything": "goes"}, cfg.Labels)
	assert.Equal(t, map[string]DBConfig{"reporting": {Host: "db"}}, cfg.Databases)
}

func TestNestedContentUnderAnInterfaceLeafIsKnown(t *testing.T) {
	isolate(t)

	type config struct {
		Extra any `mapstructure:"extra"`
	}

	path := configFile(t, "extra:\n  a:\n    b: 1\n")

	cfg, err := configloader.Load(configloader.Options[config]{Name: name, Filename: path})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": map[string]any{"b": 1}}, cfg.Extra)
}

func TestARemainFieldAcceptsAnyKeyAtItsLevel(t *testing.T) {
	isolate(t)

	type plugin struct {
		Name  string         `mapstructure:"name"`
		Extra map[string]any `mapstructure:",remain"`
	}

	type config struct {
		Plugin plugin `mapstructure:"plugin"`
		Port   int    `mapstructure:"port"`
	}

	path := configFile(t, "plugin:\n  name: x\n  colour: red\n  deep:\n    key: 1\n")

	cfg, err := configloader.Load(configloader.Options[config]{Name: name, Filename: path})
	require.NoError(t, err)
	assert.Equal(t, "x", cfg.Plugin.Name)
	assert.Equal(t, "red", cfg.Plugin.Extra["colour"])

	path = configFile(t, "colour: red\n")

	_, err = configloader.Load(configloader.Options[config]{Name: name, Filename: path})
	require.ErrorIs(t, err, configloader.ErrUnknownKeys)
}

func TestEnvNameReplacesCharactersBeforeUppercasing(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_LABELS_STRA_E", "from-env")

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "labels:\n  straße: from-file\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"straße": "from-env"}, cfg.Labels)
}

type Node struct {
	Name string `mapstructure:"name"`
	Next *Node  `mapstructure:"next"`
}

func TestSelfReferentialTypeLoads(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_NAME", "head")

	cfg, err := configloader.Load(configloader.Options[Node]{
		Name:     name,
		Filename: configFile(t, "next:\n  name: second\n  next:\n    name: third\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "head", cfg.Name)
	require.NotNil(t, cfg.Next)
	require.NotNil(t, cfg.Next.Next)
	assert.Equal(t, "third", cfg.Next.Next.Name)
}

type inner struct {
	Host string `mapstructure:"host"`
}

func TestUnexportedSquashedPointerIsRejected(t *testing.T) {
	isolate(t)

	type config struct {
		*inner `mapstructure:",squash"`
	}

	_, err := configloader.Load(configloader.Options[config]{Name: name})
	require.ErrorContains(t, err, "field inner")
	assert.ErrorContains(t, err, "unexported embedded pointer")
}

func TestDefaultEntriesDifferingByCaseCollide(t *testing.T) {
	isolate(t)

	_, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Defaults: testConfig{Labels: map[string]string{"A": "upper", "a": "lower"}},
	})
	require.ErrorIs(t, err, configloader.ErrEnvNameCollision)
	assert.ErrorContains(t, err, "default entries labels.A and labels.a both load as labels.a")
}

func TestDefaultEntryNamesMustBeLoadable(t *testing.T) {
	for _, entry := range []string{"", "example.com"} {
		t.Run(entry, func(t *testing.T) {
			isolate(t)

			_, err := configloader.Load(configloader.Options[testConfig]{
				Name:     name,
				Defaults: testConfig{Labels: map[string]string{entry: "x"}},
			})
			require.ErrorContains(t, err, "an entry name can't be empty or hold a dot")
		})
	}
}

type textKey string

func (k textKey) String() string { return "display-" + string(k) }

func (k textKey) MarshalText() ([]byte, error) { return []byte(k), nil }

func TestDefaultEntryIsNamedByItsTextForm(t *testing.T) {
	isolate(t)

	type config struct {
		Limits map[textKey]time.Duration `mapstructure:"limits"`
	}

	t.Setenv("MY_SVC_LIMITS_READ", "2s")

	cfg, err := configloader.Load(configloader.Options[config]{
		Name:     name,
		Defaults: config{Limits: map[textKey]time.Duration{"read": time.Second, "write": time.Second}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[textKey]time.Duration{"read": 2 * time.Second, "write": time.Second}, cfg.Limits)
}

// labels is unexported, so a value reached through an embedded field of this type can't be read.
type labels map[string]string

func TestUnreadableDefaultFailsTheLoad(t *testing.T) {
	isolate(t)

	type config struct {
		labels `mapstructure:",squash"`
	}

	_, err := configloader.Load(
		configloader.Options[config]{Name: name, Defaults: config{labels: labels{"team": "core"}}},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "can't be read")
}

func TestDefaultEntryNamesLoadLowercased(t *testing.T) {
	isolate(t)

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Defaults: testConfig{Labels: map[string]string{"Team": "core"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"team": "core"}, cfg.Labels)
}

// env is a map key whose String method is for display, not the key a config file holds.
type env string

func (e env) String() string {
	return "ENV(" + string(e) + ")"
}

func TestDefaultEntryIsNamedByItsKeyNotItsDisplay(t *testing.T) {
	isolate(t)

	type config struct {
		DBs map[env]DBConfig `mapstructure:"dbs"`
	}

	t.Setenv("MY_SVC_DBS_PROD_HOST", "override.example.com")

	cfg, err := configloader.Load(configloader.Options[config]{
		Name:     name,
		Defaults: config{DBs: map[env]DBConfig{"prod": {Host: "db.example.com"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "override.example.com", cfg.DBs["prod"].Host)
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

func TestEnvNameOfANestedKey(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_READ_TIMEOUT", "30s")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, cfg.HTTP.ReadTimeout)
}
