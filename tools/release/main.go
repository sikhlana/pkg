// Command release runs this repository's release CI: plan and check on pull
// requests, publish on push to main, and vulncheck every week.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/google/go-github/v93/github"
	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/tools/release/check"
	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/gorelease"
	"github.com/sikhlana/pkg/tools/release/parallel"
	"github.com/sikhlana/pkg/tools/release/plan"
	"github.com/sikhlana/pkg/tools/release/publish"
	"github.com/sikhlana/pkg/tools/release/report"
	"github.com/sikhlana/pkg/tools/release/vulncheck"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

var (
	errCheckFailed = errors.New("release check failed")
	errDirtyTree   = errors.New("the working tree has uncommitted changes to tracked files")
	errJobs        = errors.New("--jobs must be at least 1")
	errNoToken     = errors.New("GITHUB_TOKEN is empty; set it, or pass --dry-run")
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	err := newRootCommand().ExecuteContext(ctx)

	stop()

	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "release:", err)
		printPanicStack(os.Stderr, err)

		os.Exit(1)
	}
}

// printPanicStack writes the stack of every panic err carries, since a
// parallel.PanicError's message leaves it out.
func printPanicStack(w io.Writer, err error) {
	for _, panicErr := range panics(err) {
		_, _ = fmt.Fprintf(w, "%s", panicErr.Stack)
	}
}

// panics returns every *parallel.PanicError in err's tree, depth first, since
// errors.As finds only the first and a joined error can hold several.
func panics(err error) []*parallel.PanicError {
	switch e := err.(type) { //nolint:errorlint // Walks the tree itself.
	case nil:
		return nil
	case *parallel.PanicError:
		return []*parallel.PanicError{e}
	case interface{ Unwrap() []error }:
		var out []*parallel.PanicError

		for _, inner := range e.Unwrap() {
			out = append(out, panics(inner)...)
		}

		return out
	default:
		return panics(errors.Unwrap(err))
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "release",
		Short:         "Plan, check and publish library module releases, and scan for vulnerabilities",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newPlanCommand(), newCheckCommand(), newPublishCommand(), newVulncheckCommand())

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

func newVulncheckCommand() *cobra.Command {
	var (
		govulncheckPath string
		dryRun          bool
		jobs            *int
	)

	cmd := &cobra.Command{
		Use:   "vulncheck",
		Short: "Scan every library module and tool, and keep the govulncheck issue in step",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, ws, err := open(cmd.Context())
			if err != nil {
				return err
			}

			// Found before minutes of scanning, not after them.
			scanner, err := vulncheck.NewGovulncheck(govulncheckPath)
			if err != nil {
				return err
			}

			opts := vulncheck.Options{
				Workspace: ws,
				Run:       scanner.Run,
				RunURL:    vulncheck.RunURL(os.Getenv),
				Jobs:      *jobs,
			}

			if !dryRun {
				if opts.Tracker, err = newTracker(); err != nil {
					return err
				}
			}

			results, outcome, err := vulncheck.Run(cmd.Context(), opts)

			w := report.New(cmd.OutOrStdout(), os.Getenv)

			switch {
			case errors.Is(err, vulncheck.ErrIncomplete):
				w.Summary("> [!WARNING]\n> Some modules couldn't be scanned (see the log), so this report is " +
					"incomplete and the issue was left alone.\n\n" + vulncheck.Markdown(results))
			case results != nil:
				w.Summary(vulncheck.Markdown(results))
			}

			if err != nil {
				return err
			}

			if !dryRun {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "issue: %s %s\n", outcome.Action, issueRef(outcome.Issue))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&govulncheckPath, "govulncheck", "govulncheck",
		"the govulncheck binary, by default the one on PATH")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "write the report but leave the issue alone; needs no token")

	jobs = jobsFlag(cmd, "how many modules and tools to scan at once")

	return cmd
}

// jobsFlag adds --jobs to cmd, defaulting to GOMAXPROCS, and returns where
// its value lands. A value below 1 fails as the flag is parsed, before cmd
// does any work.
func jobsFlag(cmd *cobra.Command, usage string) *int {
	n := runtime.GOMAXPROCS(0)
	cmd.Flags().Var(jobsValue{n: &n}, "jobs", usage)

	return &n
}

// jobsValue is --jobs: a count of at least 1, checked when it's set.
type jobsValue struct {
	n *int
}

func (v jobsValue) Set(s string) error {
	n, err := strconv.Atoi(s)

	switch {
	case err != nil:
		return fmt.Errorf("%w, not %q", errJobs, s)
	case n < 1:
		return fmt.Errorf("%w, not %d", errJobs, n)
	}

	*v.n = n

	return nil
}

func (v jobsValue) String() string {
	return strconv.Itoa(*v.n)
}

func (jobsValue) Type() string {
	return "int"
}

// newTracker returns the GitHub tracker, failing without a token, so a run
// missing one stops before scanning rather than after.
func newTracker() (vulncheck.Tracker, error) {
	token := os.Getenv("GITHUB_TOKEN")

	if token == "" {
		return nil, errNoToken
	}

	client, err := github.NewClient(github.WithAuthToken(token))
	if err != nil {
		return nil, fmt.Errorf("creating GitHub client: %w", err)
	}

	return vulncheck.NewGitHub(client, os.Getenv("GITHUB_REPOSITORY"))
}

func issueRef(number int) string {
	if number == 0 {
		return ""
	}

	return "#" + strconv.Itoa(number)
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
