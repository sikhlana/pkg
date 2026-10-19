package main

import (
	"fmt"
	"os"

	"github.com/google/go-github/v93/github"
	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/tools/release/publish"
	"github.com/sikhlana/pkg/tools/release/report"
)

func newPublishCommand() *cobra.Command {
	var jobs *int

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Create a release, on HEAD, for every module version that has none",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, ws, err := open(cmd.Context())
			if err != nil {
				return err
			}

			// VERSION files come from the working tree, and new tags go on
			// HEAD, so the two must agree.
			clean, err := repo.Clean(cmd.Context())
			if err != nil {
				return err
			}

			if !clean {
				return errDirtyTree
			}

			commit, err := repo.Commit(cmd.Context(), "HEAD")
			if err != nil {
				return err
			}

			client, err := github.NewClient(github.WithAuthToken(os.Getenv("GITHUB_TOKEN")))
			if err != nil {
				return fmt.Errorf("creating GitHub client: %w", err)
			}

			releases, err := publish.NewGitHub(client, os.Getenv("GITHUB_REPOSITORY"))
			if err != nil {
				return err
			}

			outcomes, err := publish.Run(cmd.Context(), publish.Options{
				Repo:      repo,
				Workspace: ws,
				SHA:       commit,
				Releases:  releases,
				Jobs:      *jobs,
			})

			w := report.New(cmd.OutOrStdout(), os.Getenv)
			w.Summary(publish.Markdown(outcomes))

			for _, o := range outcomes {
				if warning := o.SkippedWarning(); warning != "" {
					w.Warning(warning)
				}

				if o.ProxyErr != nil {
					w.Warning(o.ProxyErr.Error())
					printPanicStack(cmd.ErrOrStderr(), o.ProxyErr)
				}
			}

			return err
		},
	}

	jobs = jobsFlag(
		cmd,
		"how many modules to read, and versions to announce, at once; releases are created one at a time",
	)

	return cmd
}
