package tlsconfig_test

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tls/tlsconfig"
)

// e2eInterval is the reload interval of the end-to-end test. It runs over real sockets, which synctest can't drive,
// so it waits in real time.
const e2eInterval = 100 * time.Millisecond

// e2eFiles are the paths both sides read, as a secret mount would hold them: each side's key pair and the bundle of
// CAs it trusts for the other side.
type e2eFiles struct {
	ServerCert, ServerKey, ServerCAs string
	ClientCert, ClientKey, ClientCAs string
}

func newE2EFiles(t *testing.T) e2eFiles {
	t.Helper()

	dir := t.TempDir()

	return e2eFiles{
		ServerCert: filepath.Join(dir, "server.crt"),
		ServerKey:  filepath.Join(dir, "server.key"),
		ServerCAs:  filepath.Join(dir, "server-cas.pem"),
		ClientCert: filepath.Join(dir, "client.crt"),
		ClientKey:  filepath.Join(dir, "client.key"),
		ClientCAs:  filepath.Join(dir, "client-cas.pem"),
	}
}

// issue writes a server leaf and a client leaf over the key pair files, the server's from serverCA and the client's
// from clientCA, each named cn.
func (f e2eFiles) issue(t *testing.T, serverCA, clientCA authority, cn string) {
	t.Helper()

	server := serverCA.serverLeaf(t, cn)
	copyFile(t, server.CertFile, f.ServerCert)
	copyFile(t, server.KeyFile, f.ServerKey)

	client := clientCA.clientLeaf(t, cn)
	copyFile(t, client.CertFile, f.ClientCert)
	copyFile(t, client.KeyFile, f.ClientKey)
}

// trust writes the CA bundles: serverCAs, which the client trusts for the server, and clientCAs, which the server
// trusts for clients.
func (f e2eFiles) trust(t *testing.T, serverCAs, clientCAs []authority) {
	t.Helper()

	writeBundle(t, f.ServerCAs, serverCAs)
	writeBundle(t, f.ClientCAs, clientCAs)
}

// writeBundle writes the certificates of cas into one PEM file at path.
func writeBundle(t *testing.T, path string, cas []authority) {
	t.Helper()

	var bundle []byte

	for _, ca := range cas {
		data, err := os.ReadFile(ca.File)
		require.NoError(t, err)

		bundle = append(bundle, data...)
	}

	require.NoError(t, os.WriteFile(path, bundle, 0o600))
}

// e2eResult is what one request saw from each side.
type e2eResult struct {
	ServerCN string // the server certificate the client verified
	ClientCN string // the client certificate the server verified
	Proto    int    // the HTTP major version negotiated
}

// startServer serves HTTPS with srv on a loopback port, answering each request with the common name of the client
// certificate the server verified, and returns its address. The server's own log is discarded: a rejected handshake is
// asserted from the client's side.
func startServer(t *testing.T, srv *http.Server) string {
	t.Helper()

	srv.ErrorLog = slog.NewLogLogger(slog.DiscardHandler, slog.LevelError)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.TLS.PeerCertificates[0].Subject.CommonName)
	})

	go func() { _ = srv.ServeTLS(ln, "", "") }()

	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String()
}

// newClient returns an HTTP client dialling with tlsCfg that connects to addr for every host, opens a new connection
// for every request, so each one handshakes, and attempts HTTP/2.
func newClient(t *testing.T, tlsCfg *tls.Config, addr string) *http.Client {
	t.Helper()

	var dialer net.Dialer

	transport := &http.Transport{
		TLSClientConfig: tlsCfg,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		DisableKeepAlives: true,
		ForceAttemptHTTP2: true,
	}

	t.Cleanup(transport.CloseIdleConnections)

	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

// request makes one request to https://server.test, returning the response with its body read, or the error.
func request(t *testing.T, client *http.Client) (*http.Response, string, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://server.test/", http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp, string(body), nil
}

// get makes one request to https://server.test, requires it to succeed, and reports what each side saw.
func get(t *testing.T, client *http.Client) e2eResult {
	t.Helper()

	resp, body, err := request(t, client)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	return e2eResult{
		ServerCN: resp.TLS.PeerCertificates[0].Subject.CommonName,
		ClientCN: body,
		Proto:    resp.ProtoMajor,
	}
}

// TestEndToEnd runs mutual TLS between an http.Server and an http.Client built from a ServerConfig and a
// ClientConfig, through a leaf rotation and then a CA rotation done the way cert-manager and trust-manager do it:
// distribute the new CAs beside the old, re-issue the leaves, then drop the old CAs. Neither side restarts, and once
// the old CAs are dropped, a client still holding a certificate from the old client CA is turned away.
func TestEndToEnd(t *testing.T) {
	t.Parallel()

	f := newE2EFiles(t)
	serverCA := newAuthority(t, "server-ca")
	clientCA := newAuthority(t, "client-ca")

	f.trust(t, []authority{serverCA}, []authority{clientCA})
	f.issue(t, serverCA, clientCA, "first")

	serverTLS, err := tlsconfig.ServerConfig{
		Enabled:        true,
		CertFile:       f.ServerCert,
		KeyFile:        f.ServerKey,
		ClientCAFile:   f.ClientCAs,
		ReloadInterval: e2eInterval,
	}.TLSConfig(nil)
	require.NoError(t, err)

	clientTLS, err := tlsconfig.ClientConfig{
		Enabled:        true,
		CAFile:         f.ServerCAs,
		CertFile:       f.ClientCert,
		KeyFile:        f.ClientKey,
		ReloadInterval: e2eInterval,
	}.TLSConfig(nil)
	require.NoError(t, err)

	addr := startServer(t, &http.Server{TLSConfig: serverTLS, ReadHeaderTimeout: 5 * time.Second})
	client := newClient(t, clientTLS, addr)

	// settle waits out the reload interval, then makes the request that picks up whatever changed.
	settle := func() e2eResult {
		t.Helper()

		time.Sleep(e2eInterval + e2eInterval/2)

		return get(t, client)
	}

	t.Log("first handshake")
	assert.Equal(t, e2eResult{ServerCN: "first", ClientCN: "first", Proto: 2}, get(t, client))

	t.Log("leaves re-issued from the same CAs")
	f.issue(t, serverCA, clientCA, "second")
	assert.Equal(t, e2eResult{ServerCN: "second", ClientCN: "second", Proto: 2}, settle())

	t.Log("new CAs distributed beside the old")
	nextServerCA := newAuthority(t, "next-server-ca")
	nextClientCA := newAuthority(t, "next-client-ca")
	f.trust(t, []authority{serverCA, nextServerCA}, []authority{clientCA, nextClientCA})
	assert.Equal(t, e2eResult{ServerCN: "second", ClientCN: "second", Proto: 2}, settle())

	t.Log("leaves re-issued from the new CAs")
	f.issue(t, nextServerCA, nextClientCA, "third")
	assert.Equal(t, e2eResult{ServerCN: "third", ClientCN: "third", Proto: 2}, settle())

	t.Log("old CAs dropped")
	stale := clientCA.clientLeaf(t, "stale")
	staleTLS, err := tlsconfig.ClientConfig{
		Enabled:  true,
		CAFile:   f.ServerCAs,
		CertFile: stale.CertFile,
		KeyFile:  stale.KeyFile,
	}.TLSConfig(nil)
	require.NoError(t, err)

	staleClient := newClient(t, staleTLS, addr)
	assert.Equal(t, "stale", get(t, staleClient).ClientCN, "accepted while its CA is still trusted")

	f.trust(t, []authority{nextServerCA}, []authority{nextClientCA})
	assert.Equal(t, e2eResult{ServerCN: "third", ClientCN: "third", Proto: 2}, settle())

	_, _, err = request(t, staleClient)
	require.Error(t, err, "a client certificate from the dropped CA was accepted")
	assert.ErrorContains(
		t,
		err,
		"remote error: tls: bad certificate",
		"want the server's rejection, not another failure",
	)
}
