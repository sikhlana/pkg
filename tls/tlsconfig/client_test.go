package tlsconfig_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tls/tlsconfig"
)

// serverConfig returns a server configuration presenting l.
func serverConfig(l leaf) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{l.Pair}}
}

// requireUnknownAuthority asserts that err reports a certificate no trusted CA signed.
func requireUnknownAuthority(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err)

	_, ok := errors.AsType[x509.UnknownAuthorityError](err)
	assert.True(t, ok, "want an unknown-authority error, got %v", err)
}

func TestClientDisabled(t *testing.T) {
	t.Parallel()

	t.Run("zero config", func(t *testing.T) {
		t.Parallel()

		cfg, err := tlsconfig.ClientConfig{}.TLSConfig(nil)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("paths that don't exist", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "missing")
		c := tlsconfig.ClientConfig{CAFile: missing, CertFile: missing, KeyFile: missing}

		cfg, err := c.TLSConfig(nil)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})
}

func TestClientSystemRoots(t *testing.T) {
	t.Parallel()

	ca := newAuthority(t, "private")
	server := ca.serverLeafFor(t, "server.test")

	cfg, err := tlsconfig.ClientConfig{Enabled: true}.TLSConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Nil(t, cfg.RootCAs)

	res := handshake(t, cfg, serverConfig(server), "server.test")
	require.Error(t, res.ClientErr)

	_, ok := errors.AsType[*tls.CertificateVerificationError](res.ClientErr)
	require.True(t, ok, "want a certificate verification error, got %v", res.ClientErr)

	// The platform verifiers on macOS and Windows report an untrusted root in their own words, not as an
	// UnknownAuthorityError.
	if runtime.GOOS != "darwin" && runtime.GOOS != "ios" && runtime.GOOS != "windows" {
		requireUnknownAuthority(t, res.ClientErr)
	}
}

func TestClientCAFile(t *testing.T) {
	t.Parallel()

	t.Run("a certificate from the CA", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "private")
		server := ca.serverLeafFor(t, "server.test")

		cfg, err := tlsconfig.ClientConfig{Enabled: true, CAFile: ca.File}.TLSConfig(nil)
		require.NoError(t, err)

		res := handshake(t, cfg, serverConfig(server), "server.test")
		require.NoError(t, res.ClientErr)
		require.NoError(t, res.ServerErr)
		assert.Equal(t, server.Cert.Raw, res.Client.PeerCertificates[0].Raw)
	})

	t.Run("a certificate from another CA", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "private")
		other := newAuthority(t, "other")
		server := other.serverLeafFor(t, "server.test")

		cfg, err := tlsconfig.ClientConfig{Enabled: true, CAFile: ca.File}.TLSConfig(nil)
		require.NoError(t, err)

		res := handshake(t, cfg, serverConfig(server), "server.test")
		requireUnknownAuthority(t, res.ClientErr)
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "ca.pem")

		_, err := tlsconfig.ClientConfig{Enabled: true, CAFile: missing}.TLSConfig(nil)
		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), missing)
	})

	t.Run("no certificates", func(t *testing.T) {
		t.Parallel()

		empty := filepath.Join(t.TempDir(), "ca.pem")
		require.NoError(t, os.WriteFile(empty, []byte("no certificates here\n"), 0o600))

		_, err := tlsconfig.ClientConfig{Enabled: true, CAFile: empty}.TLSConfig(nil)
		require.ErrorIs(t, err, tlsconfig.ErrNoCertificates)
		assert.Contains(t, err.Error(), empty)
	})

	t.Run("changed within the interval", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "private")
		other := newAuthority(t, "other")
		server := ca.serverLeafFor(t, "server.test")

		cfg, err := tlsconfig.ClientConfig{Enabled: true, CAFile: ca.File}.TLSConfig(nil)
		require.NoError(t, err)

		copyFile(t, other.File, ca.File)

		res := handshake(t, cfg, serverConfig(server), "server.test")
		require.NoError(t, res.ClientErr)
		require.NoError(t, res.ServerErr)
	})
}

func TestClientServerName(t *testing.T) {
	t.Parallel()

	ca := newAuthority(t, "private")
	server := ca.serverLeafFor(t, "db.internal")

	cfg, err := tlsconfig.ClientConfig{Enabled: true, CAFile: ca.File, ServerName: "db.internal"}.TLSConfig(nil)
	require.NoError(t, err)

	res := handshake(t, cfg, serverConfig(server), "127.0.0.1")
	require.NoError(t, res.ClientErr)
	require.NoError(t, res.ServerErr)
	assert.Equal(t, "db.internal", res.Server.ServerName)
}

func TestClientInsecureSkipVerify(t *testing.T) {
	t.Parallel()

	ca := newAuthority(t, "private")
	server := ca.serverLeafFor(t, "server.test")

	cfg, err := tlsconfig.ClientConfig{Enabled: true, InsecureSkipVerify: true}.TLSConfig(nil)
	require.NoError(t, err)

	res := handshake(t, cfg, serverConfig(server), "server.test")
	require.NoError(t, res.ClientErr)
	require.NoError(t, res.ServerErr)
}

func TestClientKeyPair(t *testing.T) {
	t.Parallel()

	t.Run("a server that requires a client certificate", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "private")
		clientCA := newAuthority(t, "clients")
		server := ca.serverLeafFor(t, "server.test")
		client := clientCA.clientLeaf(t, "client")

		cfg, err := tlsconfig.ClientConfig{
			Enabled:  true,
			CAFile:   ca.File,
			CertFile: client.CertFile,
			KeyFile:  client.KeyFile,
		}.TLSConfig(nil)
		require.NoError(t, err)

		srv := serverConfig(server)
		srv.ClientAuth = tls.RequireAndVerifyClientCert
		srv.ClientCAs = clientCA.pool()

		res := handshake(t, cfg, srv, "server.test")
		require.NoError(t, res.ClientErr)
		require.NoError(t, res.ServerErr)
		require.Len(t, res.Server.PeerCertificates, 1)
		assert.Equal(t, client.Cert.Raw, res.Server.PeerCertificates[0].Raw)
	})

	t.Run("a server whose request doesn't accept the pair", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "private")
		clientCA := newAuthority(t, "clients")
		client := clientCA.clientLeaf(t, "client")

		cfg, err := tlsconfig.ClientConfig{
			Enabled:  true,
			CAFile:   ca.File,
			CertFile: client.CertFile,
			KeyFile:  client.KeyFile,
		}.TLSConfig(nil)
		require.NoError(t, err)

		// The server asks for a certificate from another CA, without requiring one: a native client sends none and
		// completes the handshake, and so must this one.
		srv := serverConfig(ca.serverLeafFor(t, "server.test"))
		srv.ClientAuth = tls.VerifyClientCertIfGiven
		srv.ClientCAs = newAuthority(t, "other").pool()

		res := handshake(t, cfg, srv, "server.test")
		require.NoError(t, res.ClientErr)
		require.NoError(t, res.ServerErr)
		assert.Empty(t, res.Server.PeerCertificates)

		srv.ClientCAs = clientCA.pool()

		res = handshake(t, cfg, srv, "server.test")
		require.NoError(t, res.ClientErr)
		require.NoError(t, res.ServerErr)
		require.Len(t, res.Server.PeerCertificates, 1)
		assert.Equal(t, client.Cert.Raw, res.Server.PeerCertificates[0].Raw)
	})

	t.Run("only the certificate file", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ClientConfig{Enabled: true, CertFile: "client.crt"}

		_, err := c.TLSConfig(nil)
		require.ErrorIs(t, err, tlsconfig.ErrIncompleteKeyPair)
	})

	t.Run("a key that doesn't match", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "private")
		client := ca.clientLeaf(t, "client")
		other := ca.clientLeaf(t, "other")

		_, err := tlsconfig.ClientConfig{
			Enabled:  true,
			CertFile: client.CertFile,
			KeyFile:  other.KeyFile,
		}.TLSConfig(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), client.CertFile)
		assert.Contains(t, err.Error(), other.KeyFile)
	})
}

// clientSetup is a server that requires a client certificate from clientCA, and an enabled ClientConfig presenting
// one. Build it inside a synctest bubble, so the certificates are valid at the bubble's time.
type clientSetup struct {
	clientCA authority
	client   leaf
	srv      *tls.Config
	cfg      tlsconfig.ClientConfig
}

func newClientSetup(t *testing.T) clientSetup {
	t.Helper()

	serverCA := newAuthority(t, "server-ca")
	clientCA := newAuthority(t, "client-ca")
	client := clientCA.clientLeaf(t, "client")

	srv := serverConfig(serverCA.serverLeafFor(t, "server.test"))
	srv.ClientAuth = tls.RequireAndVerifyClientCert
	srv.ClientCAs = clientCA.pool()

	return clientSetup{
		clientCA: clientCA,
		client:   client,
		srv:      srv,
		cfg: tlsconfig.ClientConfig{
			Enabled:  true,
			CAFile:   serverCA.File,
			CertFile: client.CertFile,
			KeyFile:  client.KeyFile,
		},
	}
}

// rotate writes next over the client's key pair files.
func (s clientSetup) rotate(t *testing.T, next leaf) {
	t.Helper()

	copyFile(t, next.CertFile, s.client.CertFile)
	copyFile(t, next.KeyFile, s.client.KeyFile)
}

// requirePresents asserts that a handshake with the server succeeds and that the server sees want.
func (s clientSetup) requirePresents(t *testing.T, cfg *tls.Config, want leaf) {
	t.Helper()

	res := handshake(t, cfg, s.srv, "server.test")
	require.NoError(t, res.ClientErr)
	require.NoError(t, res.ServerErr)
	require.Len(t, res.Server.PeerCertificates, 1)
	assert.Equal(t, want.Cert.Raw, res.Server.PeerCertificates[0].Raw)
}

func TestClientKeyPairRefresh(t *testing.T) {
	t.Parallel()

	t.Run("served through GetClientCertificate only", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			cfg, err := newClientSetup(t).cfg.TLSConfig(nil)
			require.NoError(t, err)

			assert.Empty(t, cfg.Certificates)
			assert.NotNil(t, cfg.GetClientCertificate)
		})
	})

	t.Run("files replaced, interval elapsed", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newClientSetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			next := s.clientCA.clientLeaf(t, "next")
			s.rotate(t, next)

			time.Sleep(tlsconfig.DefaultReloadInterval - time.Second)
			s.requirePresents(t, cfg, s.client)

			time.Sleep(time.Second)
			s.requirePresents(t, cfg, next)
		})
	})

	t.Run("the certificate written before the key", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newClientSetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			next := s.clientCA.clientLeaf(t, "next")
			copyFile(t, next.CertFile, s.client.CertFile)

			time.Sleep(tlsconfig.DefaultReloadInterval)
			s.requirePresents(t, cfg, s.client)

			copyFile(t, next.KeyFile, s.client.KeyFile)

			time.Sleep(tlsconfig.DefaultReloadInterval)
			s.requirePresents(t, cfg, next)
		})
	})

	t.Run("a copy keeps refreshing", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newClientSetup(t)

			base, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			cfg := base.Clone()

			next := s.clientCA.clientLeaf(t, "next")
			s.rotate(t, next)

			time.Sleep(tlsconfig.DefaultReloadInterval)
			s.requirePresents(t, cfg, next)
		})
	})

	t.Run("a refresh is logged", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newClientSetup(t)
			rec := &recorder{}

			cfg, err := s.cfg.TLSConfig(slog.New(rec))
			require.NoError(t, err)

			loaded := rec.matching(slog.LevelInfo, "tls client key pair loaded")
			require.Len(t, loaded, 1)
			assert.Contains(t, loaded[0], s.client.CertFile)
			assert.Contains(t, loaded[0], "subject=CN=client")
			assert.Contains(t, loaded[0], "serial="+s.client.Cert.SerialNumber.String())
			assert.Contains(t, loaded[0], "not_after=")

			next := s.clientCA.clientLeaf(t, "next")
			s.rotate(t, next)

			time.Sleep(tlsconfig.DefaultReloadInterval)
			handshake(t, cfg, s.srv, "server.test")

			reloaded := rec.matching(slog.LevelInfo, "reloaded")
			require.Len(t, reloaded, 1)
			assert.Contains(t, reloaded[0], s.client.CertFile)
			assert.Contains(t, reloaded[0], "serial="+next.Cert.SerialNumber.String())
			assert.Empty(t, rec.matching(slog.LevelWarn, ""))
		})
	})

	t.Run("an expired pair", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newClientSetup(t)
			rec := &recorder{}

			cfg, err := s.cfg.TLSConfig(slog.New(rec))
			require.NoError(t, err)

			expired := s.clientCA.issueValid(t, "expired", x509.ExtKeyUsageClientAuth, nil,
				time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
			s.rotate(t, expired)

			time.Sleep(tlsconfig.DefaultReloadInterval)
			s.requirePresents(t, cfg, s.client)

			warns := rec.matching(slog.LevelWarn, "rejecting tls client key pair")
			require.Len(t, warns, 1)
			assert.Contains(t, warns[0], tlsconfig.ErrCertificateNotCurrent.Error())
		})
	})

	t.Run("a negative interval", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ClientConfig{Enabled: true, ReloadInterval: -time.Second}

		_, err := c.TLSConfig(nil)
		require.ErrorIs(t, err, tlsconfig.ErrNegativeReloadInterval)
	})
}

func TestClientErrorsOmitContents(t *testing.T) {
	t.Parallel()

	garbage := map[string][]byte{
		"plain text": []byte("not a key " + marker + "\n"),
		"pem block":  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte(marker)}),
	}

	for name, contents := range garbage {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ca := newAuthority(t, "private")
			client := ca.clientLeaf(t, "client")
			require.NoError(t, os.WriteFile(client.KeyFile, contents, 0o600))

			_, err := tlsconfig.ClientConfig{
				Enabled:  true,
				CertFile: client.CertFile,
				KeyFile:  client.KeyFile,
			}.TLSConfig(nil)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), marker)
		})
	}
}

// caSetup is a client trusting serverCA through a CA file it can rotate, and a server presenting a certificate
// serverCA issued for server.test. Build it inside a synctest bubble, so the certificates are valid at the bubble's
// time.
type caSetup struct {
	serverCA authority
	server   leaf
	caFile   string
	cfg      tlsconfig.ClientConfig
}

func newCASetup(t *testing.T) caSetup {
	t.Helper()

	serverCA := newAuthority(t, "server-ca")
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	copyFile(t, serverCA.File, caFile)

	return caSetup{
		serverCA: serverCA,
		server:   serverCA.serverLeafFor(t, "server.test"),
		caFile:   caFile,
		cfg:      tlsconfig.ClientConfig{Enabled: true, CAFile: caFile},
	}
}

// dial handshakes cfg with a server presenting l, dialling server.test.
func dial(t *testing.T, cfg *tls.Config, l leaf) handshakeResult {
	t.Helper()

	return handshake(t, cfg, serverConfig(l), "server.test")
}

func TestClientCARefresh(t *testing.T) {
	t.Parallel()

	t.Run("verified through the hooks only with a CA file", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			assert.True(t, cfg.InsecureSkipVerify)
			assert.NotNil(t, cfg.RootCAs)
			assert.NotNil(t, cfg.VerifyConnection)
			assert.NotNil(t, cfg.VerifyPeerCertificate)

			native, err := tlsconfig.ClientConfig{Enabled: true}.TLSConfig(nil)
			require.NoError(t, err)

			assert.False(t, native.InsecureSkipVerify)
			assert.Nil(t, native.VerifyConnection)
			assert.Nil(t, native.VerifyPeerCertificate)

			skipped := s.cfg
			skipped.InsecureSkipVerify = true

			insecure, err := skipped.TLSConfig(nil)
			require.NoError(t, err)

			assert.True(t, insecure.InsecureSkipVerify)
			assert.Nil(t, insecure.VerifyConnection)
			assert.Nil(t, insecure.VerifyPeerCertificate)
		})
	})

	t.Run("a rotated CA", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			next := newAuthority(t, "next-ca")
			nextServer := next.serverLeafFor(t, "server.test")
			copyFile(t, next.File, s.caFile)

			time.Sleep(tlsconfig.DefaultReloadInterval - time.Second)
			assertAccepted(t, true, dial(t, cfg, s.server))
			assertAccepted(t, false, dial(t, cfg, nextServer))

			time.Sleep(time.Second)
			assertAccepted(t, true, dial(t, cfg, nextServer))

			res := dial(t, cfg, s.server)
			requireUnknownAuthority(t, res.ClientErr)
		})
	})

	t.Run("a CA file emptied", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			require.NoError(t, os.WriteFile(s.caFile, []byte("no certificates here\n"), 0o600))

			time.Sleep(tlsconfig.DefaultReloadInterval)
			assertAccepted(t, true, dial(t, cfg, s.server))
		})
	})

	t.Run("the server name is still verified", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			res := dial(t, cfg, s.serverCA.serverLeafFor(t, "other.test"))
			require.Error(t, res.ClientErr)

			_, ok := errors.AsType[x509.HostnameError](res.ClientErr)
			assert.True(t, ok, "want a hostname error, got %v", res.ClientErr)
		})
	})

	t.Run("no server name", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			res := handshake(t, cfg, serverConfig(s.server), "")
			require.Error(t, res.ClientErr)
			require.ErrorIs(t, res.ClientErr, tlsconfig.ErrNoServerName)
		})
	})

	t.Run("server_name set to an IP address", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)
			s.cfg.ServerName = "10.0.0.5"

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			res := handshake(t, cfg, serverConfig(s.serverCA.serverLeafFor(t, "10.0.0.5")), "10.0.0.5")
			require.NoError(t, res.ClientErr)
			require.NoError(t, res.ServerErr)

			res = handshake(t, cfg, serverConfig(s.serverCA.serverLeafFor(t, "10.0.0.6")), "10.0.0.5")
			require.Error(t, res.ClientErr)

			_, ok := errors.AsType[x509.HostnameError](res.ClientErr)
			assert.True(t, ok, "want a hostname error, got %v", res.ClientErr)
		})
	})

	t.Run("an IP address dialled without server_name", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			res := handshake(t, cfg, serverConfig(s.serverCA.serverLeafFor(t, "10.0.0.5")), "10.0.0.5")
			require.Error(t, res.ClientErr)
			require.ErrorIs(t, res.ClientErr, tlsconfig.ErrNoServerName)
		})
	})

	t.Run("the backstop when VerifyConnection is replaced", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			cfg.VerifyConnection = func(tls.ConnectionState) error { return nil }

			assertAccepted(t, true, dial(t, cfg, s.server))

			res := dial(t, cfg, newAuthority(t, "other-ca").serverLeafFor(t, "server.test"))
			requireUnknownAuthority(t, res.ClientErr)
		})
	})

	t.Run("a copy keeps refreshing", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)

			base, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			cfg := base.Clone()

			next := newAuthority(t, "next-ca")
			copyFile(t, next.File, s.caFile)

			time.Sleep(tlsconfig.DefaultReloadInterval)
			assertAccepted(t, true, dial(t, cfg, next.serverLeafFor(t, "server.test")))
		})
	})

	t.Run("a refresh is logged", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newCASetup(t)
			rec := &recorder{}

			cfg, err := s.cfg.TLSConfig(slog.New(rec))
			require.NoError(t, err)

			copyFile(t, newAuthority(t, "next-ca").File, s.caFile)

			time.Sleep(tlsconfig.DefaultReloadInterval)
			dial(t, cfg, s.server)

			require.Len(t, rec.matching(slog.LevelInfo, "tls CAs loaded"), 1)

			reloaded := rec.matching(slog.LevelInfo, "reloaded")
			require.Len(t, reloaded, 1)
			assert.Contains(t, reloaded[0], s.caFile)
		})
	})
}
