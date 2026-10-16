package cli_test

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/cli"
)

const mainChildEnv = "CLI_TEST_MAIN_CHILD"

func TestRunReturnsTheExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: 0},
		{name: "exit code", err: cli.Exit(7, nil), want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cli.Run(func() *cobra.Command {
				return quietCommand(func(*cobra.Command, []string) error {
					return tt.err
				})
			})

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunGivesCommandsACancellableContext(t *testing.T) {
	var live, cancellable bool

	cli.Run(func() *cobra.Command {
		return quietCommand(func(cmd *cobra.Command, _ []string) error {
			live = cmd.Context().Err() == nil
			cancellable = cmd.Context().Done() != nil

			return nil
		})
	})

	assert.True(t, live)
	assert.True(t, cancellable)
}

func TestMainExitsWithTheCode(t *testing.T) {
	if os.Getenv(mainChildEnv) != "" {
		cli.Main(func() *cobra.Command {
			return quietCommand(func(*cobra.Command, []string) error {
				return cli.Exit(7, nil)
			})
		})
	}

	exe, err := os.Executable()
	require.NoError(t, err)

	child := exec.CommandContext(t.Context(), exe, "-test.run=^"+t.Name()+"$")

	child.Env = append(os.Environ(), mainChildEnv+"=1")

	exitErr, ok := errors.AsType[*exec.ExitError](child.Run())
	require.True(t, ok, "child exited 0")

	assert.Equal(t, 7, exitErr.ExitCode())
}

// quietCommand returns a root command that runs runE with no arguments and leaves error printing to the test.
func quietCommand(runE func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "myapp",
		RunE:          runE,
		SilenceErrors: true,
	}

	// A nil slice makes cobra parse the test binary's own arguments.
	cmd.SetArgs([]string{})

	return cmd
}
