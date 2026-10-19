package tlsconfig

import "errors"

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
