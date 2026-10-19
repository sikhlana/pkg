package vulncheck_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/sikhlana/pkg/tools/release/vulncheck"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

func TestRunScanErrorLeavesTheTrackerUntouched(t *testing.T) {
	// No expectations: any call fails the test.
	tracker := NewMockTracker(gomock.NewController(t))

	results, _, err := vulncheck.Run(t.Context(), vulncheck.Options{
		Jobs:      4,
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
		Jobs:      4,
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
		Jobs:      4,
		Workspace: workspaceOf(t, "cli"),
		Run:       runs(map[string][]byte{"cli": recorded(t)}),
	})
	require.NoError(t, err)

	assert.Equal(t, []vulncheck.Result{{Dir: "cli", Advisories: []vulncheck.Advisory{reachable}}}, results)
	assert.Equal(t, vulncheck.Outcome{}, outcome)
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
