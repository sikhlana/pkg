package httpserver

import "errors"

var (
	// ErrNegativeTimeout reports a negative timeout. The error carries the timeout's key.
	ErrNegativeTimeout = errors.New("timeout must not be negative")

	// ErrNilHandler is returned by a Provider handed a nil handler, or a nil pointer or func behind the interface,
	// rather than serving http.DefaultServeMux, and fails startup for a ProvideServer constructor whose Spec has none,
	// naming the server.
	ErrNilHandler = errors.New("nil handler")

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

	// ErrUnknownServer fails startup for a ProvideMiddleware or ProvideRunner naming no server ProvideServer provided.
	// The error carries the name.
	ErrUnknownServer = errors.New("unknown server")

	// ErrInvalidName fails startup for a ProvideServer, ProvideMiddleware or ProvideRunner given an empty name, or one
	// with a character other than a letter, a digit, "-", "_" or ".". The error carries the name.
	ErrInvalidName = errors.New("invalid server name")
)
