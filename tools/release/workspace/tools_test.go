package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/workspace"
)

func TestToolDirsSkipsWhatTheGoCommandSkips(t *testing.T) {
	root := t.TempDir()

	for _, dir := range []string{"release", "vet", "_attic", ".cache", "testdata"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, workspace.ToolsDir, dir), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, workspace.ToolsDir, dir, workspace.GoModFile), nil, 0o600))
	}

	require.NoError(t, os.MkdirAll(filepath.Join(root, workspace.ToolsDir, "notes"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, workspace.ToolsDir, "README.md"), nil, 0o600))

	dirs, err := workspace.ToolDirs(root)
	require.NoError(t, err)

	assert.Equal(t, []string{"tools/release", "tools/vet"}, dirs)
}

func TestToolDirsWithoutATools(t *testing.T) {
	dirs, err := workspace.ToolDirs(t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, dirs)
}
