package kernel

import (
	stdlog "log" //nolint:depguard // Run restores the log package's output, which slog.SetDefault redirects.
	"log/slog"
)

// swapDefaults saves slog's default logger, the log package's output and flags, and slog's level for what the log
// package writes, raises that level to Warn, and returns a function that puts all four back.
//
// The log package has no levels, and what libraries write through it is mostly error reports (net/http's ErrorLog,
// say), so Warn keeps them under --quiet. slog.SetDefault redirects the log package's output to the new logger's
// handler, and setting back a default that writes through the log package doesn't undo that, so the output is restored
// on its own.
func swapDefaults() func() {
	logger, writer, flags := slog.Default(), stdlog.Writer(), stdlog.Flags()
	level := slog.SetLogLoggerLevel(slog.LevelWarn)

	return func() {
		slog.SetDefault(logger)
		stdlog.SetOutput(writer)
		stdlog.SetFlags(flags)
		slog.SetLogLoggerLevel(level)
	}
}
