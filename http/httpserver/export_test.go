package httpserver

import (
	"net/http"
	"time"
)

// HTTPServer exposes the net/http server Serve would run, so tests can read the timeouts it resolved.
func HTTPServer(s *Server) *http.Server {
	return s.httpServer()
}

// ShutdownTimeout exposes the shutdown timeout s resolved from its Config.
func ShutdownTimeout(s *Server) time.Duration {
	return s.cfg.shutdownTimeout()
}

// BreakListener closes s's listener behind net/http's back, so a serving server sees its listener fail.
func BreakListener(s *Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.listener.Close()
}
