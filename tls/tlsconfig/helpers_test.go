package tlsconfig_test

import (
	"bytes"
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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// marker is a string planted in malformed files, to check that no error or record repeats their contents.
const marker = "MARKER-7f3a9c"

// authority is a test CA, written as PEM to File.
type authority struct {
	File string
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// leaf is a certificate an authority issued, written as PEM to CertFile and KeyFile.
type leaf struct {
	CertFile string
	KeyFile  string
	Cert     *x509.Certificate
	Pair     tls.Certificate
}

// newAuthority writes a self-signed CA named name into a temporary directory.
func newAuthority(t *testing.T, name string) authority {
	t.Helper()

	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: name},
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

	file := filepath.Join(t.TempDir(), name+".pem")
	writePEM(t, file, "CERTIFICATE", der)

	return authority{File: file, cert: cert, key: key}
}

// serverLeaf issues a server certificate named cn, valid for server.test.
func (a authority) serverLeaf(t *testing.T, cn string) leaf {
	t.Helper()

	return a.issue(t, cn, x509.ExtKeyUsageServerAuth, []string{"server.test"})
}

// serverLeafFor issues a server certificate named after, and valid for, name: a DNS name or an IP address.
func (a authority) serverLeafFor(t *testing.T, name string) leaf {
	t.Helper()

	return a.issue(t, name, x509.ExtKeyUsageServerAuth, []string{name})
}

// pool returns a pool holding only the authority.
func (a authority) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.cert)

	return pool
}

// clientLeaf issues a client certificate.
func (a authority) clientLeaf(t *testing.T, cn string) leaf {
	t.Helper()

	return a.issue(t, cn, x509.ExtKeyUsageClientAuth, nil)
}

// serverLeafValid issues a server certificate named cn, valid for server.test from notBefore to notAfter.
func (a authority) serverLeafValid(t *testing.T, cn string, notBefore, notAfter time.Time) leaf {
	t.Helper()

	return a.issueValid(t, cn, x509.ExtKeyUsageServerAuth, []string{"server.test"}, notBefore, notAfter)
}

// issue issues a leaf named cn for usage, valid for names, each a DNS name or an IP address, from an hour ago to an
// hour from now.
func (a authority) issue(t *testing.T, cn string, usage x509.ExtKeyUsage, names []string) leaf {
	t.Helper()

	return a.issueValid(t, cn, usage, names, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
}

// issueValid issues a leaf like issue, valid from notBefore to notAfter.
func (a authority) issueValid(
	t *testing.T,
	cn string,
	usage x509.ExtKeyUsage,
	names []string,
	notBefore, notAfter time.Time,
) leaf {
	t.Helper()

	var dnsNames []string

	var ips []net.IP

	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			ips = append(ips, ip)
		} else {
			dnsNames = append(dnsNames, name)
		}
	}

	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	dir := t.TempDir()
	l := leaf{
		CertFile: filepath.Join(dir, cn+".crt"),
		KeyFile:  filepath.Join(dir, cn+".key"),
		Cert:     cert,
	}

	writePEM(t, l.CertFile, "CERTIFICATE", der)
	writePEM(t, l.KeyFile, "PRIVATE KEY", keyDER)

	l.Pair, err = tls.LoadX509KeyPair(l.CertFile, l.KeyFile)
	require.NoError(t, err)

	return l
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

// copyFile replaces dst's contents with src's.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()

	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0o600))
}

// pipe is one direction of an in-memory connection. Unlike net.Pipe, writes never block, so a side that aborts a
// handshake can send its alert while the other side is still writing its flight.
type pipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

func newPipe() *pipe {
	p := &pipe{}
	p.cond = sync.NewCond(&p.mu)

	return p
}

func (p *pipe) write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return 0, net.ErrClosed
	}

	p.cond.Broadcast()

	return p.buf.Write(b)
}

func (p *pipe) read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for p.buf.Len() == 0 && !p.closed {
		p.cond.Wait()
	}

	if p.buf.Len() == 0 {
		return 0, io.EOF
	}

	return p.buf.Read(b)
}

func (p *pipe) close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	p.cond.Broadcast()
}

// memConn is one end of an in-memory connection with buffered writes. Deadlines aren't supported.
type memConn struct {
	in, out *pipe
}

// connPair returns the two ends of an in-memory connection.
func connPair() (*memConn, *memConn) {
	a, b := newPipe(), newPipe()

	return &memConn{in: a, out: b}, &memConn{in: b, out: a}
}

func (c *memConn) Read(b []byte) (int, error)  { return c.in.read(b) }
func (c *memConn) Write(b []byte) (int, error) { return c.out.write(b) }

func (c *memConn) Close() error {
	c.in.close()
	c.out.close()

	return nil
}

func (*memConn) LocalAddr() net.Addr              { return memAddr{} }
func (*memConn) RemoteAddr() net.Addr             { return memAddr{} }
func (*memConn) SetDeadline(time.Time) error      { return nil }
func (*memConn) SetReadDeadline(time.Time) error  { return nil }
func (*memConn) SetWriteDeadline(time.Time) error { return nil }

type memAddr struct{}

func (memAddr) Network() string { return "mem" }
func (memAddr) String() string  { return "mem" }

// handshakeResult is what each side of a handshake saw.
type handshakeResult struct {
	Client    tls.ConnectionState
	Server    tls.ConnectionState
	ClientErr error
	ServerErr error
}

// handshake runs a TLS handshake over an in-memory connection between a client using client, dialling host as
// tls.Dial would, and a server using server.
func handshake(t *testing.T, client, server *tls.Config, host string) handshakeResult {
	t.Helper()

	clientConn, serverConn := connPair()

	client = client.Clone()

	if client.ServerName == "" {
		client.ServerName = host
	}

	cc := tls.Client(clientConn, client)
	sc := tls.Server(serverConn, server)
	serverErr := make(chan error, 1)

	go func() {
		err := sc.HandshakeContext(t.Context())
		_ = serverConn.Close()

		serverErr <- err
	}()

	var res handshakeResult

	res.ClientErr = cc.HandshakeContext(t.Context())

	if res.ClientErr == nil {
		// Reads until the server side closes: closing now would fail the session tickets a server writes after reading
		// the client's certificate.
		_, _ = io.Copy(io.Discard, cc)
	}

	_ = clientConn.Close()
	res.ServerErr = <-serverErr
	res.Client = cc.ConnectionState()
	res.Server = sc.ConnectionState()

	return res
}

// recorder is a slog.Handler that keeps every record it's handed.
type recorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (*recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.records = append(r.records, rec.Clone())

	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recorder) WithGroup(string) slog.Handler      { return r }

// matching returns each record at level whose message contains part, as render renders it.
func (r *recorder) matching(level slog.Level, part string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string

	for _, rec := range r.records {
		if rec.Level == level && strings.Contains(rec.Message, part) {
			out = append(out, render(rec))
		}
	}

	return out
}

// render returns a record as its message followed by its attributes, one key=value each.
func render(rec slog.Record) string {
	text := rec.Message

	rec.Attrs(func(a slog.Attr) bool {
		text += " " + a.String()

		return true
	})

	return text
}
