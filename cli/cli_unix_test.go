//go:build unix

package cli_test

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/cli"
)

func TestRunInstallsTheHandlerBeforeBuilding(t *testing.T) {
	var cause error

	code := cli.Run(func() *cobra.Command {
		// Without the handler already installed, this would kill the test binary.
		require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGINT))

		return quietCommand(func(cmd *cobra.Command, _ []string) error {
			select {
			case <-cmd.Context().Done():
			case <-time.After(5 * time.Second):
			}

			cause = context.Cause(cmd.Context())

			return nil
		})
	})

	require.Equal(t, 0, code)

	sigErr, ok := errors.AsType[*cli.SignalError](cause)
	require.True(t, ok, "cause is %v", cause)
	assert.Equal(t, syscall.SIGINT, sigErr.Signal)
}
