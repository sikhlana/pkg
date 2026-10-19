package publish

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v93/github"

	"github.com/sikhlana/pkg/tools/release/repository"
)

// GitHub implements Releases with the GitHub REST API.
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

// Exists reports whether a published release exists for tag.
func (g GitHub) Exists(ctx context.Context, tag string) (bool, error) {
	_, resp, err := g.client.Repositories.GetReleaseByTag(ctx, g.owner, g.repo, tag)

	switch {
	case err == nil:
		return true, nil
	case resp != nil && resp.StatusCode == http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("looking up release %s: %w", tag, err)
	}
}

// Create creates release, titled with its tag and never marked as the
// repository's latest release.
func (g GitHub) Create(ctx context.Context, release Release) error {
	req := github.CreateReleaseRequest{
		TagName:    release.Tag,
		Name:       new(release.Tag),
		Body:       new(release.Notes),
		MakeLatest: new("false"),
	}

	if release.Target != "" {
		req.TargetCommitish = new(release.Target)
	}

	if _, _, err := g.client.Repositories.CreateRelease(ctx, g.owner, g.repo, req); err != nil {
		return fmt.Errorf("creating release %s: %w", release.Tag, err)
	}

	return nil
}
