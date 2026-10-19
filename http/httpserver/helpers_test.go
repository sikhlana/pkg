package httpserver_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.uber.org/fx"

	"github.com/sikhlana/pkg/http/httpserver"
	"github.com/sikhlana/pkg/kernelrun"
)

// authority is a private CA for TLS tests.
type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

// keyPair is a server certificate an authority issued, written as PEM to CertFile and KeyFile.
type keyPair struct {
	CertFile string
	KeyFile  string
	Cert     *x509.Certificate
}

// newAuthority returns a self-signed CA.
func newAuthority(t *testing.T) authority {
	t.Helper()

	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: "httpserver test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return authority{cert: cert, key: key, pool: pool}
}

// issue writes a server key pair, valid for 127.0.0.1, into dir as server.crt and server.key.
func (a authority) issue(t *testing.T, dir string) keyPair {
	t.Helper()

	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: "server"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	pair := keyPair{
		CertFile: filepath.Join(dir, "server.crt"),
		KeyFile:  filepath.Join(dir, "server.key"),
		Cert:     cert,
	}

	writePEM(t, pair.CertFile, "CERTIFICATE", der)
	writePEM(t, pair.KeyFile, "PRIVATE KEY", keyDER)

	return pair
}

// clientConfig returns a client TLS configuration trusting only the authority and offering protos through ALPN.
func (a authority) clientConfig(protos ...string) *tls.Config {
	return &tls.Config{RootCAs: a.pool, NextProtos: protos, MinVersion: tls.VersionTLS12}
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	return key
}

func serial(t *testing.T) *big.Int {
	t.Helper()

	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	require.NoError(t, err)

	return n
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600))
}

// recorder is a slog handler that keeps every record it's handed, at every level, with the attributes added through
// WithAttrs.
type recorder struct {
	mu      *sync.Mutex
	records *[]record
	attrs   []slog.Attr
}

// record is a record a recorder was handed, with each attribute's value as a string.
type record struct {
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

func newRecorder() *recorder {
	return &recorder{mu: &sync.Mutex{}, records: &[]record{}}
}

func (*recorder) Enabled(context.Context, slog.Level) bool {
	return true
}

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	attrs := make(map[string]string)

	for _, a := range r.attrs {
		attrs[a.Key] = a.Value.String()
	}

	rec.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()

		return true
	})

	r.mu.Lock()
	defer r.mu.Unlock()

	*r.records = append(*r.records, record{Level: rec.Level, Message: rec.Message, Attrs: attrs})

	return nil
}

func (r *recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &recorder{mu: r.mu, records: r.records, attrs: slices.Concat(r.attrs, attrs)}
}

func (r *recorder) WithGroup(string) slog.Handler {
	return r
}

// all returns every record handed to the recorder, or any handler derived from it, so far.
func (r *recorder) all() []record {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(*r.records)
}

// find returns the first record with message, and whether there is one.
func (r *recorder) find(message string) (record, bool) {
	for _, rec := range r.all() {
		if rec.Message == message {
			return rec, true
		}
	}

	return record{}, false
}

// ok is a handler that answers "ok".
var ok = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "ok")
})

// build builds a server for cfg through a Provider logging to logger, with opts.
func build(
	t *testing.T,
	cfg httpserver.Config,
	h http.Handler,
	logger *slog.Logger,
	opts ...httpserver.Option,
) *httpserver.Server {
	t.Helper()

	s, err := httpserver.NewProvider(httpserver.ProviderParams{Logger: logger})(cfg, h, opts...)
	require.NoError(t, err)

	return s
}

// listening builds a plaintext server for h, on 127.0.0.1 unless cfg sets a host, and binds it, closing it at the
// end of the test.
func listening(t *testing.T, cfg httpserver.Config, h http.Handler) *httpserver.Server {
	t.Helper()

	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}

	s := build(t, cfg, h, slog.New(slog.DiscardHandler))

	require.NoError(t, s.Listen(t.Context()))
	t.Cleanup(func() { _ = s.Close() })

	return s
}

// hostPort returns a Config listening on addr's host and port.
func hostPort(t *testing.T, addr net.Addr) httpserver.Config {
	t.Helper()

	tcp, isTCP := addr.(*net.TCPAddr)
	require.True(t, isTCP)

	return httpserver.Config{Host: tcp.IP.String(), Port: tcp.Port}
}

// serving runs s.Serve on ctx in a goroutine and returns a channel that receives its result.
func serving(ctx context.Context, s *httpserver.Server) <-chan error {
	done := make(chan error, 1)

	go func() { done <- s.Serve(ctx) }()

	return done
}

// get requests / from the server at addr over plaintext and returns the status code and body.
func get(t *testing.T, addr net.Addr) (int, string) {
	t.Helper()

	status, body, err := fetch(t.Context(), addr)
	require.NoError(t, err)

	return status, body
}

// fetch requests / from the server at addr over plaintext and returns the status code and body. It asserts nothing,
// so goroutines other than the test's, such as kernelrun's runners, can call it.
func fetch(ctx context.Context, addr net.Addr) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr.String()+"/", nil)
	if err != nil {
		return 0, "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)

	return resp.StatusCode, string(body), err
}

// waitFor returns what done receives, failing the test if nothing arrives within a few seconds.
func waitFor(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting")

		return nil
	}
}

// configs holds each test server's Config by name, supplied whole so constructors can pick their own.
type configs map[string]httpserver.Config

// servers is what a test runner injects to reach the named servers, as an app would.
type servers struct {
	fx.In

	API   *httpserver.Server `name:"httpserver:server:api"   optional:"true"`
	Admin *httpserver.Server `name:"httpserver:server:admin" optional:"true"`
}

// plain returns a Config for a plaintext server on 127.0.0.1:0.
func plain() httpserver.Config {
	return httpserver.Config{Host: "127.0.0.1", ShutdownTimeout: time.Second}
}

// provide provides the server name, answering with its own name, and counts its constructor's calls in calls.
func provide(name string, calls *atomic.Int32) fx.Option {
	return httpserver.ProvideServer(name, func(c configs) httpserver.Spec {
		calls.Add(1)

		return httpserver.Spec{Config: c[name], Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, name)
		})}
	})
}

// shared returns the options every command of a test app lists: the Module, a logger and the configs.
func shared(logger *slog.Logger, c configs, opts ...fx.Option) fx.Option {
	return fx.Options(append([]fx.Option{httpserver.Module, fx.Supply(logger, c)}, opts...)...)
}

// run runs an app of opts under kernelrun.
func run(t *testing.T, opts ...fx.Option) error {
	t.Helper()

	return kernelrun.Run(t.Context(), kernelrun.Options{StopTimeout: 5 * time.Second}, opts...)
}

// probe registers a runner that calls f with the servers once the app has started, and returns f's result.
func probe(f func(ctx context.Context, s servers) error) fx.Option {
	return kernelrun.ProvideRunner("probe", func(s servers) kernelrun.Func {
		return func(ctx context.Context) error { return f(ctx, s) }
	})
}

// idle registers a runner that returns at once, for commands that run nothing else.
var idle = kernelrun.WithRunner("idle", func(context.Context) error { return nil })
