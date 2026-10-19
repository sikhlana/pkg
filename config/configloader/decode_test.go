package configloader_test

import (
	"log/slog"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configloader"
)

func TestDurationDecodes(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_READ_TIMEOUT", "30s")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, cfg.HTTP.ReadTimeout)
}

func TestCommaSeparatedSliceDecodes(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HOSTS", "a,b")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, cfg.Hosts)
}

func TestTextDecodedTypesDecodeFromTheFile(t *testing.T) {
	isolate(t)

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "log:\n  level: debug\ntrusted: 10.0.0.0/8\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, slog.LevelDebug, cfg.Log.Level)
	assert.Equal(t, netip.MustParsePrefix("10.0.0.0/8"), cfg.Trusted)
}

func TestNetIPDecodesFromTheEnvironment(t *testing.T) {
	isolate(t)

	type config struct {
		IP net.IP `mapstructure:"ip"`
	}

	t.Setenv("MY_SVC_IP", "10.0.0.1")

	cfg, err := configloader.Load(configloader.Options[config]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, net.ParseIP("10.0.0.1"), cfg.IP)
}

func TestBadNumberNamesTheKeyAndTypeButNotTheValue(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_PORT", "hunter2")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.Error(t, err)
	require.ErrorContains(t, err, "decoding config")
	require.ErrorContains(t, err, "http.port")
	require.ErrorContains(t, err, "int")
	assert.NotContains(t, err.Error(), "hunter2")
	assert.Zero(t, cfg)
}

func TestBadTextDecodedValueNamesTheKeyButNotTheValue(t *testing.T) {
	isolate(t)

	cfg, err := configloader.Load(configloader.Options[testConfig]{
		Name:     name,
		Filename: configFile(t, "trusted: hunter2\nhttp:\n  port: 9090\n"),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "trusted")
	require.ErrorContains(t, err, "netip.Prefix")
	assert.NotContains(t, err.Error(), "hunter2")
	assert.Zero(t, cfg)
}

func TestBadDurationDoesntLeakTheValue(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HTTP_READ_TIMEOUT", "hunter two")

	_, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.ErrorContains(t, err, "http.read_timeout")
	require.ErrorContains(t, err, "time.Duration")
	assert.NotContains(t, err.Error(), "hunter")
}

func TestByteSliceLoadsFromAString(t *testing.T) {
	isolate(t)

	type config struct {
		Key  []byte `mapstructure:"key"`
		Seed []byte `mapstructure:"seed"`
	}

	t.Setenv("MY_SVC_KEY", "abc,def")
	t.Setenv("MY_SVC_SEED_FILE", writeFile(t, t.TempDir(), "seed", "s3cret\n"))

	cfg, err := configloader.Load(configloader.Options[config]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, []byte("abc,def"), cfg.Key)
	assert.Equal(t, []byte("s3cret"), cfg.Seed)
}

func TestCommaSeparatedSliceIsTrimmed(t *testing.T) {
	isolate(t)
	t.Setenv("MY_SVC_HOSTS", "a.example.com, b.example.com ,c.example.com")

	cfg, err := configloader.Load(configloader.Options[testConfig]{Name: name})
	require.NoError(t, err)
	assert.Equal(t, []string{"a.example.com", "b.example.com", "c.example.com"}, cfg.Hosts)
}
