package publish_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/publish"
)

func TestAnnounceEscapesThePath(t *testing.T) {
	var got string

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))

	defer srv.Close()

	p := publish.Proxy{URL: srv.URL + "/", Client: srv.Client()}
	require.NoError(t, p.Announce(t.Context(), "github.com/Sikhlana/pkg/http/server", "v0.2.0"))
	assert.Equal(t, "/github.com/!sikhlana/pkg/http/server/@v/v0.2.0.info", got)
}

func TestAnnounceReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found: unknown revision", http.StatusNotFound)
	}))

	defer srv.Close()

	err := publish.Proxy{URL: srv.URL, Client: srv.Client()}.Announce(t.Context(), "example.com/m", "v0.1.0")
	require.ErrorIs(t, err, publish.ErrProxyStatus)
	require.ErrorContains(t, err, "404 Not Found: not found: unknown revision")
}

func TestAnnounceTimesOut(t *testing.T) {
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))

	defer srv.Close()
	defer close(release)

	p := publish.Proxy{URL: srv.URL, Client: srv.Client(), Timeout: 50 * time.Millisecond}
	err := p.Announce(t.Context(), "example.com/m", "v0.1.0")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestAnnounceRejectsInvalidPath(t *testing.T) {
	err := publish.Proxy{URL: "http://127.0.0.1:0"}.Announce(t.Context(), "bad path", "v0.1.0")
	require.ErrorContains(t, err, "escaping module path")
}
