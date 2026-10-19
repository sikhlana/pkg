package httpserver_test

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

func TestProviderFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pair := newAuthority(t).issue(t, dir)

	tests := []struct {
		name    string
		cfg     httpserver.Config
		handler http.Handler
		want    error
	}{
		{
			name:    "nil handler",
			cfg:     httpserver.Config{Host: "127.0.0.1"},
			handler: nil,
			want:    httpserver.ErrNilHandler,
		},
		{
			name:    "invalid config",
			cfg:     httpserver.Config{Host: "127.0.0.1", ShutdownTimeout: -time.Second},
			handler: ok,
			want:    httpserver.ErrNegativeTimeout,
		},
		{
			name: "missing key file",
			cfg: httpserver.Config{
				Host: "127.0.0.1",
				TLS: tlsconfig.ServerConfig{
					Enabled:  true,
					CertFile: pair.CertFile,
					KeyFile:  filepath.Join(dir, "missing.key"),
				},
			},
			handler: ok,
			want:    fs.ErrNotExist,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := httpserver.NewProvider(httpserver.ProviderParams{})(tt.cfg, tt.handler)

			require.ErrorIs(t, err, tt.want)
			assert.Nil(t, s)
		})
	}
}

func TestWithNameNamesAServerBuiltByHand(t *testing.T) {
	t.Parallel()

	pair := newAuthority(t).issue(t, t.TempDir())
	cfg := httpserver.Config{
		Host: "127.0.0.1",
		TLS:  tlsconfig.ServerConfig{Enabled: true, CertFile: pair.CertFile, KeyFile: pair.KeyFile},
	}

	tests := []struct {
		name  string
		opts  []httpserver.Option
		want  string
		named bool
	}{
		{name: "named", opts: []httpserver.Option{httpserver.WithName("admin")}, want: "admin", named: true},
		{name: "unnamed"},
		{name: "empty name", opts: []httpserver.Option{httpserver.WithName("")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := newRecorder()
			s := build(t, cfg, ok, slog.New(rec), tt.opts...)

			require.NoError(t, s.Listen(t.Context()))
			t.Cleanup(func() { _ = s.Close() })

			for _, message := range []string{"http server listening", "tls server key pair loaded"} {
				r, found := rec.find(message)
				require.True(t, found, message)

				name, named := r.Attrs["server"]
				assert.Equal(t, tt.named, named, message)
				assert.Equal(t, tt.want, name, message)
			}
		})
	}
}
