package cli

import (
	"context"
	"errors"
	"strconv"

	"github.com/spf13/cobra"
)

// ExitError is an error that chooses the process exit code. Return one from a command, directly or wrapped, and
// Execute returns its Code.
type ExitError struct {
	// Code is the process exit code. Execute turns a code of zero or less into 1.
	Code int

	// Err is the underlying error, or nil when the exit code is all there is to report.
	Err error
}

// Exit returns an ExitError with the given code and underlying error. err may be nil. The result is never nil.
func Exit(code int, err error) *ExitError {
	return &ExitError{Code: code, Err: err}
}

// Error returns the underlying error's message, or "exit status N" when Err is nil.
func (e *ExitError) Error() string {
	if e.Err == nil {
		return "exit status " + strconv.Itoa(e.Code)
	}

	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ExitError) Unwrap() error {
	return e.Err
}

// Execute runs cmd with ctx and returns the process exit code for the result: 0 on success, the Code of the first
// ExitError in the error chain, and 1 for any other error. A Code of zero or less is returned as 1, so a failed
// command never exits 0.
//
// Execute prints nothing. Cobra prints a failing command's error unless the command sets SilenceErrors, which is how
// a command exits quietly.
func Execute(ctx context.Context, cmd *cobra.Command) int {
	err := cmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}

	exitErr, ok := errors.AsType[*ExitError](err)

	if !ok || exitErr.Code <= 0 {
		return 1
	}

	return exitErr.Code
}
