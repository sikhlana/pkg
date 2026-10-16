package runerr

import (
	"context"
	"errors"
)

// ErrNotStarted is the run's outcome an OnStop hook reads while fx rolls back a start that failed: no runner ran.
// kernelrun.Run attaches it to the context its OnStart hooks get, which fx passes on to the OnStop hooks it rolls back.
var ErrNotStarted = errors.New("the app didn't start")

// key is the context key With stores the run's outcome under.
type key struct{}

// With returns a copy of ctx carrying err as the run's outcome. kernelrun.Run attaches the outcome to the context its
// OnStop hooks get; tests call With to hand a hook an outcome of their choosing.
func With(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, key{}, err)
}

// From returns the run's outcome carried by ctx: nil after a clean run, the runners' crash errors joined after a
// crash, ErrNotStarted while fx rolls back a failed start, and nil when ctx carries no outcome.
func From(ctx context.Context) error {
	err, _ := ctx.Value(key{}).(error)

	return err
}
