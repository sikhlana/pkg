package tlsconfig

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"time"
)

// TLSConfig returns the TLS configuration for a server to serve with, or nil and no error when the ServerConfig is
// disabled. It runs Validate, then reads the key pair and, for a verifying policy, the client CAs, returning errors
// that name the path and never a file's contents.
//
// The returned configuration keeps both current as their files rotate: during a handshake, once ReloadInterval has
// passed since the last check, the files are re-read and changed contents that parse are swapped in. A refresh that
// fails, or that brings a key pair whose leaf has expired or isn't valid yet (ErrCertificateNotCurrent), keeps the last
// good value. Each value is logged at Info when it's loaded and when it's swapped, a key pair with its leaf's subject,
// serial and expiry, and failures at Warn, through logger; a nil logger discards them. Nothing needs starting, and
// copies made with Clone keep refreshing.
//
// The key pair is served through GetCertificate only, so Certificates must stay empty. For a verifying policy,
// VerifyConnection checks client certificates against the current client CAs: wrap it rather than replace it.
// VerifyPeerCertificate checks them too, as a backstop, so replacing VerifyConnection still rejects a certificate no
// current client CA issued. It checks expiry against the Time of the configuration returned here, read at each
// handshake, since a handshake doesn't say which copy it's using: set Time on this configuration, not on a copy.
// ClientCAs stays nil and ConnectionState.VerifiedChains stays empty; once the handshake succeeds,
// ConnectionState.PeerCertificates holds the verified client chain.
func (c ServerConfig) TLSConfig(logger *slog.Logger) (*tls.Config, error) {
	if !c.Enabled {
		return nil, nil //nolint:nilnil // nil is the "no TLS settings" every consumer understands
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	pair, err := newReloader(
		"tls server key pair",
		[]string{c.CertFile, c.KeyFile},
		[]slog.Attr{slog.String("cert_file", c.CertFile), slog.String("key_file", c.KeyFile)},
		keyPairFormat,
		c.reloadInterval(),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("loading key pair %s and %s: %w", c.CertFile, c.KeyFile, err)
	}

	cfg := &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return pair.get(hello.Context()), nil
		},
	}

	policy := c.clientAuth()

	switch policy {
	case ClientAuthNone:
		cfg.ClientAuth = tls.NoClientCert
	case ClientAuthRequest, ClientAuthVerifyIfGiven:
		cfg.ClientAuth = tls.RequestClientCert
	case ClientAuthRequire, ClientAuthRequireAndVerify:
		cfg.ClientAuth = tls.RequireAnyClientCert
	}

	if !policy.verifies() {
		return cfg, nil
	}

	cas, err := newReloader(
		"tls client CAs",
		[]string{c.ClientCAFile},
		[]slog.Attr{slog.String("client_ca_file", c.ClientCAFile)},
		poolFormat,
		c.reloadInterval(),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("loading client_ca_file %s: %w", c.ClientCAFile, err)
	}

	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		return verifyClient(cs.PeerCertificates, cas.get(context.Background()), cfg.Time)
	}

	//nolint:gosec // VerifyConnection, set above, covers resumed sessions; this is the backstop for full ones
	cfg.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
		certs, parseErr := parseCertificates(raw)
		if parseErr != nil {
			return parseErr
		}

		return verifyClient(certs, cas.get(context.Background()), cfg.Time)
	}

	return cfg, nil
}

// verifyClient verifies the client's chain, when it presented one, against roots.
func verifyClient(certs []*x509.Certificate, roots *x509.CertPool, now func() time.Time) error {
	if len(certs) == 0 {
		return nil
	}

	if err := verifyChain(certs, roots, "", x509.ExtKeyUsageClientAuth, now); err != nil {
		return fmt.Errorf("verifying client certificate: %w", err)
	}

	return nil
}
