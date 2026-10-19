package vulncheck

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Label marks the issue Reconcile keeps.
const Label = "govulncheck"

// Title is the title of the issue Reconcile opens.
const Title = "Reachable vulnerabilities in dependencies"

// Action is what Reconcile did to the issue.
type Action string

// Actions Reconcile reports.
const (
	// ActionNone means there were no findings and no open issue.
	ActionNone Action = "none"
	// ActionOpened means Reconcile opened an issue.
	ActionOpened Action = "opened"
	// ActionUpdated means Reconcile replaced the open issue's body, and the
	// advisories were the ones it already listed.
	ActionUpdated Action = "updated"
	// ActionChanged means Reconcile replaced the open issue's body and
	// commented, because the advisories changed.
	ActionChanged Action = "changed"
	// ActionClosed means there were no findings and Reconcile closed the
	// open issue.
	ActionClosed Action = "closed"
)

// Outcome is what Reconcile did.
type Outcome struct {
	// Action is what happened to the issue.
	Action Action
	// Issue is the issue's number, or zero for ActionNone.
	Issue int
}

// Reconcile brings the open issue labelled Label in line with results:
//
//   - findings and no open issue: open one with the report, creating the
//     label if needed;
//   - findings and an open issue: when the advisories found in each module
//     differ from those in its marker, comment, since an edited body notifies
//     nobody and a comment does; then replace its body with the report. An
//     advisory spreading to another module, or getting a fixed version, is a
//     change. A body without a
//     marker, edited by hand, has no history to compare, so it's replaced
//     without a comment;
//   - no findings and an open issue: close it, then comment, linking runURL
//     when it isn't empty;
//   - no findings and no open issue: nothing.
//
// Each case does the step whose loss would go unnoticed first, so a run that
// fails halfway repeats a step next time rather than skipping one: a comment
// before the body that records the IDs it announced, a close before the
// comment that a still-open issue would repeat every week. The body is the
// report from Body, cut down to fit an issue when it's too long.
func Reconcile(ctx context.Context, tracker Tracker, results []Result, runURL string) (Outcome, error) {
	findings := Findings(results)
	entries := MarkerEntries(results)

	issue, open, err := tracker.Find(ctx, Label)
	if err != nil {
		return Outcome{}, err
	}

	switch {
	case len(findings) == 0 && !open:
		return Outcome{Action: ActionNone}, nil
	case len(findings) == 0:
		return closeIssue(ctx, tracker, issue.Number, runURL)
	case !open:
		return openIssue(ctx, tracker, results, runURL)
	}

	action := ActionUpdated

	if previous, ok := ParseMarker(issue.Body); ok && !slices.Equal(previous, entries) {
		if err = tracker.Comment(ctx, issue.Number, changeComment(previous, entries, results, runURL)); err != nil {
			return Outcome{}, err
		}

		action = ActionChanged
	}

	if err = tracker.Edit(ctx, issue.Number, Body(results, runURL)); err != nil {
		return Outcome{}, err
	}

	return Outcome{Action: action, Issue: issue.Number}, nil
}

// RunURL returns the URL of the GitHub Actions run, read through getenv, or
// empty outside GitHub Actions.
func RunURL(getenv func(string) string) string {
	server, repo, id := getenv("GITHUB_SERVER_URL"), getenv("GITHUB_REPOSITORY"), getenv("GITHUB_RUN_ID")

	if server == "" || repo == "" || id == "" {
		return ""
	}

	return server + "/" + repo + "/actions/runs/" + id
}

func openIssue(ctx context.Context, tracker Tracker, results []Result, runURL string) (Outcome, error) {
	if err := tracker.EnsureLabel(ctx, Label); err != nil {
		return Outcome{}, err
	}

	number, err := tracker.Create(ctx, Title, Body(results, runURL), Label)
	if err != nil {
		return Outcome{}, err
	}

	return Outcome{Action: ActionOpened, Issue: number}, nil
}

func closeIssue(ctx context.Context, tracker Tracker, number int, runURL string) (Outcome, error) {
	if err := tracker.Close(ctx, number); err != nil {
		return Outcome{}, err
	}

	if err := tracker.Comment(ctx, number, "No reachable vulnerabilities "+inRun(runURL)+"."); err != nil {
		return Outcome{}, err
	}

	return Outcome{Action: ActionClosed, Issue: number}, nil
}

func changeComment(previous, current []string, results []Result, runURL string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "The reachable advisories changed %s.\n", inRun(runURL))

	// A digest says the findings changed, not how.
	if isDigest(previous) || isDigest(current) {
		fmt.Fprintf(&b, "\nThere are too many findings to list here; the step summary %s has them.\n", inRun(runURL))

		return b.String()
	}

	before, after := fixes(previous), fixes(current)

	var added, gone, fixable, withdrawn []string

	for _, key := range slices.Sorted(maps.Keys(after)) {
		hadFix, found := before[key]

		switch {
		case !found:
			added = append(added, describe(key))
		case !hadFix && after[key]:
			fixable = append(fixable, describe(key)+" (fixed in "+fixedIn(results, key)+")")
		case hadFix && !after[key]:
			withdrawn = append(withdrawn, describe(key))
		}
	}

	for _, key := range slices.Sorted(maps.Keys(before)) {
		if _, found := after[key]; !found {
			gone = append(gone, describe(key))
		}
	}

	for _, section := range []struct {
		title string
		items []string
	}{
		{"New", added},
		{"Fix available", fixable},
		{"Fix no longer listed", withdrawn},
		{"No longer found", gone},
	} {
		if len(section.items) > 0 {
			fmt.Fprintf(&b, "\n%s: %s\n", section.title, strings.Join(section.items, ", "))
		}
	}

	return b.String()
}

// fixes maps each finding's dir:ID key to whether its advisory has a fix.
func fixes(findings []string) map[string]bool {
	out := make(map[string]bool, len(findings))

	for _, f := range findings {
		key, hasFix := splitFinding(f)
		out[key] = hasFix
	}

	return out
}

// fixedIn returns the fixed versions the advisories behind a dir:ID key name, joined.
func fixedIn(results []Result, key string) string {
	dir, id, _ := strings.Cut(key, ":")

	var versions []string

	for _, r := range results {
		if r.Dir != dir {
			continue
		}

		for _, a := range r.Advisories {
			if a.ID == id && a.Fixed != "" {
				versions = append(versions, a.Dependency+" "+a.Fixed)
			}
		}
	}

	slices.Sort(versions)

	return strings.Join(slices.Compact(versions), ", ")
}

// describe renders a dir:ID key as "ID in dir".
func describe(key string) string {
	dir, id, _ := strings.Cut(key, ":")

	return id + " in " + dir
}

func inRun(runURL string) string {
	if runURL == "" {
		return "in the latest scan"
	}

	return "in [this run](" + runURL + ")"
}
