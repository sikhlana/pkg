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
)

var (
	// ErrNotListening is returned by Server.Serve on a server that isn't listening: Listen hasn't bound it, or it's
	// been closed.
	ErrNotListening = errors.New("server is not listening")

	// ErrAlreadyListening is returned by Server.Listen on a server that has already bound a listener.
	ErrAlreadyListening = errors.New("server is already listening")

	// ErrAlreadyServing is returned by Server.Serve while another call is serving the same server.
	ErrAlreadyServing = errors.New("server is already serving")

	// ErrShutdownTimeout is returned by Server.Serve when connections were still active once the shutdown timeout
	// passed, and were closed. It doesn't match context.DeadlineExceeded, so a run that ended by deadline still
	// reports the requests it cut.
	ErrShutdownTimeout = errors.New("shutdown timeout exceeded")
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
// ErrNotListening before Listen or after Close, and ErrAlreadyServing while another call is serving.
//
// Requests' contexts carry ctx's values, but not its cancellation: ending ctx starts the drain rather than cancelling
// the requests in flight.
//
// Once ctx ends, Serve stops accepting connections and waits up to the shutdown timeout for in-flight requests, then
// returns nil. Connections still active when the timeout passes are closed, and Serve returns an error matching
// ErrShutdownTimeout. Any failure of the listener while serving is returned wrapped, and closes the listener and
// every connection.
func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()

	switch s.state {
	case stateBuilt, stateClosed:
		s.mu.Unlock()

		return ErrNotListening
	case stateServing:
		s.mu.Unlock()

		return ErrAlreadyServing
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

	srv := s.httpServer(ctx)
	drained := make(chan error, 1)
	stopDrain := context.AfterFunc(ctx, func() { drained <- s.drain(ctx, srv) })

	err := s.serve(srv, l)

	// Only the drain's Shutdown makes serve return ErrServerClosed, so the drain's outcome is the one that counts.
	if errors.Is(err, http.ErrServerClosed) {
		return <-drained
	}

	// ServeTLS can fail before handing l to Serve, which is what would otherwise close it. Closing srv first also
	// ends at once a drain that ctx started meanwhile, rather than waiting out the shutdown timeout for it.
	_ = l.Close()
	_ = srv.Close()

	var drainErr error

	if !stopDrain() {
		drainErr = <-drained
	}

	return fmt.Errorf("serving: %w", errors.Join(err, drainErr))
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

// httpServer returns the net/http server Serve runs, with the Config's timeouts, net/http's error reports logged at
// Debug, and requests' contexts carrying ctx's values without its cancellation.
func (s *Server) httpServer(ctx context.Context) *http.Server {
	base := context.WithoutCancel(ctx)

	return &http.Server{
		Handler:           s.handler,
		TLSConfig:         s.tlsConfig,
		ReadTimeout:       s.cfg.readTimeout(),
		ReadHeaderTimeout: s.cfg.readHeaderTimeout(),
		WriteTimeout:      s.cfg.writeTimeout(),
		IdleTimeout:       s.cfg.idleTimeout(),
		ErrorLog:          slog.NewLogLogger(s.logger.Handler(), slog.LevelDebug),
		BaseContext:       func(net.Listener) context.Context { return base },
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
	timeout := s.cfg.shutdownTimeout()

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	err := srv.Shutdown(shutdownCtx)

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		_ = srv.Close()

		return fmt.Errorf("%w: connections still active after %s", ErrShutdownTimeout, timeout)
	case err != nil:
		_ = srv.Close()

		return fmt.Errorf("shutting down: %w", err)
	}

	s.logger.LogAttrs(ctx, slog.LevelDebug, "http server stopped")

	return nil
}

// Spec is what a ProvideServer constructor says the server serves: its Config and its handler.
type Spec struct {
	Config  Config
	Handler http.Handler
}

// NameTag returns the fx name tag ProvideServer provides the server name under, for injecting it with fx.ParamTags or
// a name struct tag.
func NameTag(name string) string {
	return fmt.Sprintf(`name:"httpserver:server:%s"`, name)
}

// builtSpec names a ProvideServer constructor's result inside the private module that builds the server from it.
const builtSpec = `name:"httpserver:spec::built"`

// ProvideServer provides the server name, injectable under NameTag(name), built from the Spec constructor returns.
// constructor can take any parameters fx can supply, and returns a Spec, optionally followed by an error. ProvideServer
// wraps the Spec's handler in the middleware in MiddlewareGroup(name), ordered as Chain orders them, then builds the
// server by calling the app's Provider with WithName(name), so the name is on every record the server logs. A
// decoration of the Provider applies to the server, and a handler it wraps already runs the middleware. A Spec with a
// nil handler fails startup with an error matching ErrNilHandler that names the server.
//
// Providing a server doesn't run it, and nothing is built until something injects it: list ProvideRunner(name) in
// the commands that serve it. A name provided twice fails startup, and an invalid one with ErrInvalidName.
//
// anns are applied to constructor after ProvideServer's own result tag, for its inputs and hooks: fx.ParamTags to
// take a tagged dependency, fx.From, fx.OnStart, fx.OnStop. One that tags or converts the result fails startup.
func ProvideServer(name string, constructor any, anns ...fx.Annotation) fx.Option {
	if err := checkName(name); err != nil {
		return fx.Error(err)
	}

	return fx.Module(
		"httpserver",
		fx.Provide(
			fx.Private,
			fx.Annotate(constructor, append([]fx.Annotation{fx.ResultTags(builtSpec)}, anns...)...),
		),
		fx.Provide(fx.Annotate(
			func(spec Spec, p Provider, mws []Middleware) (*Server, error) {
				if spec.Handler == nil {
					return nil, fmt.Errorf("%w: server %q", ErrNilHandler, name)
				}

				return p(spec.Config, Chain(spec.Handler, mws...), WithName(name))
			},
			fx.ParamTags(builtSpec, "", MiddlewareGroup(name)),
			fx.ResultTags(NameTag(name)),
		)),
		fx.Supply(fx.Annotate(name, fx.ResultTags(serverNames))),
	)
}
