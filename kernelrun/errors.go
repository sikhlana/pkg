package kernelrun

import (
	"errors"
	"fmt"
)

var (
	// ErrNoRunners is returned by Run for an app that registers no runners.
	ErrNoRunners = errors.New("no runners")

	// ErrDuplicateRunner is returned by Run for an app that registers two runners under one name.
	ErrDuplicateRunner = errors.New("duplicate runner name")

	// ErrInvalidRunner is returned by Run for a runner with an empty name or a nil Func.
	ErrInvalidRunner = errors.New("invalid runner")
)

// ErrExited is the crash a runner that exits through runtime.Goexit, without returning, becomes. Run wraps it with the
// runner's name.
var ErrExited = errors.New("exited without returning")

// panicError is the crash a runner's recovered panic becomes.
type panicError struct {
	value any
	stack []byte
}

// Error returns "panic: " followed by the panic's value.
func (e *panicError) Error() string {
	return fmt.Sprintf("panic: %v", e.value)
}
