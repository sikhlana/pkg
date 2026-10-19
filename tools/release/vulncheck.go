package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/google/go-github/v93/github"
	"github.com/spf13/cobra"

	"github.com/sikhlana/pkg/tools/release/report"
	"github.com/sikhlana/pkg/tools/release/vulncheck"
)

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
