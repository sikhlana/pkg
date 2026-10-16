//go:build unix

package cli_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/cli"
)

const signalChildEnv = "CLI_TEST_SIGNAL_CHILD"

func TestSignalContextStartsLive(t *testing.T) {
	ctx, stop := cli.SignalContext(t.Context())

	defer stop()

	assert.NoError(t, ctx.Err())
}

func TestSignalContextFirstSignalCancels(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, stop := cli.SignalContext(t.Context())

			defer stop()

			require.NoError(t, syscall.Kill(os.Getpid(), sig))

			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				require.FailNow(t, "context not cancelled by the signal")
			}

			cause := context.Cause(ctx)

			sigErr, ok := errors.AsType[*cli.SignalError](cause)
			require.True(t, ok, "cause is %v", cause)
			assert.Equal(t, sig, sigErr.Signal)
			assert.ErrorIs(t, cause, context.Canceled)
		})
	}
}

func TestSignalContextParentCancels(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())

	ctx, stop := cli.SignalContext(parent)

	defer stop()

	cancel()

	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestSignalContextStopCancels(t *testing.T) {
	ctx, stop := cli.SignalContext(t.Context())

	stop()
	stop()

	assert.ErrorIs(t, context.Cause(ctx), context.Canceled)
}

func TestSignalErrorMessage(t *testing.T) {
	assert.EqualError(t, &cli.SignalError{Signal: os.Interrupt}, "interrupt signal received")
}

func TestSignalContextSecondSignalTerminates(t *testing.T) {
	if os.Getenv(signalChildEnv) != "" {
		runSignalChild(t)

		return
	}

	exe, err := os.Executable()
	require.NoError(t, err)

	child := exec.CommandContext(t.Context(), exe, "-test.run=^"+t.Name()+"$")

	child.Env = append(os.Environ(), signalChildEnv+"=1")

	stdout, err := child.StdoutPipe()
	require.NoError(t, err)

	require.NoError(t, child.Start())

	// t.Context is cancelled before cleanups run, which kills a child left running by a failed check; this reaps it.
	// After a successful Wait it returns at once.
	t.Cleanup(func() { _ = child.Wait() })

	lines := bufio.NewScanner(stdout)

	require.True(t, lines.Scan())
	require.Equal(t, "ready", lines.Text())

	// SIGINT first and SIGTERM second: a background job started from a script inherits SIGINT ignored, and that's
	// what a second SIGINT would get once the handler is released.
	require.NoError(t, child.Process.Signal(syscall.SIGINT))

	require.True(t, lines.Scan())
	require.Equal(t, "cancelled", lines.Text())

	require.NoError(t, child.Process.Signal(syscall.SIGTERM))

	assert.False(t, lines.Scan(), "child printed %q after cancellation", lines.Text())

	exitErr, ok := errors.AsType[*exec.ExitError](child.Wait())
	require.True(t, ok, "child exited normally")

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)

	assert.True(t, status.Signaled())
	assert.Equal(t, syscall.SIGTERM, status.Signal())
}

// runSignalChild is the child half of TestSignalContextSecondSignalTerminates: a program that ignores its context
// after the first signal, so only the second one can end it.
func runSignalChild(t *testing.T) {
	t.Helper()

	ctx, stop := cli.SignalContext(t.Context())

	defer stop()

	fmt.Println("ready")

	<-ctx.Done()

	fmt.Println("cancelled")

	time.Sleep(10 * time.Second)
}
