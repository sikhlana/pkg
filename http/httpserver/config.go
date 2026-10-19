package httpserver

import (
	"cmp"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/sikhlana/pkg/config/configvalidator"
	"github.com/sikhlana/pkg/tls/tlsconfig"
)

const (
	// DefaultReadTimeout bounds reading a whole request, body included, when ReadTimeout is zero.
	DefaultReadTimeout = 5 * time.Second

	// DefaultReadHeaderTimeout bounds reading a request's headers when ReadHeaderTimeout is zero.
	DefaultReadHeaderTimeout = 1 * time.Second

	// DefaultWriteTimeout bounds writing a response when WriteTimeout is zero.
	DefaultWriteTimeout = 10 * time.Second

	// DefaultIdleTimeout bounds the wait for a keep-alive connection's next request when IdleTimeout is zero.
	DefaultIdleTimeout = 60 * time.Second

	// DefaultShutdownTimeout bounds draining in-flight requests when ShutdownTimeout is zero.
	DefaultShutdownTimeout = 10 * time.Second
)

// ErrNegativeTimeout reports a negative timeout. The error carries the timeout's key.
var ErrNegativeTimeout = errors.New("timeout must not be negative")

// Config is a server's operator settings. Zero timeouts mean their Default* constants.
type Config struct {
	// Host is the hostname or IP address the server listens on. Empty means every interface.
	Host string `mapstructure:"host" validate:"omitempty,hostname_rfc1123|ip"`

	// Port is the TCP port the server listens on. Zero picks a free port, which Server.Addr reports once bound.
	Port int `mapstructure:"port" validate:"gte=0,lte=65535"`

	// ReadTimeout bounds reading a whole request, body included. Zero means DefaultReadTimeout.
	ReadTimeout time.Duration `mapstructure:"read_timeout"`

	// ReadHeaderTimeout bounds reading a request's headers. Zero means DefaultReadHeaderTimeout.
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`

	// WriteTimeout bounds writing a response, from the end of the request's headers. Zero means DefaultWriteTimeout.
	WriteTimeout time.Duration `mapstructure:"write_timeout"`

	// IdleTimeout bounds the wait for a keep-alive connection's next request. Zero means DefaultIdleTimeout.
	IdleTimeout time.Duration `mapstructure:"idle_timeout"`

	// ShutdownTimeout bounds draining in-flight requests once the server is told to stop; connections still active
	// after it are closed. Zero means DefaultShutdownTimeout.
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`

	// TLS serves HTTPS, with HTTP/2, when enabled, re-reading the key pair and client CAs as they rotate.
	TLS tlsconfig.ServerConfig `mapstructure:"tls"`
}

// Validate returns every problem with the Config, joined, each under its config key: ErrNegativeTimeout for each
// negative timeout, and the TLS settings' own errors. The host and port are checked by their validate tags, which
// configvalidator.Validate runs. It reads no files.
func (c Config) Validate() error {
	var errs []error

	timeouts := []struct {
		key   string
		value time.Duration
	}{
		{"read_timeout", c.ReadTimeout},
		{"read_header_timeout", c.ReadHeaderTimeout},
		{"write_timeout", c.WriteTimeout},
		{"idle_timeout", c.IdleTimeout},
		{"shutdown_timeout", c.ShutdownTimeout},
	}

	for _, t := range timeouts {
		if t.value < 0 {
			errs = append(errs, &configvalidator.Error{Key: t.key, Err: ErrNegativeTimeout})
		}
	}

	errs = append(errs, configvalidator.ValidateField(&c, &c.TLS))

	return errors.Join(errs...)
}

// address returns Host and Port as net.Listen takes them.
func (c Config) address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func (c Config) readTimeout() time.Duration {
	return cmp.Or(c.ReadTimeout, DefaultReadTimeout)
}

func (c Config) readHeaderTimeout() time.Duration {
	return cmp.Or(c.ReadHeaderTimeout, DefaultReadHeaderTimeout)
}

func (c Config) writeTimeout() time.Duration {
	return cmp.Or(c.WriteTimeout, DefaultWriteTimeout)
}

func (c Config) idleTimeout() time.Duration {
	return cmp.Or(c.IdleTimeout, DefaultIdleTimeout)
}

func (c Config) shutdownTimeout() time.Duration {
	return cmp.Or(c.ShutdownTimeout, DefaultShutdownTimeout)
}
