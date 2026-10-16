package configloader_test

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin library behaviour the loader's design relies on, so an upgrade that changes it fails here first.

func TestViperLowercasesMapKeys(t *testing.T) {
	v := readSpikeFile(t, "labels:\n  Team: core\n")

	var cfg struct {
		Labels map[string]string `mapstructure:"labels"`
	}

	require.NoError(t, v.Unmarshal(&cfg))
	assert.Equal(t, map[string]string{"team": "core"}, cfg.Labels)
	assert.Equal(t, []string{"labels.team"}, v.AllKeys())
}

func TestViperReplacesADefaultSliceWithTheFiles(t *testing.T) {
	v := readSpikeFile(t, "hosts: [x]\n")
	v.SetDefault("hosts", []string{"a", "b", "c"})

	var cfg struct {
		Hosts []string `mapstructure:"hosts"`
	}

	require.NoError(t, v.Unmarshal(&cfg))
	assert.Equal(t, []string{"x"}, cfg.Hosts)
}

func TestViperMergesMapEntriesAcrossDefaultsAndTheFile(t *testing.T) {
	v := readSpikeFile(t, "labels:\n  tier: gold\n  zone: eu\n")
	v.SetDefault("labels.team", "core")
	v.SetDefault("labels.tier", "silver")

	var cfg struct {
		Labels map[string]string `mapstructure:"labels"`
	}

	require.NoError(t, v.Unmarshal(&cfg))
	assert.Equal(t, map[string]string{"team": "core", "tier": "gold", "zone": "eu"}, cfg.Labels)
}

func TestMapstructureMergesIntoAPrefilledSliceByIndex(t *testing.T) {
	type host struct {
		Name string `mapstructure:"name"`
		Port int    `mapstructure:"port"`
	}

	cfg := struct {
		Hosts []host `mapstructure:"hosts"`
	}{Hosts: []host{{Name: "a", Port: 1}, {Name: "b", Port: 2}}}

	require.NoError(t, mapstructure.Decode(map[string]any{"hosts": []any{map[string]any{"name": "x"}}}, &cfg))
	assert.Equal(t, []host{{Name: "x", Port: 1}}, cfg.Hosts)
}

func TestViperKeepsTextDecodedDefaults(t *testing.T) {
	cutoff := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	trusted := netip.MustParsePrefix("10.0.0.0/8")

	v := viper.New()
	v.SetDefault("cutoff", cutoff)
	v.SetDefault("trusted", trusted)

	var cfg struct {
		Cutoff  time.Time    `mapstructure:"cutoff"`
		Trusted netip.Prefix `mapstructure:"trusted"`
	}

	require.NoError(t, v.Unmarshal(&cfg, spikeHooks(mapstructure.TextUnmarshallerHookFunc())))
	assert.Equal(t, cutoff, cfg.Cutoff)
	assert.Equal(t, trusted, cfg.Trusted)
}

func TestNetIPDecodesAsTextBeforeTheWeakSliceHook(t *testing.T) {
	v := readSpikeFile(t, "ip: 10.0.0.1\n")

	var cfg struct {
		IP net.IP `mapstructure:"ip"`
	}

	require.NoError(t, v.Unmarshal(&cfg, spikeHooks(
		mapstructure.TextUnmarshallerHookFunc(),
		mapstructure.StringToWeakSliceHookFunc(","),
	)))
	assert.Equal(t, net.ParseIP("10.0.0.1"), cfg.IP)
}

func TestViperBindEnvReadsTheNameVerbatim(t *testing.T) {
	t.Setenv("SPIKE_HTTP_PORT", "9191")
	t.Setenv("SPIKE_HTTP", "shadow")

	v := readSpikeFile(t, "http:\n  port: 9090\n  host: localhost\n")
	require.NoError(t, v.BindEnv("http.port", "SPIKE_HTTP_PORT"))

	var cfg struct {
		HTTP struct {
			Host string `mapstructure:"host"`
			Port int    `mapstructure:"port"`
		} `mapstructure:"http"`
	}

	require.NoError(t, v.Unmarshal(&cfg))
	assert.Equal(t, 9191, cfg.HTTP.Port)
	assert.Equal(t, "localhost", cfg.HTTP.Host)
}

func TestViperConfigFileUsedNamesTheFileTheSearchFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("port = 1\n"), 0o600))

	v := viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(dir)

	require.NoError(t, v.ReadInConfig())
	assert.Equal(t, path, v.ConfigFileUsed())
}

// readSpikeFile returns a fresh viper that has read contents as a YAML config file.
func readSpikeFile(t *testing.T, contents string) *viper.Viper {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	v := viper.New()
	v.SetConfigFile(path)
	require.NoError(t, v.ReadInConfig())

	return v
}

// spikeHooks returns a viper decoder option composing hooks.
func spikeHooks(hooks ...mapstructure.DecodeHookFunc) viper.DecoderConfigOption {
	return viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(hooks...))
}
