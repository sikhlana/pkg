package httpserver_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

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

func TestANilOptionIsSkipped(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	p := httpserver.NewProvider(httpserver.ProviderParams{Logger: slog.New(rec)})

	s, err := p(httpserver.Config{Host: "127.0.0.1"}, ok, nil, httpserver.WithName("admin"))
	require.NoError(t, err)

	require.NoError(t, s.Listen(t.Context()))
	t.Cleanup(func() { _ = s.Close() })

	listening, found := rec.find("http server listening")
	require.True(t, found)
	assert.Equal(t, "admin", listening.Attrs["server"])
}
