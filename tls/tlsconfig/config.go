package tlsconfig

import (
	"errors"
	"time"
)

// DefaultReloadInterval is how often key pair and CA files are checked for changes when ReloadInterval is zero.
const DefaultReloadInterval = 30 * time.Second

// ClientAuth is a server's policy for client certificates.
type ClientAuth string

const (
	// ClientAuthNone doesn't ask clients for a certificate.
	ClientAuthNone ClientAuth = "none"

	// ClientAuthRequest asks for a certificate, but neither requires nor verifies one.
	ClientAuthRequest ClientAuth = "request"

	// ClientAuthRequire requires a certificate, but doesn't verify it.
	ClientAuthRequire ClientAuth = "require"

	// ClientAuthVerifyIfGiven asks for a certificate, and verifies it against the client CAs when one is presented.
	ClientAuthVerifyIfGiven ClientAuth = "verify_if_given"

	// ClientAuthRequireAndVerify requires a certificate, and verifies it against the client CAs.
	ClientAuthRequireAndVerify ClientAuth = "require_and_verify"
)

var (
	// ErrIncompleteKeyPair reports an enabled ClientConfig with only one of cert_file and key_file set.
	ErrIncompleteKeyPair = errors.New("cert_file and key_file must be set together")

	// ErrMissingKeyPair reports an enabled ServerConfig without both cert_file and key_file.
	ErrMissingKeyPair = errors.New("cert_file and key_file are required when enabled")

	// ErrUnknownClientAuth reports a client_auth other than the ClientAuth constants.
	ErrUnknownClientAuth = errors.New("unknown client_auth")

	// ErrClientCARequired reports a verifying client_auth without a client_ca_file.
	ErrClientCARequired = errors.New("client_ca_file is required to verify client certificates")

	// ErrClientCAUnused reports a client_ca_file beside a client_auth that doesn't verify, which would never read it.
	ErrClientCAUnused = errors.New("client_ca_file is only used by verify_if_given and require_and_verify")

	// ErrNegativeReloadInterval reports a negative reload_interval.
	ErrNegativeReloadInterval = errors.New("reload_interval must not be negative")

	// ErrNoCertificates reports a CA file that holds no PEM certificates.
	ErrNoCertificates = errors.New("no certificates in file")

	// ErrNoServerName fails a client's handshake with no server name to verify the server's certificate against: with
	// a CAFile, the client dialled an IP address, or nothing, without ServerName.
	ErrNoServerName = errors.New("no server name to verify the server certificate against: set server_name, " +
		"to the server's IP address when dialling one")

	// ErrNoServerCertificate fails a client's handshake with a server that presented no certificate.
	ErrNoServerCertificate = errors.New("server presented no certificate")

	// ErrCertificateNotCurrent rejects a refreshed key pair whose leaf has expired or isn't valid yet. It's logged, not
	// returned: the last good pair stays.
	ErrCertificateNotCurrent = errors.New("certificate outside its validity period")
)

// ClientConfig is a client's operator TLS settings, for a client module to embed in its own Config. Every field but
// Enabled is ignored while Enabled is false, so an operator can switch TLS off without removing the paths.
type ClientConfig struct {
	// Enabled turns TLS on. When false, TLSConfig returns nil.
	Enabled bool `mapstructure:"enabled"`

	// CAFile names a PEM file whose certificates replace the system roots for verifying the server. Re-read as it
	// rotates.
	CAFile string `mapstructure:"ca_file"`

	// ServerName is the name the server's certificate is verified against, and sent as SNI, in place of the dialled
	// host.
	ServerName string `mapstructure:"server_name"`

	// InsecureSkipVerify accepts any server certificate. For development only.
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`

	// CertFile names a PEM certificate the client presents to servers that ask for one. Set with KeyFile. Re-read as
	// it rotates.
	CertFile string `mapstructure:"cert_file"`

	// KeyFile names the PEM private key for CertFile. Set with CertFile. Re-read as it rotates.
	KeyFile string `mapstructure:"key_file"`

	// ReloadInterval is the least time between checks of the CA and key pair files for changes. Zero means
	// DefaultReloadInterval.
	ReloadInterval time.Duration `mapstructure:"reload_interval"`
}

// Validate returns every problem with an enabled ClientConfig, joined: ErrIncompleteKeyPair and
// ErrNegativeReloadInterval. A disabled ClientConfig is always valid. It reads no files.
func (c ClientConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	var errs []error

	if (c.CertFile == "") != (c.KeyFile == "") {
		errs = append(errs, ErrIncompleteKeyPair)
	}

	if c.ReloadInterval < 0 {
		errs = append(errs, ErrNegativeReloadInterval)
	}

	return errors.Join(errs...)
}

// reloadInterval returns ReloadInterval, or DefaultReloadInterval when it's zero.
func (c ClientConfig) reloadInterval() time.Duration {
	return resolveInterval(c.ReloadInterval)
}

// ServerConfig is a server's operator TLS settings, for a server module to embed in its own Config. Every field but
// Enabled is ignored while Enabled is false, so an operator can switch TLS off without removing the paths.
type ServerConfig struct {
	// Enabled turns TLS on. When false, TLSConfig returns nil.
	Enabled bool `mapstructure:"enabled"`

	// CertFile names the server's PEM certificate, with any intermediates after it. Re-read as it rotates.
	CertFile string `mapstructure:"cert_file"`

	// KeyFile names the PEM private key for CertFile. Re-read as it rotates.
	KeyFile string `mapstructure:"key_file"`

	// ClientCAFile names a PEM file of the CAs that client certificates must chain to. Re-read as it rotates.
	ClientCAFile string `mapstructure:"client_ca_file"`

	// ClientAuth is the client certificate policy. Unset means ClientAuthRequireAndVerify when ClientCAFile is set,
	// and ClientAuthNone otherwise.
	ClientAuth ClientAuth `mapstructure:"client_auth"`

	// ReloadInterval is the least time between checks of the files for changes. Zero means DefaultReloadInterval.
	ReloadInterval time.Duration `mapstructure:"reload_interval"`
}

// Validate returns every problem with an enabled ServerConfig, joined: ErrMissingKeyPair, ErrNegativeReloadInterval,
// ErrUnknownClientAuth, ErrClientCARequired and ErrClientCAUnused. A disabled ServerConfig is always valid. It reads no
// files.
func (c ServerConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	var errs []error

	if c.CertFile == "" || c.KeyFile == "" {
		errs = append(errs, ErrMissingKeyPair)
	}

	if c.ReloadInterval < 0 {
		errs = append(errs, ErrNegativeReloadInterval)
	}

	switch c.clientAuth() {
	case ClientAuthNone, ClientAuthRequest, ClientAuthRequire:
		if c.ClientCAFile != "" {
			errs = append(errs, ErrClientCAUnused)
		}
	case ClientAuthVerifyIfGiven, ClientAuthRequireAndVerify:
		if c.ClientCAFile == "" {
			errs = append(errs, ErrClientCARequired)
		}
	default:
		errs = append(errs, ErrUnknownClientAuth)
	}

	return errors.Join(errs...)
}

// reloadInterval returns ReloadInterval, or DefaultReloadInterval when it's zero.
func (c ServerConfig) reloadInterval() time.Duration {
	return resolveInterval(c.ReloadInterval)
}

// clientAuth returns ClientAuth, or the policy an unset one means.
func (c ServerConfig) clientAuth() ClientAuth {
	switch {
	case c.ClientAuth != "":
		return c.ClientAuth
	case c.ClientCAFile != "":
		return ClientAuthRequireAndVerify
	default:
		return ClientAuthNone
	}
}

// verifies reports whether the policy verifies client certificates.
func (a ClientAuth) verifies() bool {
	return a == ClientAuthVerifyIfGiven || a == ClientAuthRequireAndVerify
}

func resolveInterval(d time.Duration) time.Duration {
	if d == 0 {
		return DefaultReloadInterval
	}

	return d
}
