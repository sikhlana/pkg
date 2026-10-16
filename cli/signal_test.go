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
	ctx, stop := cli.SignalContext(t.Context())

	defer stop()

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGINT))

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		require.FailNow(t, "context not cancelled by SIGINT")
	}

	cause := context.Cause(ctx)

	require.ErrorIs(t, cause, context.Canceled)
	assert.ErrorContains(t, cause, os.Interrupt.String())
}

func TestSignalContextParentCancels(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())

	ctx, stop := cli.SignalContext(parent)

	defer stop()

	cancel()

	assert.ErrorIs(t, ctx.Err(), context.Canceled)
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

	lines := bufio.NewScanner(stdout)

	require.True(t, lines.Scan())
	require.Equal(t, "ready", lines.Text())

	require.NoError(t, child.Process.Signal(os.Interrupt))

	require.True(t, lines.Scan())
	require.Equal(t, "cancelled", lines.Text())

	// The handler is released on its own goroutine just after cancellation, so leave it a moment, as a person
	// pressing Ctrl-C twice would.
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, child.Process.Signal(os.Interrupt))

	exitErr, ok := errors.AsType[*exec.ExitError](child.Wait())
	require.True(t, ok, "child exited normally")

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)

	assert.True(t, status.Signaled())
	assert.Equal(t, syscall.SIGINT, status.Signal())
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
