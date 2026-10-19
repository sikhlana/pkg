package httpserver

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"go.uber.org/fx"

	"github.com/sikhlana/pkg/config/configvalidator"
)

// ErrNilHandler is returned by a Provider handed a nil handler, rather than serving http.DefaultServeMux, and fails
// startup for a ProvideServer constructor whose Spec has none, naming the server.
var ErrNilHandler = errors.New("nil handler")

// Provider builds a Server from a Config and the handler it serves. The Config is validated, and with TLS enabled
// its key pair, and any client CAs, are read once to build the TLS configuration, which then re-reads them as they
// rotate; a pair that can't be loaded fails the build. It returns ErrNilHandler for a nil handler. The Server it
// returns is inert until Listen is called. opts set per-build settings, such as WithName; a decoration of a Provider
// passes them on to keep them.
type Provider func(cfg Config, handler http.Handler, opts ...Option) (*Server, error)

// ProviderParams is NewProvider's input.
type ProviderParams struct {
	fx.In

	// Logger receives the servers' records, and net/http's own error reports at Debug. Nil, or none in the graph,
	// discards them.
	Logger *slog.Logger `optional:"true"`
}

// NewProvider returns a Provider whose servers log through p.Logger.
func NewProvider(p ProviderParams) Provider {
	base := p.Logger

	if base == nil {
		base = slog.New(slog.DiscardHandler)
	}

	return func(cfg Config, handler http.Handler, opts ...Option) (*Server, error) {
		if handler == nil {
			return nil, ErrNilHandler
		}

		var o options

		for _, opt := range opts {
			opt(&o)
		}

		logger := base

		if o.name != "" {
			logger = logger.With(slog.String("server", o.name))
		}

		if err := configvalidator.Validate(&cfg); err != nil {
			return nil, fmt.Errorf("validating config: %w", err)
		}

		tlsConfig, err := cfg.TLS.TLSConfig(logger)
		if err != nil {
			return nil, fmt.Errorf("building tls config: %w", err)
		}

		return &Server{cfg: cfg, handler: handler, tlsConfig: tlsConfig, logger: logger}, nil
	}
}
