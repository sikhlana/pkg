package main

import (
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/tools/release/plan"
	"github.com/sikhlana/pkg/tools/release/report"
)

func newPlanCommand() *cobra.Command {
	var base string

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "List the library modules a pull request lints and tests",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, ws, err := open(cmd.Context())
			if err != nil {
				return err
			}

			files, err := plan.Diff(cmd.Context(), repo, base)
			if err != nil {
				return err
			}

			selected := plan.Select(ws, files)

			matrix, err := plan.Matrix(selected)
			if err != nil {
				return err
			}

			w := report.New(cmd.OutOrStdout(), os.Getenv)
			w.Output("modules", matrix)
			w.Output("empty", strconv.FormatBool(len(selected) == 0))

			return nil
		},
	}

	cmd.Flags().StringVar(&base, "base", "origin/main", "the pull request's base branch")

	return cmd
}
