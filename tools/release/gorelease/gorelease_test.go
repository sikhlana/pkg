package gorelease_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/gorelease"
)

// fake writes a shell script standing in for gorelease and returns a Runner
// for it.
func fake(t *testing.T, script string) gorelease.Runner {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "gorelease")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o700))

	return gorelease.Runner{Path: bin}
}

func TestRunParsesTheSuggestion(t *testing.T) {
	runner := fake(t, "echo '# summary'\necho 'Suggested version: v0.2.0 (with tag logger/v0.2.0)'\n")

	result, err := runner.Run(t.Context(), t.TempDir(), "v0.1.0")
	require.NoError(t, err)

	assert.Equal(t, "v0.2.0", result.Suggested)
	assert.False(t, result.Failed)
	assert.False(t, result.Incompatible)
}

func TestRunPassesTheBaseWithTheWorkspaceOff(t *testing.T) {
	runner := fake(t, `echo "$1 GOWORK=$GOWORK"`+"\n")

	result, err := runner.Run(t.Context(), t.TempDir(), gorelease.BaseNone)
	require.NoError(t, err)

	assert.Equal(t, "-base=none GOWORK=off", result.Output)
}

func TestRunReportsAFailedComparison(t *testing.T) {
	runner := fake(t, "echo 'Incompatible changes were detected.'\nexit 1\n")

	result, err := runner.Run(t.Context(), t.TempDir(), "v1.2.0")
	require.NoError(t, err)

	assert.True(t, result.Failed)
	assert.True(t, result.Incompatible)
	assert.Empty(t, result.Suggested)
}

func TestRunTimesOut(t *testing.T) {
	runner := fake(t, "echo 'fetching'\nexec sleep 30\n")
	runner.Timeout = 3 * time.Second

	start := time.Now()

	_, err := runner.Run(t.Context(), t.TempDir(), "v0.1.0")

	require.ErrorIs(t, err, gorelease.ErrTimeout)
	assert.Contains(t, err.Error(), "fetching", "the output so far shows where it stalled")
	assert.Less(t, time.Since(start), 15*time.Second)
}

func TestRunWithoutTheBinary(t *testing.T) {
	runner := gorelease.Runner{Path: filepath.Join(t.TempDir(), "missing")}

	_, err := runner.Run(t.Context(), t.TempDir(), "v0.1.0")
	require.ErrorContains(t, err, "running gorelease")
}
