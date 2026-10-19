package main

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/parallel"
)

func TestJobsBelowOneIsRejectedBeforeAnyWork(t *testing.T) {
	for _, command := range []string{"check", "publish", "vulncheck"} {
		t.Run(command, func(t *testing.T) {
			// Outside a repository, any work would fail on opening it first.
			t.Chdir(t.TempDir())

			root := newRootCommand()
			root.SetArgs([]string{command, "--jobs", "0"})

			err := root.ExecuteContext(t.Context())
			require.ErrorIs(t, err, errJobs)
			require.ErrorContains(t, err, "--jobs must be at least 1, not 0")
		})
	}
}

func TestJobsMustBeANumber(t *testing.T) {
	t.Chdir(t.TempDir())

	root := newRootCommand()
	root.SetArgs([]string{"check", "--jobs", "many"})

	err := root.ExecuteContext(t.Context())
	require.ErrorIs(t, err, errJobs)
	require.ErrorContains(t, err, `--jobs must be at least 1, not "many"`)
}

func TestJobsDefaultsToGOMAXPROCS(t *testing.T) {
	cmd := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
	jobs := jobsFlag(cmd, "how many")

	cmd.SetArgs(nil)
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	assert.Equal(t, runtime.GOMAXPROCS(0), *jobs)
	assert.Equal(t, strconv.Itoa(runtime.GOMAXPROCS(0)), cmd.Flags().Lookup("jobs").DefValue)
}

func TestPrintPanicStack(t *testing.T) {
	var out bytes.Buffer

	err := fmt.Errorf("checking logger: %w", &parallel.PanicError{Value: "boom", Stack: []byte("goroutine 7\n")})
	printPanicStack(&out, errors.Join(errors.New("other"), err))

	assert.Equal(t, "goroutine 7\n", out.String())
}

func TestPrintPanicStackPrintsEveryPanic(t *testing.T) {
	var out bytes.Buffer

	first := fmt.Errorf("checking logger: %w", &parallel.PanicError{Value: "boom", Stack: []byte("goroutine 7\n")})
	second := fmt.Errorf("checking kernel: %w", &parallel.PanicError{Value: "bang", Stack: []byte("goroutine 9\n")})
	printPanicStack(&out, errors.Join(first, errors.New("other"), second))

	assert.Equal(t, "goroutine 7\ngoroutine 9\n", out.String())
}

func TestPrintPanicStackWithoutAPanic(t *testing.T) {
	var out bytes.Buffer

	printPanicStack(&out, errors.New("other"))

	assert.Empty(t, out.String())
}
