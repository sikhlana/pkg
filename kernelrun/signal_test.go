//go:build unix

package kernelrun_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/kernelrun"
)

const signalChildEnv = "KERNELRUN_TEST_SIGNAL_CHILD"

func TestRunNeverSubscribesToSignals(t *testing.T) {
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

	require.NoError(t, child.Process.Signal(syscall.SIGINT))

	assert.False(t, lines.Scan(), "child printed %q after the signal", lines.Text())

	exitErr, ok := errors.AsType[*exec.ExitError](child.Wait())
	require.True(t, ok, "child exited normally")

	status, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok)

	assert.True(t, status.Signaled())
	assert.Equal(t, syscall.SIGINT, status.Signal())
}

// runSignalChild is the child half of TestRunNeverSubscribesToSignals: an app whose runner waits on a context no
// signal cancels, so only the signal's default action can end it.
func runSignalChild(t *testing.T) {
	t.Helper()

	err := run(t, kernelrun.Options{}, kernelrun.WithRunner("api", func(ctx context.Context) error {
		fmt.Println("ready")

		return waitDone(ctx)
	}))

	fmt.Println("returned", err)
}
