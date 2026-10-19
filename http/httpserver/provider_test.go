package httpserver_test

import (
	"io/fs"
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
			name:    "nil pointer handler",
			cfg:     httpserver.Config{Host: "127.0.0.1"},
			handler: (*pointerHandler)(nil),
			want:    httpserver.ErrNilHandler,
		},
		{
			name:    "nil func handler",
			cfg:     httpserver.Config{Host: "127.0.0.1"},
			handler: http.HandlerFunc(nil),
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
