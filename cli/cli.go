package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// ConfigFlag is the name of the persistent config-file flag NewRootCommand adds. Any command under that root reads
// it with cmd.Flags().GetString(cli.ConfigFlag).
const ConfigFlag = "config"

// NewRootCommand returns a root command with the given name and short description. Every command under it accepts
// a --config (-c) string flag, empty by default. A failing command prints its error but never the usage text, flag
// errors included; --help still prints help. --version prints the same line as NewVersionCommand.
func NewRootCommand(name, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          name,
		Short:        short,
		Version:      ReadBuildInfo().String(),
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringP(ConfigFlag, "c", "", "path to the config file")
	cmd.SetVersionTemplate("{{.DisplayName}} {{.Version}}\n")

	return cmd
}

// NewVersionCommand returns a version subcommand that prints the root command's display name and the ReadBuildInfo
// line to the command's output. It works under any root, not only one from NewRootCommand.
func NewVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), cmd.Root().DisplayName(), ReadBuildInfo().String())
			if err != nil {
				return fmt.Errorf("printing version: %w", err)
			}

			return nil
		},
	}
}
