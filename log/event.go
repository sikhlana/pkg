package log

import (
	"log/slog"

	"go.uber.org/fx/fxevent"
)

// NewEventLogger returns an fx event logger that writes fx's events through logger at Debug and its failures at
// Error, so the logger's level decides what shows: everything under debug, only failures otherwise. Pass it to
// fx.WithLogger at the top level of the app; inside an fx.Module it only sees that module's events.
func NewEventLogger(logger *slog.Logger) fxevent.Logger {
	l := &fxevent.SlogLogger{Logger: logger}
	l.UseLogLevel(slog.LevelDebug)

	return l
}
