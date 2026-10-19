package vulncheck

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v93/github"

	"github.com/sikhlana/pkg/tools/release/repository"
)

const (
	labelColor       = "b60205"
	labelDescription = "Reachable vulnerabilities found by the weekly govulncheck scan"
)

// GitHub implements Tracker with the GitHub REST API.
type GitHub struct {
	client *github.Client
	owner  string
	repo   string
}

// NewGitHub returns a GitHub for the repository name, given as owner/name, as in
// GITHUB_REPOSITORY, failing with repository.ErrInvalid otherwise. It makes no
// API calls.
func NewGitHub(client *github.Client, name string) (GitHub, error) {
	owner, repo, err := repository.Parse(name)
	if err != nil {
		return GitHub{}, err
	}

	return GitHub{client: client, owner: owner, repo: repo}, nil
}

// Find returns the oldest open issue carrying label, skipping pull requests,
// which the issues API also lists, and reading page after page until it finds
// one.
func (g GitHub) Find(ctx context.Context, label string) (Issue, bool, error) {
	opts := &github.IssueListByRepoOptions{
		State:       "open",
		Labels:      []string{label},
		Sort:        "created",
		Direction:   "asc",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		issues, resp, err := g.client.Issues.ListByRepo(ctx, g.owner, g.repo, opts)
		if err != nil {
			return Issue{}, false, fmt.Errorf("listing issues labelled %s: %w", label, err)
		}

		for _, issue := range issues {
			if !issue.IsPullRequest() {
				return Issue{Number: issue.GetNumber(), Body: issue.GetBody()}, true, nil
			}
		}

		if resp.NextPage == 0 {
			return Issue{}, false, nil
		}

		opts.ListOptions.Page = resp.NextPage
	}
}

// EnsureLabel creates label, in red and described as the scan's, when the
// repository doesn't have it.
func (g GitHub) EnsureLabel(ctx context.Context, label string) error {
	_, resp, err := g.client.Issues.GetLabel(ctx, g.owner, g.repo, label)

	switch {
	case err == nil:
		return nil
	case resp == nil || resp.StatusCode != http.StatusNotFound:
		return fmt.Errorf("looking up label %s: %w", label, err)
	}

	_, _, err = g.client.Issues.CreateLabel(ctx, g.owner, g.repo, github.CreateIssueLabelRequest{
		Name:        label,
		Color:       new(labelColor),
		Description: new(labelDescription),
	})
	if err != nil {
		return fmt.Errorf("creating label %s: %w", label, err)
	}

	return nil
}

// Create opens an issue carrying label and returns its number.
func (g GitHub) Create(ctx context.Context, title, body, label string) (int, error) {
	issue, _, err := g.client.Issues.Create(ctx, g.owner, g.repo, github.CreateIssueRequest{
		Title:  title,
		Body:   new(body),
		Labels: []string{label},
	})
	if err != nil {
		return 0, fmt.Errorf("creating issue: %w", err)
	}

	return issue.GetNumber(), nil
}

// Edit replaces an issue's body.
func (g GitHub) Edit(ctx context.Context, number int, body string) error {
	_, _, err := g.client.Issues.Update(ctx, g.owner, g.repo, number, github.UpdateIssueRequest{Body: new(body)})
	if err != nil {
		return fmt.Errorf("editing issue #%d: %w", number, err)
	}

	return nil
}

// Comment adds a comment to an issue.
func (g GitHub) Comment(ctx context.Context, number int, body string) error {
	_, _, err := g.client.Issues.CreateComment(ctx, g.owner, g.repo, number, github.IssueCommentRequest{Body: body})
	if err != nil {
		return fmt.Errorf("commenting on issue #%d: %w", number, err)
	}

	return nil
}

// Close closes an issue as completed.
func (g GitHub) Close(ctx context.Context, number int) error {
	_, _, err := g.client.Issues.Update(ctx, g.owner, g.repo, number, github.UpdateIssueRequest{
		State:       new("closed"),
		StateReason: new("completed"),
	})
	if err != nil {
		return fmt.Errorf("closing issue #%d: %w", number, err)
	}

	return nil
}
