package cli_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/cli"
)

type result struct {
	code   int
	stdout string
	stderr string
}

func TestRootCommandRuntimeError(t *testing.T) {
	got := execute(t, newRoot(&cobra.Command{
		Use: "serve",
		RunE: func(*cobra.Command, []string) error {
			return errors.New("boom")
		},
	}), "serve")

	assert.Equal(t, result{code: 1, stderr: "Error: boom\n"}, got)
}

func TestRootCommandFlagError(t *testing.T) {
	got := execute(t, newRoot(&cobra.Command{
		Use: "serve",
		Run: func(*cobra.Command, []string) {},
	}), "serve", "--nope")

	assert.Equal(t, result{code: 1, stderr: "Error: unknown flag: --nope\n"}, got)
}

func TestRootCommandQuietExit(t *testing.T) {
	got := execute(t, newRoot(&cobra.Command{
		Use: "check",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceErrors = true

			return cli.Exit(1, nil)
		},
	}), "check")

	assert.Equal(t, result{code: 1}, got)
}

func TestRootCommandHelp(t *testing.T) {
	got := execute(t, newRoot(), "--help")

	require.Equal(t, 0, got.code)
	assert.Contains(t, got.stdout, "Usage:")
	assert.Contains(t, got.stdout, "--config")
}

func TestVersionFlagMatchesSubcommand(t *testing.T) {
	want := result{stdout: "myapp " + cli.ReadBuildInfo().String() + "\n"}

	assert.Equal(t, want, execute(t, newRoot(), "--version"))
	assert.Equal(t, want, execute(t, newRoot(), "version"))
}

func TestVersionFollowsTheRoot(t *testing.T) {
	want := result{stdout: "myapp 1.4.0\n"}

	for _, args := range [][]string{{"--version"}, {"version"}} {
		root := newRoot()

		root.Version = "1.4.0"

		assert.Equal(t, want, execute(t, root, args...))
	}
}

func TestVersionCommandUnderAnyRoot(t *testing.T) {
	root := &cobra.Command{Use: "other"}

	root.AddCommand(cli.NewVersionCommand())

	assert.Equal(t, result{stdout: "other " + cli.ReadBuildInfo().String() + "\n"}, execute(t, root, "version"))
}

func TestConfigFlagReachesSubcommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unset", args: []string{"serve"}, want: ""},
		{name: "set", args: []string{"serve", "--config", "/etc/myapp.yaml"}, want: "/etc/myapp.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string

			root := newRoot(&cobra.Command{
				Use: "serve",
				RunE: func(cmd *cobra.Command, _ []string) error {
					var err error

					got, err = cmd.Flags().GetString(cli.ConfigFlag)

					return err
				},
			})

			require.Equal(t, result{}, execute(t, root, tt.args...))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSubcommandOwnsEveryShorthand(t *testing.T) {
	var got int

	serve := &cobra.Command{
		Use: "serve",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error

			got, err = cmd.Flags().GetInt("concurrency")

			return err
		},
	}

	serve.Flags().IntP("concurrency", "c", 1, "number of workers")

	require.Equal(t, result{}, execute(t, newRoot(serve), "serve", "-c", "4"))
	assert.Equal(t, 4, got)
}

// newRoot returns a root command from NewRootCommand with the version subcommand and the given subcommands.
func newRoot(commands ...*cobra.Command) *cobra.Command {
	root := cli.NewRootCommand("myapp", "An example program")

	root.AddCommand(cli.NewVersionCommand())
	root.AddCommand(commands...)

	return root
}

// execute runs root with args through cli.Execute and captures what it wrote.
func execute(t *testing.T, root *cobra.Command, args ...string) result {
	t.Helper()

	var stdout, stderr bytes.Buffer

	root.SetOut(&stdout)
	root.SetErr(&stderr)
	// A nil slice makes cobra parse the test binary's own arguments.
	root.SetArgs(append([]string{}, args...))

	code := cli.Execute(t.Context(), root)

	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}
