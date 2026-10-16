package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// SignalContext returns a copy of parent that's cancelled on the first SIGINT or SIGTERM, when parent is done, or
// when stop is called, whichever happens first. When a signal cancels it, context.Cause returns an error that names
// the signal.
//
// Once the context is done, SignalContext stops relaying those signals, so a second SIGINT or SIGTERM gets the
// operating system's default action and ends the process, even while the program is still shutting down. That
// doesn't hold if anything else in the process has subscribed to the same signal with signal.Notify: its
// subscription keeps the signal from reaching the default action.
//
// Call stop when the context is no longer needed, typically with defer in main. It's safe to call more than once.
func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)

	context.AfterFunc(ctx, stop)

	return ctx, stop
}
