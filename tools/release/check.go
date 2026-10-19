package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/tools/release/check"
	"github.com/sikhlana/pkg/tools/release/gorelease"
	"github.com/sikhlana/pkg/tools/release/report"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

func newCheckCommand() *cobra.Command {
	var (
		base, goreleasePath string
		jobs                *int
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check changed modules' versions and the repository invariants",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, ws, err := open(cmd.Context())
			if err != nil {
				return err
			}

			// gorelease runs in each module's directory, so a relative path would break.
			if goreleasePath != "" {
				if goreleasePath, err = filepath.Abs(goreleasePath); err != nil {
					return fmt.Errorf("resolving --gorelease: %w", err)
				}
			}

			// Where check builds gorelease when it isn't given one. Made up front, so the
			// build, which runs on a check worker, writes nothing this function reads.
			var tmp string

			if goreleasePath == "" {
				if tmp, err = os.MkdirTemp("", "release-gorelease-"); err != nil {
					return fmt.Errorf("creating temporary directory: %w", err)
				}

				defer func() { _ = os.RemoveAll(tmp) }()
			}

			r, err := check.Run(cmd.Context(), check.Options{
				Repo:      repo,
				Workspace: ws,
				Base:      base,
				Jobs:      *jobs,
				Gorelease: func(ctx context.Context) (gorelease.Runner, error) {
					if goreleasePath != "" {
						return gorelease.Runner{Path: goreleasePath}, nil
					}

					return gorelease.Build(
						ctx,
						filepath.Join(repo.Root, filepath.FromSlash(workspace.ToolModuleDir)),
						tmp,
					)
				},
			})
			if err != nil {
				return err
			}

			w := report.New(cmd.OutOrStdout(), os.Getenv)
			w.Summary(r.Markdown())
			w.Errors(r.Problems)

			if r.Failed() {
				return errCheckFailed
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&base, "base", "origin/main", "the pull request's base branch")
	cmd.Flags().StringVar(&goreleasePath, "gorelease", "",
		"a prebuilt gorelease binary; by default check builds the version go.mod pins")

	jobs = jobsFlag(cmd, "how many modules to check at once")

	return cmd
}
