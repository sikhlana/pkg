package httpserver_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"io/fs"
	"log" //nolint:depguard // the test proves nothing reaches the standard library's log output
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

func TestBuiltServerIsNotListening(t *testing.T) {
	t.Parallel()

	s := build(t, httpserver.Config{Host: "127.0.0.1"}, ok, nil)

	assert.Nil(t, s.Addr())
	assert.NoError(t, s.Close())
}

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

func TestServeBeforeListen(t *testing.T) {
	t.Parallel()

	s := build(t, httpserver.Config{Host: "127.0.0.1"}, ok, nil)

	require.ErrorIs(t, s.Serve(t.Context()), httpserver.ErrNotListening)
	assert.Nil(t, s.Addr())
}

func TestListenTwice(t *testing.T) {
	t.Parallel()

	s := listening(t, httpserver.Config{}, ok)

	require.ErrorIs(t, s.Listen(t.Context()), httpserver.ErrAlreadyListening)

	ctx, cancel := context.WithCancel(t.Context())
	done := serving(ctx, s)

	status, body := get(t, s.Addr())
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "ok", body)

	cancel()
	require.NoError(t, waitFor(t, done))
}

func TestServeTwice(t *testing.T) {
	t.Parallel()

	s := listening(t, httpserver.Config{}, ok)

	ctx, cancel := context.WithCancel(t.Context())
	done := serving(ctx, s)

	// The first request proves the first Serve has taken over the listener.
	status, _ := get(t, s.Addr())
	require.Equal(t, http.StatusOK, status)

	require.ErrorIs(t, s.Serve(t.Context()), httpserver.ErrAlreadyListening)
	require.NoError(t, s.Close(), "a no-op while serving")

	status, _ = get(t, s.Addr())
	assert.Equal(t, http.StatusOK, status)

	cancel()
	require.NoError(t, waitFor(t, done))

	require.ErrorIs(t, s.Serve(t.Context()), httpserver.ErrNotListening, "not reusable once served")
}

func TestCloseReleasesAnUnservedListener(t *testing.T) {
	t.Parallel()

	s := listening(t, httpserver.Config{}, ok)
	addr := s.Addr().String()

	require.NoError(t, s.Close())
	require.NoError(t, s.Close(), "a second Close is a no-op")

	var lc net.ListenConfig

	l, err := lc.Listen(t.Context(), "tcp", addr)
	require.NoError(t, err, "the address is free again")
	require.NoError(t, l.Close())

	require.ErrorIs(t, s.Serve(t.Context()), httpserver.ErrNotListening)
	require.ErrorIs(t, s.Listen(t.Context()), httpserver.ErrAlreadyListening)
}

func TestListenOnATakenAddress(t *testing.T) {
	t.Parallel()

	taken := listening(t, httpserver.Config{}, ok)
	s := build(t, hostPort(t, taken.Addr()), ok, nil)

	err := s.Listen(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), taken.Addr().String())
	assert.Nil(t, s.Addr())
}

// blocking returns a handler that signals entered when a request arrives, then answers once release is closed or
// the request's context ends.
func blocking(entered chan<- struct{}, release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}

		select {
		case <-release:
			_, _ = io.WriteString(w, "done")
		case <-r.Context().Done():
		}
	})
}

func TestDrainFinishesInTime(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	s := listening(t, httpserver.Config{ShutdownTimeout: 5 * time.Second}, blocking(entered, release))
	addr := s.Addr()

	ctx, cancel := context.WithCancel(t.Context())
	done := serving(ctx, s)

	type result struct {
		status int
		body   string
		err    error
	}

	results := make(chan result, 1)

	go func() {
		status, body, err := fetch(t.Context(), addr)

		results <- result{status, body, err}
	}()

	<-entered
	cancel()

	assert.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr.String(), 100*time.Millisecond)
		if err != nil {
			return true
		}

		_ = conn.Close()

		return false
	}, 5*time.Second, 10*time.Millisecond, "new connections are refused while draining")

	close(release)

	got := <-results
	assert.Equal(t, result{http.StatusOK, "done", nil}, got)
	require.NoError(t, waitFor(t, done))
}

func TestDrainRunsOutOfTime(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})

	t.Cleanup(func() { close(release) })

	s := listening(t, httpserver.Config{ShutdownTimeout: 50 * time.Millisecond}, blocking(entered, release))
	addr := s.Addr()

	ctx, cancel := context.WithCancel(t.Context())
	done := serving(ctx, s)

	failed := make(chan error, 1)

	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr.String(), nil)
		if err != nil {
			failed <- err

			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}

		failed <- err
	}()

	<-entered
	cancel()

	require.ErrorIs(t, waitFor(t, done), context.DeadlineExceeded)
	assert.Error(t, waitFor(t, failed), "the connection was closed under the request")
}

func TestServeReturnsAListenerFailure(t *testing.T) {
	t.Parallel()

	s := listening(t, httpserver.Config{}, ok)
	done := serving(t.Context(), s)

	status, _ := get(t, s.Addr())
	require.Equal(t, http.StatusOK, status)

	require.NoError(t, httpserver.BreakListener(s))

	err := waitFor(t, done)
	require.ErrorIs(t, err, net.ErrClosed)
}

// tlsServer builds and binds a TLS server for pair on 127.0.0.1:0, serving until the end of the test.
func tlsServer(t *testing.T, pair keyPair, reload time.Duration, logger *slog.Logger) *httpserver.Server {
	t.Helper()

	s := build(t, httpserver.Config{
		Host: "127.0.0.1",
		TLS: tlsconfig.ServerConfig{
			Enabled:        true,
			CertFile:       pair.CertFile,
			KeyFile:        pair.KeyFile,
			ReloadInterval: reload,
		},
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}), logger)

	require.NoError(t, s.Listen(t.Context()))

	ctx, cancel := context.WithCancel(t.Context())
	done := serving(ctx, s)

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return s
}

// handshake dials the server at addr over TLS with cfg and returns the connection's state.
func handshake(t *testing.T, addr net.Addr, cfg *tls.Config) tls.ConnectionState {
	t.Helper()

	d := tls.Dialer{Config: cfg}

	conn, err := d.DialContext(t.Context(), "tcp", addr.String())
	require.NoError(t, err)

	defer conn.Close()

	tlsConn, isTLS := conn.(*tls.Conn)
	require.True(t, isTLS)

	return tlsConn.ConnectionState()
}

func TestTLSNegotiatesHTTP2(t *testing.T) {
	t.Parallel()

	ca := newAuthority(t)
	s := tlsServer(t, ca.issue(t, t.TempDir()), 0, nil)

	state := handshake(t, s.Addr(), ca.clientConfig("h2", "http/1.1"))

	assert.Equal(t, "h2", state.NegotiatedProtocol)
}

func TestTLSServesHTTP1(t *testing.T) {
	t.Parallel()

	ca := newAuthority(t)
	s := tlsServer(t, ca.issue(t, t.TempDir()), 0, nil)

	state := handshake(t, s.Addr(), ca.clientConfig("http/1.1"))
	assert.Equal(t, "http/1.1", state.NegotiatedProtocol)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: ca.clientConfig("http/1.1")}}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+s.Addr().String(), nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "HTTP/1.1", string(body))
}

func TestTLSPicksUpARotatedKeyPair(t *testing.T) {
	t.Parallel()

	ca := newAuthority(t)
	dir := t.TempDir()
	first := ca.issue(t, dir)
	s := tlsServer(t, first, 10*time.Millisecond, nil)

	assert.Equal(t, first.Cert.SerialNumber, handshake(t, s.Addr(), ca.clientConfig()).PeerCertificates[0].SerialNumber)

	second := ca.issue(t, t.TempDir())

	for _, f := range [][2]string{{second.CertFile, first.CertFile}, {second.KeyFile, first.KeyFile}} {
		contents, err := os.ReadFile(f[0])
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(f[1], contents, 0o600))
	}

	assert.Eventually(t, func() bool {
		serial := handshake(t, s.Addr(), ca.clientConfig()).PeerCertificates[0].SerialNumber

		return serial.Cmp(second.Cert.SerialNumber) == 0
	}, 5*time.Second, 20*time.Millisecond)
}

func TestPlaintextServesHTTP1Only(t *testing.T) {
	t.Parallel()

	s := listening(t, httpserver.Config{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Proto)
	}))

	ctx, cancel := context.WithCancel(t.Context())
	done := serving(ctx, s)

	_, body := get(t, s.Addr())
	assert.Equal(t, "HTTP/1.1", body)

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)

	h2c := &http.Client{Transport: &http.Transport{Protocols: protocols}}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+s.Addr().String(), nil)
	require.NoError(t, err)

	resp, err := h2c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}

	require.Error(t, err, "cleartext HTTP/2 isn't served")

	cancel()
	require.NoError(t, waitFor(t, done))
}

func TestLogsTheLifecycle(t *testing.T) {
	t.Parallel()

	rec := newRecorder()
	s := build(t, httpserver.Config{Host: "127.0.0.1"}, ok, slog.New(rec))

	require.NoError(t, s.Listen(t.Context()))

	ctx, cancel := context.WithCancel(t.Context())
	done := serving(ctx, s)

	cancel()
	require.NoError(t, waitFor(t, done))

	listening, found := rec.find("http server listening")
	require.True(t, found)
	assert.Equal(t, slog.LevelInfo, listening.Level)
	assert.Equal(t, s.Addr().String(), listening.Attrs["address"])
	assert.NotEqual(t, "127.0.0.1:0", listening.Attrs["address"], "the real port")

	draining, found := rec.find("http server draining")
	require.True(t, found)
	assert.Equal(t, slog.LevelInfo, draining.Level)

	stopped, found := rec.find("http server stopped")
	require.True(t, found)
	assert.Equal(t, slog.LevelDebug, stopped.Level)
}

// TestLogsAFailedHandshakeAtDebug swaps the standard library's log output, so it doesn't run in parallel.
func TestLogsAFailedHandshakeAtDebug(t *testing.T) {
	var stdlog bytes.Buffer

	prev := log.Writer()

	log.SetOutput(&stdlog)
	t.Cleanup(func() { log.SetOutput(prev) })

	rec := newRecorder()
	ca := newAuthority(t)
	s := tlsServer(t, ca.issue(t, t.TempDir()), 0, slog.New(rec))

	var d net.Dialer

	conn, err := d.DialContext(t.Context(), "tcp", s.Addr().String())
	require.NoError(t, err)

	_, err = io.WriteString(conn, "hello, server\r\n\r\n")
	require.NoError(t, err)

	_, _ = io.Copy(io.Discard, conn)
	require.NoError(t, conn.Close())

	assert.Eventually(t, func() bool {
		for _, r := range rec.all() {
			if r.Level == slog.LevelDebug && strings.Contains(r.Message, "TLS handshake error") {
				return true
			}
		}

		return false
	}, 5*time.Second, 10*time.Millisecond)

	assert.Empty(t, stdlog.String())
}
