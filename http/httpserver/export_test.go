package httpserver

import (
	"context"
	"net/http"
	"time"
)

// HTTPServer exposes the net/http server Serve would run, so tests can read the timeouts it resolved.
func HTTPServer(ctx context.Context, s *Server) *http.Server {
	return s.httpServer(ctx)
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

// DropCertificates removes s's certificate source, so ServeTLS fails before it starts serving.
func DropCertificates(s *Server) {
	s.tlsConfig.GetCertificate = nil
}
