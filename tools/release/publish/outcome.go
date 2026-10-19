package publish

import (
	"fmt"
	"strings"

	"github.com/sikhlana/pkg/tools/release/report"
)

// Status is what Run did for a module.
type Status string

// Statuses Run reports.
const (
	// StatusReleased means the module's version already had a release.
	StatusReleased Status = "already released"
	// StatusCreated means Run created the release.
	StatusCreated Status = "created"
	// StatusFailed means Run couldn't create the release; Outcome.Err says
	// why.
	StatusFailed Status = "failed"
)

// Outcome is what Run did for one library module.
type Outcome struct {
	// Dir is the module's directory.
	Dir string
	// Tag is the tag for the module's VERSION, empty when VERSION is
	// invalid.
	Tag string
	// Status is what happened.
	Status Status
	// Err is why the release failed, for StatusFailed.
	Err error
	// Skipped lists versions, such as v0.4.0, that VERSION held after the
	// previous release but that were never released, because a publish
	// failed and a later merge bumped VERSION again, for StatusCreated.
	// The new release includes their changes.
	Skipped []string
	// ProxyErr is why the module proxy announcement failed, for
	// StatusCreated. It doesn't fail the release.
	ProxyErr error
}

// Markdown renders outcomes for the job summary.
func Markdown(outcomes []Outcome) string {
	var b strings.Builder

	b.WriteString("## Publish\n\n")

	if len(outcomes) == 0 {
		b.WriteString("No library modules.\n")
		return b.String()
	}

	b.WriteString("| Module | Tag | Result |\n")
	b.WriteString("|--------|-----|--------|\n")

	for _, o := range outcomes {
		result := string(o.Status)

		if o.Err != nil {
			result += ": " + o.Err.Error()
		}

		if len(o.Skipped) > 0 {
			result += "; never released: " + strings.Join(o.Skipped, ", ")
		}

		if o.ProxyErr != nil {
			result += "; module proxy not updated: " + o.ProxyErr.Error()
		}

		fmt.Fprintf(&b, "| %s | %s | %s |\n", o.Dir, o.Tag, report.Cell(result))
	}

	return b.String()
}

// SkippedWarning explains o's skipped versions, or returns "" when there are
// none.
func (o Outcome) SkippedWarning() string {
	if len(o.Skipped) == 0 {
		return ""
	}

	return fmt.Sprintf(
		"%s: VERSION held %s after its previous release, but they were never released; %s includes their changes",
		o.Dir,
		strings.Join(o.Skipped, ", "),
		o.Tag,
	)
}
