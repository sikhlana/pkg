package tlsconfig_test

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tls/tlsconfig"
)

// setup is a server's CA, its key pair and an enabled Config serving it. Build it inside a synctest bubble, so the
// certificates are valid at the bubble's time.
type setup struct {
	ca     authority
	server leaf
	cfg    tlsconfig.ServerConfig
}

func newSetup(t *testing.T) setup {
	t.Helper()

	ca := newAuthority(t, "server-ca")
	server := ca.serverLeaf(t, "server")

	return setup{
		ca:     ca,
		server: server,
		cfg:    tlsconfig.ServerConfig{Enabled: true, CertFile: server.CertFile, KeyFile: server.KeyFile},
	}
}

// rotate writes next over the server's key pair files.
func (s setup) rotate(t *testing.T, next leaf) {
	t.Helper()

	copyFile(t, next.CertFile, s.server.CertFile)
	copyFile(t, next.KeyFile, s.server.KeyFile)
}

// served handshakes with srv as a client that sends no server name, presenting certs, and returns the result.
func served(t *testing.T, srv *tls.Config, certs ...tls.Certificate) handshakeResult {
	t.Helper()

	client := &tls.Config{InsecureSkipVerify: true, Certificates: certs}

	return handshake(t, client, srv, "")
}

// requireServes asserts that a handshake with srv succeeds and receives want.
func requireServes(t *testing.T, srv *tls.Config, want leaf) {
	t.Helper()

	res := served(t, srv)
	require.NoError(t, res.ClientErr)
	require.NoError(t, res.ServerErr)
	require.NotEmpty(t, res.Client.PeerCertificates)
	assert.Equal(t, want.Cert.Subject.CommonName, res.Client.PeerCertificates[0].Subject.CommonName)
}

func TestServerDisabled(t *testing.T) {
	t.Parallel()

	t.Run("zero config", func(t *testing.T) {
		t.Parallel()

		cfg, err := tlsconfig.ServerConfig{}.TLSConfig(nil)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("incomplete", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ServerConfig{ClientAuth: "mandatory", ReloadInterval: -time.Second}

		cfg, err := c.TLSConfig(nil)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})
}

func TestServerKeyPair(t *testing.T) {
	t.Parallel()

	t.Run("without a key file", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ServerConfig{Enabled: true, CertFile: "server.crt"}

		_, err := c.TLSConfig(nil)
		require.ErrorIs(t, err, tlsconfig.ErrMissingKeyPair)
	})

	t.Run("a key for another certificate", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "server-ca")
		server := ca.serverLeaf(t, "server")
		other := ca.serverLeaf(t, "other")

		_, err := tlsconfig.ServerConfig{
			Enabled:  true,
			CertFile: server.CertFile,
			KeyFile:  other.KeyFile,
		}.TLSConfig(
			nil,
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), server.CertFile)
	})

	t.Run("a missing key file", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "server-ca")
		server := ca.serverLeaf(t, "server")
		require.NoError(t, os.Remove(server.KeyFile))

		_, err := tlsconfig.ServerConfig{
			Enabled:  true,
			CertFile: server.CertFile,
			KeyFile:  server.KeyFile,
		}.TLSConfig(
			nil,
		)
		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), server.CertFile)
	})
}

func TestServerWithoutServerName(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newSetup(t)

		cfg, err := s.cfg.TLSConfig(nil)
		require.NoError(t, err)
		assert.Empty(t, cfg.Certificates)

		res := served(t, cfg)
		require.NoError(t, res.ClientErr)
		assert.Empty(t, res.Server.ServerName)
		assert.Equal(t, s.server.Cert.Raw, res.Client.PeerCertificates[0].Raw)

		next := s.ca.serverLeaf(t, "next")
		s.rotate(t, next)
		time.Sleep(tlsconfig.DefaultReloadInterval)

		res = served(t, cfg)
		require.NoError(t, res.ClientErr)
		assert.Empty(t, res.Server.ServerName)
		assert.Equal(t, next.Cert.Raw, res.Client.PeerCertificates[0].Raw)
	})
}

func TestServerRefresh(t *testing.T) {
	t.Parallel()

	t.Run("after the interval and not before", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			s.cfg.ReloadInterval = 10 * time.Second

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			next := s.ca.serverLeaf(t, "next")
			s.rotate(t, next)

			time.Sleep(5 * time.Second)
			requireServes(t, cfg, s.server)

			time.Sleep(5 * time.Second)
			requireServes(t, cfg, next)
		})
	})

	t.Run("the default interval", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			next := s.ca.serverLeaf(t, "next")
			s.rotate(t, next)

			time.Sleep(29 * time.Second)
			requireServes(t, cfg, s.server)

			time.Sleep(2 * time.Second)
			requireServes(t, cfg, next)
		})
	})

	t.Run("a negative interval", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ServerConfig{
			Enabled:        true,
			CertFile:       "server.crt",
			KeyFile:        "server.key",
			ReloadInterval: -time.Second,
		}

		_, err := c.TLSConfig(nil)
		require.ErrorIs(t, err, tlsconfig.ErrNegativeReloadInterval)
	})
}

func TestServerKeepsLastGoodPair(t *testing.T) {
	t.Parallel()

	t.Run("the key lags behind the certificate", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			next := s.ca.serverLeaf(t, "next")

			copyFile(t, next.CertFile, s.server.CertFile)
			time.Sleep(tlsconfig.DefaultReloadInterval)
			requireServes(t, cfg, s.server)

			copyFile(t, next.KeyFile, s.server.KeyFile)
			time.Sleep(tlsconfig.DefaultReloadInterval)
			requireServes(t, cfg, next)
		})
	})

	t.Run("a deleted key file", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			require.NoError(t, os.Remove(s.server.KeyFile))
			time.Sleep(tlsconfig.DefaultReloadInterval)
			requireServes(t, cfg, s.server)
		})
	})
}

func TestServerClientAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		policy  tlsconfig.ClientAuth
		trusted bool // a certificate from the client CA is accepted
		other   bool // a certificate from another CA is accepted
		none    bool // no certificate is accepted
		seen    bool // the server sees a presented certificate
		ca      bool // the policy takes a client CA file
	}{
		{policy: tlsconfig.ClientAuthNone, trusted: true, other: true, none: true},
		{policy: tlsconfig.ClientAuthRequest, trusted: true, other: true, none: true, seen: true},
		{policy: tlsconfig.ClientAuthRequire, trusted: true, other: true, seen: true},
		{policy: tlsconfig.ClientAuthVerifyIfGiven, trusted: true, none: true, seen: true, ca: true},
		{policy: tlsconfig.ClientAuthRequireAndVerify, trusted: true, seen: true, ca: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.policy), func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				s := newSetup(t)
				clientCA := newAuthority(t, "client-ca")
				trusted := clientCA.clientLeaf(t, "trusted")
				other := newAuthority(t, "other-ca").clientLeaf(t, "other")

				s.cfg.ClientAuth = tt.policy

				if tt.ca {
					s.cfg.ClientCAFile = clientCA.File
				}

				cfg, err := s.cfg.TLSConfig(nil)
				require.NoError(t, err)

				res := served(t, cfg, trusted.Pair)
				assertAccepted(t, tt.trusted, res)

				if tt.trusted {
					if tt.seen {
						require.Len(t, res.Server.PeerCertificates, 1)
						assert.Equal(t, trusted.Cert.Raw, res.Server.PeerCertificates[0].Raw)
					} else {
						assert.Empty(t, res.Server.PeerCertificates)
					}

					assert.Empty(t, res.Server.VerifiedChains)
				}

				assertAccepted(t, tt.other, served(t, cfg, other.Pair))
				assertAccepted(t, tt.none, served(t, cfg))
			})
		})
	}

	t.Run("unset with a client CA file", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			clientCA := newAuthority(t, "client-ca")
			s.cfg.ClientCAFile = clientCA.File

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			assertAccepted(t, false, served(t, cfg))
			assertAccepted(t, true, served(t, cfg, clientCA.clientLeaf(t, "trusted").Pair))
		})
	})

	t.Run("unset without a client CA file", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			client := newAuthority(t, "client-ca").clientLeaf(t, "client")

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			res := served(t, cfg, client.Pair)
			assertAccepted(t, true, res)
			assert.Empty(t, res.Server.PeerCertificates)
		})
	})

	t.Run("a verifying policy without a client CA file", func(t *testing.T) {
		t.Parallel()

		for _, policy := range []tlsconfig.ClientAuth{
			tlsconfig.ClientAuthVerifyIfGiven,
			tlsconfig.ClientAuthRequireAndVerify,
		} {
			c := tlsconfig.ServerConfig{
				Enabled:    true,
				CertFile:   "server.crt",
				KeyFile:    "server.key",
				ClientAuth: policy,
			}

			_, err := c.TLSConfig(nil)
			require.ErrorIs(t, err, tlsconfig.ErrClientCARequired, policy)
		}
	})

	t.Run("a client CA file beside a policy that doesn't verify", func(t *testing.T) {
		t.Parallel()

		for _, policy := range []tlsconfig.ClientAuth{
			tlsconfig.ClientAuthNone,
			tlsconfig.ClientAuthRequest,
			tlsconfig.ClientAuthRequire,
		} {
			c := tlsconfig.ServerConfig{
				Enabled:      true,
				CertFile:     "server.crt",
				KeyFile:      "server.key",
				ClientCAFile: "client-ca.crt",
				ClientAuth:   policy,
			}

			_, err := c.TLSConfig(nil)
			require.ErrorIs(t, err, tlsconfig.ErrClientCAUnused, policy)
		}
	})

	t.Run("an unknown policy", func(t *testing.T) {
		t.Parallel()

		c := tlsconfig.ServerConfig{
			Enabled:    true,
			CertFile:   "server.crt",
			KeyFile:    "server.key",
			ClientAuth: "mandatory",
		}

		_, err := c.TLSConfig(nil)
		require.ErrorIs(t, err, tlsconfig.ErrUnknownClientAuth)
	})
}

// assertAccepted asserts that both sides of a handshake succeeded, or that one of them failed.
func assertAccepted(t *testing.T, want bool, res handshakeResult) {
	t.Helper()

	err := errors.Join(res.ClientErr, res.ServerErr)

	if want {
		assert.NoError(t, err)
	} else {
		assert.Error(t, err)
	}
}

func TestServerRejectsPairOutsideValidity(t *testing.T) {
	t.Parallel()

	windows := map[string]func() (time.Time, time.Time){
		"expired": func() (time.Time, time.Time) {
			return time.Now().Add(-2 * time.Hour), time.Now().Add(-time.Hour)
		},
		"not valid yet": func() (time.Time, time.Time) {
			return time.Now().Add(time.Hour), time.Now().Add(2 * time.Hour)
		},
	}

	for name, window := range windows {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				s := newSetup(t)
				rec := &recorder{}

				cfg, err := s.cfg.TLSConfig(slog.New(rec))
				require.NoError(t, err)

				notBefore, notAfter := window()
				s.rotate(t, s.ca.serverLeafValid(t, "outside", notBefore, notAfter))

				for range 2 {
					time.Sleep(tlsconfig.DefaultReloadInterval)
					requireServes(t, cfg, s.server)
				}

				warns := rec.matching(slog.LevelWarn, "rejecting tls server key pair")
				require.Len(t, warns, 1)
				assert.Contains(t, warns[0], tlsconfig.ErrCertificateNotCurrent.Error())
				assert.Contains(t, warns[0], "subject=CN=outside")

				next := s.ca.serverLeaf(t, "next")
				s.rotate(t, next)
				time.Sleep(tlsconfig.DefaultReloadInterval)
				requireServes(t, cfg, next)
			})
		})
	}

	t.Run("accepted at the first load", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ca := newAuthority(t, "server-ca")
			expired := ca.serverLeafValid(t, "expired", time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))

			_, err := tlsconfig.ServerConfig{Enabled: true, CertFile: expired.CertFile, KeyFile: expired.KeyFile}.
				TLSConfig(nil)
			require.NoError(t, err)
		})
	})
}

func TestServerBackstop(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newSetup(t)
		clientCA := newAuthority(t, "client-ca")
		s.cfg.ClientCAFile = clientCA.File

		cfg, err := s.cfg.TLSConfig(nil)
		require.NoError(t, err)

		require.NotNil(t, cfg.VerifyPeerCertificate)

		cfg.VerifyConnection = func(tls.ConnectionState) error { return nil }

		assertAccepted(t, true, served(t, cfg, clientCA.clientLeaf(t, "trusted").Pair))
		assertAccepted(t, false, served(t, cfg, newAuthority(t, "other-ca").clientLeaf(t, "other").Pair))
	})
}

func TestServerVerifyConnectionSet(t *testing.T) {
	t.Parallel()

	verifying := map[tlsconfig.ClientAuth]bool{
		tlsconfig.ClientAuthNone:             false,
		tlsconfig.ClientAuthRequest:          false,
		tlsconfig.ClientAuthRequire:          false,
		tlsconfig.ClientAuthVerifyIfGiven:    true,
		tlsconfig.ClientAuthRequireAndVerify: true,
	}

	ca := newAuthority(t, "server-ca")
	server := ca.serverLeaf(t, "server")
	clientCA := newAuthority(t, "client-ca")

	for policy, verifies := range verifying {
		c := tlsconfig.ServerConfig{
			Enabled:    true,
			CertFile:   server.CertFile,
			KeyFile:    server.KeyFile,
			ClientAuth: policy,
		}

		if verifies {
			c.ClientCAFile = clientCA.File
		}

		cfg, err := c.TLSConfig(nil)
		require.NoError(t, err)

		assert.Equal(t, verifies, cfg.VerifyConnection != nil, policy)
		assert.Equal(t, verifies, cfg.VerifyPeerCertificate != nil, policy)
		assert.Nil(t, cfg.ClientCAs, policy)
		assert.Empty(t, cfg.Certificates, policy)
		assert.NotNil(t, cfg.GetCertificate, policy)
	}
}

func TestServerClientCARefresh(t *testing.T) {
	t.Parallel()

	t.Run("a rotated client CA", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			oldCA := newAuthority(t, "old-ca")
			newCA := newAuthority(t, "new-ca")
			oldClient := oldCA.clientLeaf(t, "old")
			newClient := newCA.clientLeaf(t, "new")

			s.cfg.ClientCAFile = filepath.Join(t.TempDir(), "clients.pem")
			copyFile(t, oldCA.File, s.cfg.ClientCAFile)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			assertAccepted(t, true, served(t, cfg, oldClient.Pair))

			copyFile(t, newCA.File, s.cfg.ClientCAFile)
			time.Sleep(tlsconfig.DefaultReloadInterval)

			assertAccepted(t, true, served(t, cfg, newClient.Pair))
			assertAccepted(t, false, served(t, cfg, oldClient.Pair))
		})
	})

	t.Run("a client CA file emptied", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			clientCA := newAuthority(t, "client-ca")
			client := clientCA.clientLeaf(t, "client")
			s.cfg.ClientCAFile = clientCA.File

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			require.NoError(t, os.WriteFile(clientCA.File, []byte("no certificates here\n"), 0o600))
			time.Sleep(tlsconfig.DefaultReloadInterval)

			assertAccepted(t, true, served(t, cfg, client.Pair))
		})
	})

	t.Run("no certificates at the build", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "server-ca")
		server := ca.serverLeaf(t, "server")
		empty := filepath.Join(t.TempDir(), "clients.pem")
		require.NoError(t, os.WriteFile(empty, []byte("no certificates here\n"), 0o600))

		_, err := tlsconfig.ServerConfig{
			Enabled:      true,
			CertFile:     server.CertFile,
			KeyFile:      server.KeyFile,
			ClientCAFile: empty,
		}.TLSConfig(nil)
		require.ErrorIs(t, err, tlsconfig.ErrNoCertificates)
		assert.Contains(t, err.Error(), empty)
	})
}

func TestServerClone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newSetup(t)
		clientCA := newAuthority(t, "client-ca")
		client := clientCA.clientLeaf(t, "client")
		s.cfg.ClientCAFile = clientCA.File

		base, err := s.cfg.TLSConfig(nil)
		require.NoError(t, err)

		cfg := base.Clone()
		cfg.NextProtos = append(cfg.NextProtos, "h2")

		next := s.ca.serverLeaf(t, "next")
		s.rotate(t, next)
		time.Sleep(tlsconfig.DefaultReloadInterval)

		res := handshake(t, &tls.Config{
			InsecureSkipVerify: true,
			Certificates:       []tls.Certificate{client.Pair},
			NextProtos:         []string{"h2"},
		}, cfg, "")
		require.NoError(t, res.ClientErr)
		require.NoError(t, res.ServerErr)
		assert.Equal(t, next.Cert.Raw, res.Client.PeerCertificates[0].Raw)
		assert.Equal(t, "h2", res.Client.NegotiatedProtocol)

		assertAccepted(t, false, served(t, cfg))
	})
}

func TestServerLogging(t *testing.T) {
	t.Parallel()

	t.Run("a successful refresh", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			rec := &recorder{}

			cfg, err := s.cfg.TLSConfig(slog.New(rec))
			require.NoError(t, err)

			loaded := rec.matching(slog.LevelInfo, "tls server key pair loaded")
			require.Len(t, loaded, 1)
			assert.Contains(t, loaded[0], "subject=CN=server")
			assert.Contains(t, loaded[0], "serial="+s.server.Cert.SerialNumber.String())
			assert.Empty(t, rec.matching(slog.LevelInfo, "reloaded"))

			next := s.ca.serverLeaf(t, "next")
			s.rotate(t, next)
			time.Sleep(tlsconfig.DefaultReloadInterval)
			served(t, cfg)

			reloaded := rec.matching(slog.LevelInfo, "reloaded")
			require.Len(t, reloaded, 1)
			assert.Contains(t, reloaded[0], s.server.CertFile)
			assert.Contains(t, reloaded[0], "serial="+next.Cert.SerialNumber.String())
			assert.Empty(t, rec.matching(slog.LevelWarn, ""))

			time.Sleep(tlsconfig.DefaultReloadInterval)
			served(t, cfg)

			assert.Len(t, rec.matching(slog.LevelInfo, "reloaded"), 1)
		})
	})

	t.Run("a pair that keeps failing", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			rec := &recorder{}

			cfg, err := s.cfg.TLSConfig(slog.New(rec))
			require.NoError(t, err)

			copyFile(t, s.ca.serverLeaf(t, "next").CertFile, s.server.CertFile)

			for range 3 {
				time.Sleep(tlsconfig.DefaultReloadInterval)
				requireServes(t, cfg, s.server)
			}

			warns := rec.matching(slog.LevelWarn, "")
			require.Len(t, warns, 1)
			assert.Contains(t, warns[0], s.server.CertFile)
			assert.Empty(t, rec.matching(slog.LevelInfo, "reloaded"))
		})
	})

	t.Run("a read that keeps failing", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			rec := &recorder{}

			cfg, err := s.cfg.TLSConfig(slog.New(rec))
			require.NoError(t, err)

			require.NoError(t, os.Remove(s.server.KeyFile))

			for range 3 {
				time.Sleep(tlsconfig.DefaultReloadInterval)
				requireServes(t, cfg, s.server)
			}

			assert.Len(t, rec.matching(slog.LevelWarn, ""), 3)
		})
	})

	t.Run("no logger", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)

			cfg, err := s.cfg.TLSConfig(nil)
			require.NoError(t, err)

			copyFile(t, s.ca.serverLeaf(t, "broken").CertFile, s.server.CertFile)
			time.Sleep(tlsconfig.DefaultReloadInterval)
			requireServes(t, cfg, s.server)

			next := s.ca.serverLeaf(t, "next")
			s.rotate(t, next)
			time.Sleep(tlsconfig.DefaultReloadInterval)
			requireServes(t, cfg, next)
		})
	})
}

func TestServerOmitsContents(t *testing.T) {
	t.Parallel()

	garbage := []byte("not a key " + marker + "\n")

	t.Run("building", func(t *testing.T) {
		t.Parallel()

		ca := newAuthority(t, "server-ca")
		server := ca.serverLeaf(t, "server")
		require.NoError(t, os.WriteFile(server.KeyFile, garbage, 0o600))

		_, err := tlsconfig.ServerConfig{
			Enabled:  true,
			CertFile: server.CertFile,
			KeyFile:  server.KeyFile,
		}.TLSConfig(
			nil,
		)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), marker)
	})

	t.Run("refreshing", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s := newSetup(t)
			rec := &recorder{}

			cfg, err := s.cfg.TLSConfig(slog.New(rec))
			require.NoError(t, err)

			require.NoError(t, os.WriteFile(s.server.KeyFile, garbage, 0o600))
			time.Sleep(tlsconfig.DefaultReloadInterval)
			requireServes(t, cfg, s.server)

			warns := rec.matching(slog.LevelWarn, "")
			require.Len(t, warns, 1)
			assert.NotContains(t, warns[0], marker)
		})
	})
}

func TestServerConcurrentHandshakes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newSetup(t)

		cfg, err := s.cfg.TLSConfig(nil)
		require.NoError(t, err)

		next := s.ca.serverLeaf(t, "next")
		s.rotate(t, next)
		time.Sleep(tlsconfig.DefaultReloadInterval)

		results := make(chan *x509.Certificate, 8)

		for range cap(results) {
			go func() {
				res := served(t, cfg)

				if res.ClientErr != nil {
					results <- nil

					return
				}

				results <- res.Client.PeerCertificates[0]
			}()
		}

		for range cap(results) {
			cert := <-results
			require.NotNil(t, cert)
			assert.Contains(t, []string{"server", "next"}, cert.Subject.CommonName)
		}

		requireServes(t, cfg, next)
	})
}
