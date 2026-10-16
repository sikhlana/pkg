// Command release runs this repository's release CI: plan and check on pull
// requests, publish on push to main.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"

	"github.com/google/go-github/v93/github"
	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/tools/release/check"
	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/gorelease"
	"github.com/sikhlana/pkg/tools/release/plan"
	"github.com/sikhlana/pkg/tools/release/publish"
	"github.com/sikhlana/pkg/tools/release/report"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

var (
	errCheckFailed = errors.New("release check failed")
	errDirtyTree   = errors.New("the working tree has uncommitted changes to tracked files")
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	err := newRootCommand().ExecuteContext(ctx)

	stop()

	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "release:", err)

		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "release",
		Short:         "Plan, check and publish library module releases",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newPlanCommand(), newCheckCommand(), newPublishCommand())

	return root
}

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

func newCheckCommand() *cobra.Command {
	var base, goreleasePath string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check changed modules' versions and the repository invariants",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo, ws, err := open(cmd.Context())
			if err != nil {
				return err
			}

			// Created only when check builds gorelease itself.
			var tmp string

			defer func() {
				if tmp != "" {
					_ = os.RemoveAll(tmp)
				}
			}()

			// gorelease runs in each module's directory, so a relative path would break.
			if goreleasePath != "" {
				if goreleasePath, err = filepath.Abs(goreleasePath); err != nil {
					return fmt.Errorf("resolving --gorelease: %w", err)
				}
			}

			r, err := check.Run(cmd.Context(), check.Options{
				Repo:      repo,
				Workspace: ws,
				Base:      base,
				Gorelease: func(ctx context.Context) (gorelease.Runner, error) {
					if goreleasePath != "" {
						return gorelease.Runner{Path: goreleasePath}, nil
					}

					if tmp, err = os.MkdirTemp("", "release-gorelease-"); err != nil {
						return gorelease.Runner{}, fmt.Errorf("creating temporary directory: %w", err)
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

	return cmd
}

func newPublishCommand() *cobra.Command {
	return &cobra.Command{
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
			})

			w := report.New(cmd.OutOrStdout(), os.Getenv)
			w.Summary(publish.Markdown(outcomes))

			for _, o := range outcomes {
				if warning := o.SkippedWarning(); warning != "" {
					w.Warning(warning)
				}

				if o.ProxyErr != nil {
					w.Warning(o.ProxyErr.Error())
				}
			}

			return err
		},
	}
}

func open(ctx context.Context) (git.Repo, workspace.Workspace, error) {
	repo, err := git.Open(ctx, ".")
	if err != nil {
		return git.Repo{}, workspace.Workspace{}, err
	}

	ws, err := workspace.Load(repo.Root)
	if err != nil {
		return git.Repo{}, workspace.Workspace{}, err
	}

	return repo, ws, nil
}
