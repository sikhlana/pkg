package vulncheck

import "context"

//go:generate mockgen -typed -destination=mock_tracker_test.go -package=vulncheck_test . Tracker

// Tracker finds, files and updates issues.
type Tracker interface {
	// Find returns the oldest open issue carrying label, and false when
	// there's none.
	Find(ctx context.Context, label string) (Issue, bool, error)
	// EnsureLabel creates label when the repository doesn't have it.
	EnsureLabel(ctx context.Context, label string) error
	// Create opens an issue carrying label and returns its number.
	Create(ctx context.Context, title, body, label string) (int, error)
	// Edit replaces an issue's body.
	Edit(ctx context.Context, number int, body string) error
	// Comment adds a comment to an issue.
	Comment(ctx context.Context, number int, body string) error
	// Close closes an issue as completed.
	Close(ctx context.Context, number int) error
}

// Issue is an open issue.
type Issue struct {
	// Number is the issue's number.
	Number int
	// Body is the issue's body.
	Body string
}
