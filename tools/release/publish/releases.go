package publish

import "context"

//go:generate mockgen -typed -destination=mock_releases_test.go -package=publish_test . Releases

// Releases looks up and creates GitHub releases.
type Releases interface {
	// Exists reports whether a release exists for tag.
	Exists(ctx context.Context, tag string) (bool, error)
	// Create creates a release, and its tag when the tag doesn't exist.
	Create(ctx context.Context, release Release) error
}

// Release is a GitHub release to create. Its title is its tag.
type Release struct {
	// Tag is the release's tag, such as logger/v0.4.0.
	Tag string
	// Target is the commit to create the tag on. It's empty when the tag
	// already exists, which leaves the tag where it is.
	Target string
	// Notes is the release body.
	Notes string
}
