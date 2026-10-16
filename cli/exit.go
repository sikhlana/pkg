package cli

import (
	"context"
	"errors"
	"strconv"

	"github.com/spf13/cobra"
)

const maxExitCode = 255

// ExitError is an error that chooses the process exit code. Return one from a command, directly or wrapped, and
// Execute returns its Code.
type ExitError struct {
	// Code is the process exit code. Codes outside 1 to 255 exit 1.
	Code int

	// Err is the underlying error, or nil when the exit code is all there is to report.
	Err error
}

// Exit returns an *ExitError with the given code and underlying error. err may be nil. The result is never nil.
func Exit(code int, err error) error {
	return &ExitError{Code: code, Err: err}
}

// Error returns the underlying error's message, or "exit status N" when Err is nil, where N is the code the process
// exits with.
func (e *ExitError) Error() string {
	if e.Err == nil {
		return "exit status " + strconv.Itoa(e.exitCode())
	}

	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *ExitError) Unwrap() error {
	return e.Err
}

// exitCode returns Code, or 1 when Code is outside 1 to 255. Zero would report success, and Unix keeps only a code's
// low 8 bits, so 256 would exit 0 and a negative code would wrap.
func (e *ExitError) exitCode() int {
	if e.Code < 1 || e.Code > maxExitCode {
		return 1
	}

	return e.Code
}

// Execute runs cmd's root command with ctx and returns the process exit code for the result: 0 on success, the Code
// of the first *ExitError in the error chain, and 1 for any other error. A Code outside 1 to 255 is returned as 1, so
// a failed command never exits 0.
//
// Every command in the tree gets ctx before the run, so a tree that's executed more than once, as in tests, never
// hands a command an earlier run's context.
//
// Execute prints nothing. Cobra prints a failing command's error unless the command sets SilenceErrors, which is how
// a command exits quietly.
func Execute(ctx context.Context, cmd *cobra.Command) int {
	root := cmd.Root()

	setContext(ctx, root)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}

	if exitErr, ok := errors.AsType[*ExitError](err); ok {
		return exitErr.exitCode()
	}

	return 1
}

// setContext gives ctx to cmd and every command below it. Cobra only fills in a command's context when it's unset.
func setContext(ctx context.Context, cmd *cobra.Command) {
	cmd.SetContext(ctx)

	for _, sub := range cmd.Commands() {
		setContext(ctx, sub)
	}
}
