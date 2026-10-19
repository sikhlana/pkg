package vulncheck_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/sikhlana/pkg/tools/release/vulncheck"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

const runURL = "https://github.com/sikhlana/pkg/actions/runs/42"

var errTracker = errors.New("GitHub is down")

func findings(ids ...string) []vulncheck.Result {
	advisories := make([]vulncheck.Advisory, 0, len(ids))

	for _, id := range ids {
		advisories = append(advisories, vulncheck.Advisory{ID: id, Dependency: "golang.org/x/net", Version: "v0.22.0"})
	}

	return []vulncheck.Result{{Dir: "cli"}, {Dir: "kernel", Advisories: advisories}}
}

func clean() []vulncheck.Result {
	return []vulncheck.Result{{Dir: "cli"}, {Dir: "kernel"}}
}

func TestReconcileFindingsWithoutAnIssueOpensOne(t *testing.T) {
	results := findings("GO-1")
	tracker := NewMockTracker(gomock.NewController(t))

	gomock.InOrder(
		tracker.EXPECT().Find(gomock.Any(), vulncheck.Label).Return(vulncheck.Issue{}, false, nil),
		tracker.EXPECT().EnsureLabel(gomock.Any(), vulncheck.Label).Return(nil),
		tracker.EXPECT().
			Create(gomock.Any(), vulncheck.Title, vulncheck.Body(results, runURL), vulncheck.Label).
			Return(7, nil),
	)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, results, runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.Outcome{Action: vulncheck.ActionOpened, Issue: 7}, outcome)
}

func TestReconcileSameFindingsReplacesTheBodyWithoutAComment(t *testing.T) {
	results := findings("GO-2", "GO-1")
	tracker := NewMockTracker(gomock.NewController(t))

	tracker.EXPECT().
		Find(gomock.Any(), vulncheck.Label).
		Return(vulncheck.Issue{Number: 7, Body: vulncheck.Markdown(findings("GO-1", "GO-2"))}, true, nil)
	tracker.EXPECT().Edit(gomock.Any(), 7, vulncheck.Body(results, runURL)).Return(nil)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, results, runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.Outcome{Action: vulncheck.ActionUpdated, Issue: 7}, outcome)
}

func TestReconcileChangedFindingsCommentsThenReplacesTheBody(t *testing.T) {
	results := findings("GO-2", "GO-3")
	tracker := NewMockTracker(gomock.NewController(t))

	gomock.InOrder(
		tracker.EXPECT().
			Find(gomock.Any(), vulncheck.Label).
			Return(vulncheck.Issue{Number: 7, Body: vulncheck.Markdown(findings("GO-1", "GO-2"))}, true, nil),
		tracker.EXPECT().Comment(gomock.Any(), 7, "The reachable advisories changed in [this run]("+runURL+").\n"+
			"\nNew: GO-3 in kernel\n"+
			"\nNo longer found: GO-1 in kernel\n").Return(nil),
		tracker.EXPECT().Edit(gomock.Any(), 7, vulncheck.Body(results, runURL)).Return(nil),
	)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, results, runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.Outcome{Action: vulncheck.ActionChanged, Issue: 7}, outcome)
}

func TestReconcileIssueWithoutAMarkerIsReplacedWithoutAComment(t *testing.T) {
	results := findings("GO-1")
	tracker := NewMockTracker(gomock.NewController(t))

	// No Comment expected: a hand-edited body has no history to compare against.
	tracker.EXPECT().Find(gomock.Any(), vulncheck.Label).Return(vulncheck.Issue{Number: 7, Body: "By hand."}, true, nil)
	tracker.EXPECT().Edit(gomock.Any(), 7, vulncheck.Body(results, "")).Return(nil)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, results, "")
	require.NoError(t, err)

	assert.Equal(t, vulncheck.ActionUpdated, outcome.Action)
}

func TestReconcileAdvisorySpreadingToAnotherModuleComments(t *testing.T) {
	previous := findings("GO-1")
	results := append(findings("GO-1"), vulncheck.Result{
		Dir:        "log",
		Advisories: []vulncheck.Advisory{{ID: "GO-1", Dependency: "golang.org/x/net", Version: "v0.22.0"}},
	})
	tracker := NewMockTracker(gomock.NewController(t))

	gomock.InOrder(
		tracker.EXPECT().
			Find(gomock.Any(), vulncheck.Label).
			Return(vulncheck.Issue{Number: 7, Body: vulncheck.Markdown(previous)}, true, nil),
		tracker.EXPECT().
			Comment(gomock.Any(), 7, "The reachable advisories changed in [this run]("+runURL+").\n\nNew: GO-1 in log\n").
			Return(nil),
		tracker.EXPECT().Edit(gomock.Any(), 7, vulncheck.Body(results, runURL)).Return(nil),
	)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, results, runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.ActionChanged, outcome.Action)
}

func TestReconcileNoFindingsClosesThenComments(t *testing.T) {
	tracker := NewMockTracker(gomock.NewController(t))

	gomock.InOrder(
		tracker.EXPECT().
			Find(gomock.Any(), vulncheck.Label).
			Return(vulncheck.Issue{Number: 7, Body: vulncheck.Markdown(findings("GO-1"))}, true, nil),
		tracker.EXPECT().Close(gomock.Any(), 7).Return(nil),
		tracker.EXPECT().
			Comment(gomock.Any(), 7, "No reachable vulnerabilities in [this run]("+runURL+").").
			Return(nil),
	)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, clean(), runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.Outcome{Action: vulncheck.ActionClosed, Issue: 7}, outcome)
}

func TestReconcileNoFindingsWithoutAnIssueDoesNothing(t *testing.T) {
	tracker := NewMockTracker(gomock.NewController(t))

	tracker.EXPECT().Find(gomock.Any(), vulncheck.Label).Return(vulncheck.Issue{}, false, nil)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, clean(), runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.Outcome{Action: vulncheck.ActionNone}, outcome)
}

func TestReconcileTrackerErrors(t *testing.T) {
	tracker := NewMockTracker(gomock.NewController(t))

	tracker.EXPECT().Find(gomock.Any(), vulncheck.Label).Return(vulncheck.Issue{}, false, errTracker)

	_, err := vulncheck.Reconcile(t.Context(), tracker, findings("GO-1"), runURL)
	require.ErrorIs(t, err, errTracker)

	tracker.EXPECT().Find(gomock.Any(), vulncheck.Label).Return(vulncheck.Issue{}, false, nil)
	tracker.EXPECT().EnsureLabel(gomock.Any(), vulncheck.Label).Return(errTracker)

	_, err = vulncheck.Reconcile(t.Context(), tracker, findings("GO-1"), runURL)
	require.ErrorIs(t, err, errTracker)
}

func TestReconcileFailedCommentLeavesTheBodyAlone(t *testing.T) {
	tracker := NewMockTracker(gomock.NewController(t))

	// No Edit expected: the old marker stays, so the next run announces the change again.
	tracker.EXPECT().
		Find(gomock.Any(), vulncheck.Label).
		Return(vulncheck.Issue{Number: 7, Body: vulncheck.Markdown(findings("GO-1"))}, true, nil)
	tracker.EXPECT().Comment(gomock.Any(), 7, gomock.Any()).Return(errTracker)

	_, err := vulncheck.Reconcile(t.Context(), tracker, findings("GO-2"), runURL)
	require.ErrorIs(t, err, errTracker)
}

func TestReconcileFailedCloseDoesNotComment(t *testing.T) {
	tracker := NewMockTracker(gomock.NewController(t))

	// No Comment expected: the next run retries the close without a second comment.
	tracker.EXPECT().
		Find(gomock.Any(), vulncheck.Label).
		Return(vulncheck.Issue{Number: 7, Body: vulncheck.Markdown(findings("GO-1"))}, true, nil)
	tracker.EXPECT().Close(gomock.Any(), 7).Return(errTracker)

	_, err := vulncheck.Reconcile(t.Context(), tracker, clean(), runURL)
	require.ErrorIs(t, err, errTracker)
}

func TestRunScanErrorLeavesTheTrackerUntouched(t *testing.T) {
	// No expectations: any call fails the test.
	tracker := NewMockTracker(gomock.NewController(t))

	results, _, err := vulncheck.Run(t.Context(), vulncheck.Options{
		Workspace: workspaceOf(t, "cli", "kernel"),
		Run:       runs(map[string][]byte{"cli": nil}),
		Tracker:   tracker,
		RunURL:    runURL,
	})

	require.ErrorIs(t, err, vulncheck.ErrIncomplete)
	assert.Equal(t, []vulncheck.Result{{Dir: "cli"}}, results, "the completed scans, for an incomplete report")
}

func TestRunReconciles(t *testing.T) {
	tracker := NewMockTracker(gomock.NewController(t))

	tracker.EXPECT().Find(gomock.Any(), vulncheck.Label).Return(vulncheck.Issue{}, false, nil)

	results, outcome, err := vulncheck.Run(t.Context(), vulncheck.Options{
		Workspace: workspaceOf(t, "cli"),
		Run:       runs(map[string][]byte{"cli": nil}),
		Tracker:   tracker,
	})
	require.NoError(t, err)

	assert.Equal(t, []vulncheck.Result{{Dir: "cli"}}, results)
	assert.Equal(t, vulncheck.ActionNone, outcome.Action)
}

func TestRunWithoutATrackerOnlyScans(t *testing.T) {
	results, outcome, err := vulncheck.Run(t.Context(), vulncheck.Options{
		Workspace: workspaceOf(t, "cli"),
		Run:       runs(map[string][]byte{"cli": recorded(t)}),
	})
	require.NoError(t, err)

	assert.Equal(t, []vulncheck.Result{{Dir: "cli", Advisories: []vulncheck.Advisory{reachable}}}, results)
	assert.Equal(t, vulncheck.Outcome{}, outcome)
}

func TestRunURL(t *testing.T) {
	env := map[string]string{
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "sikhlana/pkg",
		"GITHUB_RUN_ID":     "42",
	}

	assert.Equal(t, runURL, vulncheck.RunURL(func(k string) string { return env[k] }))
	assert.Empty(t, vulncheck.RunURL(func(string) string { return "" }))
}

// workspaceOf returns a workspace rooted in a temporary directory with a library module in each of dirs and no tools.
func workspaceOf(t *testing.T, dirs ...string) workspace.Workspace {
	t.Helper()

	modules := make([]workspace.Module, 0, len(dirs))

	for _, dir := range dirs {
		modules = append(modules, workspace.Module{Dir: dir})
	}

	return workspace.Workspace{Root: t.TempDir(), Modules: modules}
}

func TestReconcileDigestMarkerDetectsAChangeWithoutListingIt(t *testing.T) {
	previous := manyFindings(0)
	results := manyFindings(1)
	tracker := NewMockTracker(gomock.NewController(t))

	gomock.InOrder(
		tracker.EXPECT().
			Find(gomock.Any(), vulncheck.Label).
			Return(vulncheck.Issue{Number: 7, Body: vulncheck.Body(previous, runURL)}, true, nil),
		tracker.EXPECT().
			Comment(gomock.Any(), 7, "The reachable advisories changed in [this run]("+runURL+").\n"+
				"\nThere are too many findings to list here; the step summary in [this run]("+runURL+") has them.\n").
			Return(nil),
		tracker.EXPECT().Edit(gomock.Any(), 7, vulncheck.Body(results, runURL)).Return(nil),
	)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, results, runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.ActionChanged, outcome.Action)
}

func TestReconcileDigestMarkerUnchangedDoesNotComment(t *testing.T) {
	results := manyFindings(0)
	tracker := NewMockTracker(gomock.NewController(t))

	tracker.EXPECT().
		Find(gomock.Any(), vulncheck.Label).
		Return(vulncheck.Issue{Number: 7, Body: vulncheck.Body(results, runURL)}, true, nil)
	tracker.EXPECT().Edit(gomock.Any(), 7, vulncheck.Body(results, runURL)).Return(nil)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, results, runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.ActionUpdated, outcome.Action)
}

// manyFindings returns enough findings that the marker holds a digest, with offset shifting every advisory ID so two
// offsets give different findings.
func manyFindings(offset int) []vulncheck.Result {
	results := make([]vulncheck.Result, 0, 200)

	for i := range 200 {
		advisories := make([]vulncheck.Advisory, 0, 10)

		for j := range 10 {
			advisories = append(advisories, vulncheck.Advisory{ID: fmt.Sprintf("GO-2026-%04d", j+offset)})
		}

		results = append(results, vulncheck.Result{Dir: fmt.Sprintf("module%03d", i), Advisories: advisories})
	}

	return results
}

func TestReconcileFixLandingComments(t *testing.T) {
	previous := findings("GO-1")
	results := findings("GO-1")
	results[1].Advisories[0].Fixed = "v0.61.0"
	tracker := NewMockTracker(gomock.NewController(t))

	gomock.InOrder(
		tracker.EXPECT().
			Find(gomock.Any(), vulncheck.Label).
			Return(vulncheck.Issue{Number: 7, Body: vulncheck.Markdown(previous)}, true, nil),
		tracker.EXPECT().
			Comment(gomock.Any(), 7, "The reachable advisories changed in [this run]("+runURL+").\n"+
				"\nFix available: GO-1 in kernel (fixed in golang.org/x/net v0.61.0)\n").
			Return(nil),
		tracker.EXPECT().Edit(gomock.Any(), 7, vulncheck.Body(results, runURL)).Return(nil),
	)

	outcome, err := vulncheck.Reconcile(t.Context(), tracker, results, runURL)
	require.NoError(t, err)

	assert.Equal(t, vulncheck.ActionChanged, outcome.Action)
}

func TestReconcileFixWithdrawnComments(t *testing.T) {
	previous := findings("GO-1")
	previous[1].Advisories[0].Fixed = "v0.61.0"
	results := findings("GO-1")
	tracker := NewMockTracker(gomock.NewController(t))

	tracker.EXPECT().
		Find(gomock.Any(), vulncheck.Label).
		Return(vulncheck.Issue{Number: 7, Body: vulncheck.Markdown(previous)}, true, nil)
	tracker.EXPECT().
		Comment(gomock.Any(), 7, "The reachable advisories changed in [this run]("+runURL+").\n"+
			"\nFix no longer listed: GO-1 in kernel\n").
		Return(nil)
	tracker.EXPECT().Edit(gomock.Any(), 7, vulncheck.Body(results, runURL)).Return(nil)

	_, err := vulncheck.Reconcile(t.Context(), tracker, results, runURL)
	require.NoError(t, err)
}
