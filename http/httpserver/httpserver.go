package httpserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"go.uber.org/fx"

	"github.com/sikhlana/pkg/config/configvalidator"
)

var (
	// ErrNilHandler is returned by a Provider handed a nil handler, rather than serving http.DefaultServeMux.
	ErrNilHandler = errors.New("nil handler")

	// ErrNotListening is returned by Server.Serve on a server that isn't listening: Listen hasn't bound it, or it's
	// been closed.
	ErrNotListening = errors.New("server is not listening")

	// ErrAlreadyListening is returned by Server.Listen on a server that has already bound a listener, and by
	// Server.Serve on one that's already serving.
	ErrAlreadyListening = errors.New("server is already listening")
)

// state is where a Server is in its life: built, listening, serving, then closed.
type state int

const (
	stateBuilt state = iota
	stateListening
	stateServing
	stateClosed
)

// Server is an HTTP server split into three steps: Listen binds its address, Serve serves on that listener until its
// context ends, and Close releases a listener that was never served. Building one opens no socket. A Server is used
// once; after Close, or once Serve returns, build a new one. Its methods are safe for concurrent use.
type Server struct {
	cfg       Config
	handler   http.Handler
	tlsConfig *tls.Config
	logger    *slog.Logger

	mu       sync.Mutex
	state    state
	listener net.Listener
}

// Provider builds a Server from a Config and the handler it serves. The Config is validated, and with TLS enabled
// its key pair, and any client CAs, are read once to build the TLS configuration, which then re-reads them as they
// rotate; a pair that can't be loaded fails the build. It returns ErrNilHandler for a nil handler. The Server it
// returns is inert until Listen is called.
type Provider func(cfg Config, handler http.Handler) (*Server, error)

// ProviderParams is NewProvider's input.
type ProviderParams struct {
	fx.In

	// Logger receives the servers' records, and net/http's own error reports at Debug. Nil, or none in the graph,
	// discards them.
	Logger *slog.Logger `optional:"true"`
}

// NewProvider returns a Provider whose servers log through p.Logger.
func NewProvider(p ProviderParams) Provider {
	logger := p.Logger

	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return func(cfg Config, handler http.Handler) (*Server, error) {
		if handler == nil {
			return nil, ErrNilHandler
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

// Listen binds the server's address. It returns ErrAlreadyListening once the server has bound a listener, even after
// Close, and net.Listen's error, which names the address, when the bind fails.
func (s *Server) Listen(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != stateBuilt {
		return ErrAlreadyListening
	}

	var lc net.ListenConfig

	l, err := lc.Listen(ctx, "tcp", s.cfg.address())
	if err != nil {
		return err
	}

	s.listener = l
	s.state = stateListening

	s.logger.LogAttrs(ctx, slog.LevelInfo, "http server listening", slog.String("address", l.Addr().String()))

	return nil
}

// Serve serves on the listener Listen bound, over TLS when the Config enables it, until ctx ends. It returns
// ErrNotListening before Listen or after Close, and ErrAlreadyListening while another call is serving.
//
// Once ctx ends, Serve stops accepting connections and waits up to the shutdown timeout for in-flight requests, then
// returns nil. Connections still active when the timeout passes are closed, and Serve returns an error matching
// context.DeadlineExceeded. Any failure of the listener while serving is returned wrapped, and closes every
// connection.
func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()

	switch s.state {
	case stateBuilt, stateClosed:
		s.mu.Unlock()

		return ErrNotListening
	case stateServing:
		s.mu.Unlock()

		return ErrAlreadyListening
	case stateListening:
	}

	s.state = stateServing
	l := s.listener
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.state = stateClosed
		s.mu.Unlock()
	}()

	srv := s.httpServer()
	stop := make(chan struct{})
	drained := make(chan error, 1)

	go func() {
		select {
		case <-ctx.Done():
			drained <- s.drain(ctx, srv)
		case <-stop:
			drained <- nil
		}
	}()

	err := s.serve(srv, l)

	close(stop)

	drainErr := <-drained

	// Shutdown makes Serve return ErrServerClosed at once, so the drain's outcome is the one that counts.
	if errors.Is(err, http.ErrServerClosed) {
		return drainErr
	}

	_ = srv.Close()

	return fmt.Errorf("serving: %w", err)
}

// Close releases the listener Listen bound when Serve never took it over. It's a no-op before Listen, while Serve
// runs, since Serve releases its listener when it returns, and after an earlier Close.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != stateListening {
		return nil
	}

	s.state = stateClosed

	return s.listener.Close()
}

// Addr returns the address the server is bound to, with the real port when the Config asked for port 0, or nil before
// Listen.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener == nil {
		return nil
	}

	return s.listener.Addr()
}

// httpServer returns the net/http server Serve runs, with the Config's timeouts and net/http's error reports logged
// at Debug.
func (s *Server) httpServer() *http.Server {
	return &http.Server{
		Handler:           s.handler,
		TLSConfig:         s.tlsConfig,
		ReadTimeout:       s.cfg.readTimeout(),
		ReadHeaderTimeout: s.cfg.readHeaderTimeout(),
		WriteTimeout:      s.cfg.writeTimeout(),
		IdleTimeout:       s.cfg.idleTimeout(),
		ErrorLog:          slog.NewLogLogger(s.logger.Handler(), slog.LevelDebug),
	}
}

// serve runs srv on l until it's shut down or l fails.
func (s *Server) serve(srv *http.Server, l net.Listener) error {
	if s.tlsConfig != nil {
		// Empty file names make ServeTLS use the TLS configuration's GetCertificate, which re-reads rotated files.
		return srv.ServeTLS(l, "", "")
	}

	return srv.Serve(l)
}

// drain shuts srv down within the shutdown timeout, closing whatever connections are left when it passes.
func (s *Server) drain(ctx context.Context, srv *http.Server) error {
	s.logger.LogAttrs(ctx, slog.LevelInfo, "http server draining")

	// ctx is already done; the drain needs its values but not its cancellation.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.shutdownTimeout())
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()

		return fmt.Errorf("draining connections: %w", err)
	}

	s.logger.LogAttrs(ctx, slog.LevelDebug, "http server stopped")

	return nil
}
