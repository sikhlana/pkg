package httpserver_test

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/config/configloader"
	"github.com/sikhlana/pkg/config/configvalidator"
	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

func TestConfigZeroTimeoutsUseTheDefaults(t *testing.T) {
	t.Parallel()

	s := build(t, httpserver.Config{Host: "127.0.0.1"}, ok, nil)
	srv := httpserver.HTTPServer(t.Context(), s)

	assert.Equal(t, 5*time.Second, srv.ReadTimeout)
	assert.Equal(t, 5*time.Second, srv.ReadHeaderTimeout)
	assert.Equal(t, 10*time.Second, srv.WriteTimeout)
	assert.Equal(t, 60*time.Second, srv.IdleTimeout)
	assert.Equal(t, 10*time.Second, httpserver.ShutdownTimeout(s))
}

func TestConfigSetTimeoutsWin(t *testing.T) {
	t.Parallel()

	s := build(t, httpserver.Config{
		Host:              "127.0.0.1",
		ReadTimeout:       time.Minute,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}, ok, nil)
	srv := httpserver.HTTPServer(t.Context(), s)

	assert.Equal(t, time.Minute, srv.ReadTimeout)
	assert.Equal(t, 2*time.Second, srv.ReadHeaderTimeout)
	assert.Equal(t, 3*time.Second, srv.WriteTimeout)
	assert.Equal(t, 4*time.Second, srv.IdleTimeout)
	assert.Equal(t, 5*time.Second, httpserver.ShutdownTimeout(s))
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  httpserver.Config
		want error
		key  string
	}{
		{name: "zero value", cfg: httpserver.Config{}},
		{name: "host and port", cfg: httpserver.Config{Host: "127.0.0.1", Port: 8080}},
		{
			name: "negative read_timeout",
			cfg:  httpserver.Config{Port: 8080, ReadTimeout: -time.Second},
			want: httpserver.ErrNegativeTimeout,
			key:  "read_timeout",
		},
		{
			name: "negative read_header_timeout",
			cfg:  httpserver.Config{Port: 8080, ReadHeaderTimeout: -time.Second},
			want: httpserver.ErrNegativeTimeout,
			key:  "read_header_timeout",
		},
		{
			name: "negative write_timeout",
			cfg:  httpserver.Config{Port: 8080, WriteTimeout: -time.Second},
			want: httpserver.ErrNegativeTimeout,
			key:  "write_timeout",
		},
		{
			name: "negative idle_timeout",
			cfg:  httpserver.Config{Port: 8080, IdleTimeout: -time.Second},
			want: httpserver.ErrNegativeTimeout,
			key:  "idle_timeout",
		},
		{
			name: "negative shutdown_timeout",
			cfg:  httpserver.Config{Port: 8080, ShutdownTimeout: -time.Second},
			want: httpserver.ErrNegativeTimeout,
			key:  "shutdown_timeout",
		},
		{
			name: "tls without a key file",
			cfg: httpserver.Config{
				Port: 8443,
				TLS:  tlsconfig.ServerConfig{Enabled: true, CertFile: "server.crt"},
			},
			want: tlsconfig.ErrMissingKeyPair,
			key:  "tls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()

			if tt.want == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.want)

			violation, found := errors.AsType[*configvalidator.Error](err)
			require.True(t, found)
			assert.Equal(t, tt.key, violation.Key)
		})
	}
}

func TestConfigValidateReportsEveryNegativeTimeout(t *testing.T) {
	t.Parallel()

	err := httpserver.Config{
		Port:            8080,
		ReadTimeout:     -time.Second,
		ShutdownTimeout: -time.Second,
	}.Validate()

	require.ErrorIs(t, err, httpserver.ErrNegativeTimeout)
	assert.Contains(t, err.Error(), "read_timeout")
	assert.Contains(t, err.Error(), "shutdown_timeout")
}

// app is an application config holding one server's section, as configloader loads it.
type app struct {
	HTTP httpserver.Config `mapstructure:"http"`
}

// load loads an app from a YAML file holding contents.
func load(t *testing.T, contents string) (app, error) {
	t.Helper()

	file := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(file, []byte(contents), 0o600))

	return configloader.Load(configloader.Options[app]{
		Name:     "httpservertest",
		Filename: file,
		Logger:   slog.New(slog.DiscardHandler),
	})
}

func TestConfigLoadsThroughConfigloader(t *testing.T) {
	t.Parallel()

	cfg, err := load(t, "http:\n  host: 127.0.0.1\n  port: 8080\n  shutdown_timeout: 30s\n  tls:\n    enabled: false\n")
	require.NoError(t, err)

	assert.Equal(t, httpserver.Config{Host: "127.0.0.1", Port: 8080, ShutdownTimeout: 30 * time.Second}, cfg.HTTP)
}

func TestConfigLoadFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
		key      string
		want     error
	}{
		{name: "malformed host", contents: "http:\n  host: \"foo bar\"\n", key: "http.host"},
		{name: "host with a port", contents: "http:\n  host: \"localhost:80\"\n", key: "http.host"},
		{name: "port too large", contents: "http:\n  port: 65536\n", key: "http.port"},
		{name: "negative port", contents: "http:\n  port: -1\n", key: "http.port"},
		{
			name:     "negative shutdown_timeout",
			contents: "http:\n  shutdown_timeout: -1s\n",
			key:      "http.shutdown_timeout",
			want:     httpserver.ErrNegativeTimeout,
		},
		{
			name:     "tls without a key file",
			contents: "http:\n  tls:\n    enabled: true\n    cert_file: server.crt\n",
			key:      "http.tls",
			want:     tlsconfig.ErrMissingKeyPair,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := load(t, tt.contents)
			require.Error(t, err)

			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			}

			violation, found := errors.AsType[*configvalidator.Error](err)
			require.True(t, found)
			assert.Equal(t, tt.key, violation.Key)
		})
	}
}

// apiConfig is a module's own config embedding a server's, as the README's fx example does.
type apiConfig struct {
	httpserver.Config `mapstructure:",squash"`

	MaxUploadSize int64 `mapstructure:"max_upload_size" validate:"gte=0"`
}

// loadAPI loads an apiConfig under api from a YAML file holding contents.
func loadAPI(t *testing.T, contents string) (apiConfig, error) {
	t.Helper()

	file := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(file, []byte(contents), 0o600))

	type root struct {
		API apiConfig `mapstructure:"api"`
	}

	cfg, err := configloader.Load(configloader.Options[root]{
		Name:     "httpservertest",
		Filename: file,
		Logger:   slog.New(slog.DiscardHandler),
	})

	return cfg.API, err
}

func TestSquashedConfigLoads(t *testing.T) {
	t.Parallel()

	cfg, err := loadAPI(t, "api:\n  port: 8443\n  shutdown_timeout: 20s\n  max_upload_size: 1024\n")
	require.NoError(t, err)

	assert.Equal(t, apiConfig{
		Port: 8443, ShutdownTimeout: 20 * time.Second,
		MaxUploadSize: 1024,
	}, cfg)
}

func TestSquashedConfigValidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
		key      string
	}{
		{name: "the server's Validate", contents: "api:\n  shutdown_timeout: -1s\n", key: "api.shutdown_timeout"},
		{name: "the server's tags", contents: "api:\n  port: 65536\n", key: "api.port"},
		{name: "the module's own tags", contents: "api:\n  max_upload_size: -1\n", key: "api.max_upload_size"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadAPI(t, tt.contents)

			violation, found := errors.AsType[*configvalidator.Error](err)
			require.True(t, found, "got %v", err)
			assert.Equal(t, tt.key, violation.Key)
		})
	}
}

// errStorage is what storageConfig's Validate reports.
var errStorage = errors.New("storage is broken")

// storageConfig is a section with its own Validate method, beside a squashed server config.
type storageConfig struct {
	Bucket string `mapstructure:"bucket"`
}

func (s storageConfig) Validate() error {
	if s.Bucket == "broken" {
		return errStorage
	}

	return nil
}

// storedAPIConfig squashes a server's config beside another Validator, so it checks both itself.
type storedAPIConfig struct {
	httpserver.Config `mapstructure:",squash"`

	Storage storageConfig `mapstructure:"storage"`
}

func (c storedAPIConfig) Validate() error {
	return errors.Join(
		configvalidator.ValidateField(&c, &c.Config),
		configvalidator.ValidateField(&c, &c.Storage),
	)
}

func TestSquashedConfigWithItsOwnValidate(t *testing.T) {
	t.Parallel()

	type root struct {
		API storedAPIConfig `mapstructure:"api"`
	}

	file := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(
		t,
		os.WriteFile(file, []byte("api:\n  shutdown_timeout: -1s\n  storage:\n    bucket: broken\n"), 0o600),
	)

	_, err := configloader.Load(configloader.Options[root]{
		Name:     "httpservertest",
		Filename: file,
		Logger:   slog.New(slog.DiscardHandler),
	})

	var keys []string

	violations, found := errors.AsType[configvalidator.Errors](err)
	require.True(t, found, "got %v", err)

	for _, v := range violations {
		violation, isViolation := errors.AsType[*configvalidator.Error](v)
		require.True(t, isViolation)

		keys = append(keys, violation.Key)
	}

	assert.ElementsMatch(t, []string{"api.shutdown_timeout", "api.storage"}, keys)
	require.ErrorIs(t, err, httpserver.ErrNegativeTimeout)
	require.ErrorIs(t, err, errStorage)
}
