package workspace_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

func load(t *testing.T, f fixture.Repo) workspace.Workspace {
	t.Helper()

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	return ws
}

func dirs(modules []workspace.Module) []string {
	var out []string

	for _, m := range modules {
		out = append(out, m.Dir)
	}

	return out
}
