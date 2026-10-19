// Package parallel runs per-module work on a bounded pool. Every item gets
// its own outcome: nothing is cancelled when one item fails, a panic becomes
// that item's error, and no task outlives the call.
package parallel

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/alitto/pond/v2"
)

var (
	// ErrNotRun is wrapped by an item's Result.Err when its function never
	// ran because the context ended first. The error also wraps the
	// context's cause.
	ErrNotRun = errors.New("not run")

	// ErrPanic is wrapped by an item's Result.Err, a *PanicError, when its
	// function panicked: a bug in the tool, not a failure of the item.
	ErrPanic = errors.New("panicked")

	// ErrNoResult is an item's Result.Err when its function ended without
	// returning or panicking, as runtime.Goexit makes it.
	ErrNoResult = errors.New("ended without returning")
)

// PanicError is an item's Result.Err when its function panicked. It wraps
// ErrPanic. Its message holds only the panic value, since it can reach a job
// summary or an annotation; Stack keeps the goroutine's stack for whoever
// reports the bug.
type PanicError struct {
	// Value is what the function panicked with.
	Value any
	// Stack is the panicking goroutine's stack, as debug.Stack formats it.
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("%v: %v", ErrPanic, e.Value)
}

// Unwrap returns ErrPanic.
func (e *PanicError) Unwrap() error {
	return ErrPanic
}

// Result is one item's outcome: the function's value and error, or, with a
// zero Value, an error wrapping ErrNotRun, ErrPanic or ErrNoResult when the
// function never ran, panicked or never returned.
type Result[R any] struct {
	Value R
	Err   error
}

// Map runs fn over items on a pool of at most jobs workers, stopping
// starting new ones when ctx ends, and returns a Result per item in items'
// order once every function that started has returned. One item failing
// doesn't stop the others. jobs below 1 means 1, so an Options.Jobs field's
// zero value runs one at a time.
func Map[T, R any](ctx context.Context, jobs int, items []T, fn func(context.Context, T) (R, error)) []Result[R] {
	results := make([]Result[R], len(items))
	ran := make([]bool, len(items))
	// pond reads 0 as no limit.
	pool := pond.NewPool(max(jobs, 1), pond.WithContext(ctx))

	for i, item := range items {
		// Each task writes only its own index, and StopAndWait orders those
		// writes before the reads below. The returned task is only ever
		// cancelled, which ran records.
		_ = pool.Submit(func() {
			ran[i] = true

			// Left in place only when fn never returns, so that never reads
			// as success.
			results[i].Err = ErrNoResult

			defer func() {
				if v := recover(); v != nil {
					results[i] = Result[R]{Err: &PanicError{Value: v, Stack: debug.Stack()}}
				}
			}()

			value, err := fn(ctx, item)
			results[i] = Result[R]{Value: value, Err: err}
		})
	}

	pool.StopAndWait()

	for i := range results {
		if !ran[i] {
			results[i].Err = fmt.Errorf("%w: %w", ErrNotRun, context.Cause(ctx))
		}
	}

	return results
}
