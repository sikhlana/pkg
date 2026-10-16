package cli

import (
	"cmp"
	"fmt"

	"github.com/spf13/cobra"
)

// ConfigFlag is the name of the persistent config-file flag NewRootCommand adds, --config. Any command under that
// root reads it with cmd.Flags().GetString(cli.ConfigFlag).
const ConfigFlag = "config"

// NewRootCommand returns a root command with the given name and short description. Every command under it accepts
// a --config string flag, empty by default. The flag has no shorthand, so every letter stays free for subcommands. A
// failing command prints its error but never the usage text, flag errors included; --help still prints help.
// --version prints the root's display name and its Version, which starts as the ReadBuildInfo line.
func NewRootCommand(name, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          name,
		Short:        short,
		Version:      ReadBuildInfo().String(),
		SilenceUsage: true,
	}

	cmd.PersistentFlags().String(ConfigFlag, "", "path to the config file")
	cmd.SetVersionTemplate("{{.DisplayName}} {{.Version}}\n")

	return cmd
}

// NewVersionCommand returns a version subcommand that prints the root command's display name and its Version, or the
// ReadBuildInfo line when the root's Version is empty. Under a root from NewRootCommand it prints the same line as
// --version, including after the program changes the root's Version.
func NewVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := cmd.Root()

			_, err := fmt.Fprintln(
				cmd.OutOrStdout(),
				root.DisplayName(),
				cmp.Or(root.Version, ReadBuildInfo().String()),
			)
			if err != nil {
				return fmt.Errorf("printing version: %w", err)
			}

			return nil
		},
	}
}
