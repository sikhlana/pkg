package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// SignalError is the cause of a context that SignalContext cancelled because a signal arrived. errors.Is matches it
// against context.Canceled, as it does the context's Err.
type SignalError struct {
	// Signal is the signal that cancelled the context.
	Signal os.Signal
}

// Error returns the signal's name followed by "signal received", as in "interrupt signal received".
func (e *SignalError) Error() string {
	return e.Signal.String() + " signal received"
}

// Is reports whether target is context.Canceled.
func (e *SignalError) Is(target error) bool {
	return target == context.Canceled
}

// SignalContext returns a copy of parent that's cancelled on the first SIGINT or SIGTERM, when parent is done, or
// when stop is called, whichever happens first. When a signal cancels it, context.Cause returns a *SignalError
// naming the signal.
//
// SignalContext stops relaying those signals before the context is done, so a second SIGINT or SIGTERM, even one that
// arrives while the program is still shutting down, gets the action the process started with. That's normally the
// default action, which ends the process. A process started with the signal ignored, such as a background job in a
// script, goes back to ignoring it. Anything else in the process that subscribed to the same signal with
// signal.Notify keeps receiving it, and then the signal doesn't end the process either.
//
// Call stop when the context is no longer needed, typically with defer in main. It's safe to call more than once.
func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)

	signals := make(chan os.Signal, 1)

	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-signals:
			// Released before cancelling, so whatever reacts to the cancellation already sees the original action.
			signal.Stop(signals)
			cancel(&SignalError{Signal: sig})
		case <-ctx.Done():
			signal.Stop(signals)
		}
	}()

	stop := func() {
		signal.Stop(signals)
		cancel(nil)
	}

	return ctx, stop
}
