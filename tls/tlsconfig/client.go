package tlsconfig

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"time"
)

// TLSConfig returns the TLS configuration for a client to dial with, or nil and no error when the ClientConfig is
// disabled. It runs Validate, then reads CAFile and the key pair, returning errors that name the path and never a
// file's contents.
//
// The returned configuration keeps both current as their files rotate: during a handshake, once ReloadInterval has
// passed since the last check, the files are re-read and changed contents that parse are swapped in. The key pair is
// checked during handshakes in which the server asks for a certificate. A refresh that fails, or that brings a key pair
// whose leaf has expired or isn't valid yet (ErrCertificateNotCurrent), keeps the last good value. Each value is logged
// at Info when it's loaded and when it's swapped, a key pair with its leaf's subject, serial and expiry, and failures
// at Warn, through logger; a nil logger discards them. Nothing needs starting, and copies made with Clone keep
// refreshing.
//
// The key pair is presented through GetClientCertificate only, so Certificates must stay empty. As with Certificates,
// it isn't sent when the server's request doesn't accept it, by signature scheme or by the CAs the server lists. With a
// CAFile, the server's certificate is verified in VerifyConnection, against the current CAs and the connection's server
// name, because crypto/tls can't swap RootCAs per handshake. The connection's server name is empty for an IP address,
// so ServerName is used then: unlike native verification, a client dialling an IP address must set ServerName to it, or
// every handshake fails with ErrNoServerName. InsecureSkipVerify is set to turn native verification off, and RootCAs
// holds the CAs first read. Wrap VerifyConnection rather than replace it. VerifyPeerCertificate checks the chain too,
// as a backstop, but not the server name. Both check expiry against the Time of the configuration returned here, read
// at each handshake: set Time on this configuration, not on a copy. ConnectionState.VerifiedChains stays empty; once
// the handshake succeeds, ConnectionState.PeerCertificates holds the verified server chain.
func (c ClientConfig) TLSConfig(logger *slog.Logger) (*tls.Config, error) {
	if !c.Enabled {
		return nil, nil //nolint:nilnil // nil is the "no TLS settings" every consumer understands
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	cfg := &tls.Config{
		ServerName:         c.ServerName,
		InsecureSkipVerify: c.InsecureSkipVerify, //nolint:gosec // operator opt-in for development, documented as unsafe
	}

	if c.CAFile != "" && !c.InsecureSkipVerify {
		if err := c.verifyWithCAFile(cfg, logger); err != nil {
			return nil, err
		}
	}

	if c.CertFile == "" {
		return cfg, nil
	}

	pair, err := newReloader(
		"tls client key pair",
		[]string{c.CertFile, c.KeyFile},
		[]slog.Attr{slog.String("cert_file", c.CertFile), slog.String("key_file", c.KeyFile)},
		keyPairFormat,
		c.reloadInterval(),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("loading key pair %s and %s: %w", c.CertFile, c.KeyFile, err)
	}

	cfg.GetClientCertificate = func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		cert := pair.get(info.Context())

		// crypto/tls sends no certificate when none in Certificates suits the request, so a server that only asks
		// for one can still complete the handshake. The hook replaces that choice, so it makes it too.
		if supported := info.SupportsCertificate(cert) == nil; !supported {
			return new(tls.Certificate), nil
		}

		return cert, nil
	}

	return cfg, nil
}

// verifyWithCAFile sets cfg up to verify servers in its hooks, against CAFile's current CAs.
func (c ClientConfig) verifyWithCAFile(cfg *tls.Config, logger *slog.Logger) error {
	cas, err := newReloader(
		"tls CAs",
		[]string{c.CAFile},
		[]slog.Attr{slog.String("ca_file", c.CAFile)},
		poolFormat,
		c.reloadInterval(),
		logger,
	)
	if err != nil {
		return fmt.Errorf("loading ca_file %s: %w", c.CAFile, err)
	}

	// Native verification can't see rotated CAs, so it's off and the hooks below verify instead. RootCAs keeps the
	// first CAs for a copy that turns it back on.
	cfg.RootCAs = cas.get(context.Background())
	cfg.InsecureSkipVerify = true

	// crypto/tls leaves ConnectionState.ServerName empty when the name is an IP address, since SNI can't carry one,
	// so server_name stands in. A dialled IP address can't: tls.Dial and http.Transport set it on a copy of the
	// configuration this hook never sees.
	serverName := c.ServerName

	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		name := cs.ServerName

		if name == "" {
			name = serverName
		}

		if name == "" {
			return ErrNoServerName
		}

		return verifyServer(cs.PeerCertificates, cas.get(context.Background()), name, cfg.Time)
	}

	//nolint:gosec // VerifyConnection, set above, covers resumed sessions; this is the backstop for full ones
	cfg.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
		certs, parseErr := parseCertificates(raw)
		if parseErr != nil {
			return parseErr
		}

		return verifyServer(certs, cas.get(context.Background()), "", cfg.Time)
	}

	return nil
}

// verifyServer verifies the server's chain against roots and, when name isn't empty, the leaf against name.
func verifyServer(certs []*x509.Certificate, roots *x509.CertPool, name string, now func() time.Time) error {
	if len(certs) == 0 {
		return ErrNoServerCertificate
	}

	if err := verifyChain(certs, roots, name, x509.ExtKeyUsageServerAuth, now); err != nil {
		return fmt.Errorf("verifying server certificate: %w", err)
	}

	return nil
}
