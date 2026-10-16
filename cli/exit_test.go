package cli_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/cli"
)

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: 0},
		{name: "plain error", err: errors.New("boom"), want: 1},
		{name: "wrapped exit code", err: fmt.Errorf("serving: %w", cli.Exit(7, errors.New("boom"))), want: 7},
		{name: "zero code", err: cli.Exit(0, errors.New("boom")), want: 1},
		{name: "negative code", err: cli.Exit(-2, nil), want: 1},
		{name: "largest code", err: cli.Exit(255, nil), want: 255},
		{name: "code that would exit 0", err: cli.Exit(256, nil), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{
				Use: "myapp",
				RunE: func(*cobra.Command, []string) error {
					return tt.err
				},
			}

			cmd.SetArgs([]string{})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			assert.Equal(t, tt.want, cli.Execute(t.Context(), cmd))
		})
	}
}

func TestExecutePassesContext(t *testing.T) {
	type key struct{}

	var got any

	serve := &cobra.Command{
		Use: "serve",
		Run: func(cmd *cobra.Command, _ []string) {
			got = cmd.Context().Value(key{})
		},
	}

	root := &cobra.Command{Use: "myapp"}

	root.AddCommand(serve, &cobra.Command{Use: "check", Run: func(*cobra.Command, []string) {}})
	root.SetArgs([]string{"serve"})

	for _, value := range []string{"first", "second"} {
		ctx := context.WithValue(t.Context(), key{}, value)

		require.Equal(t, 0, cli.Execute(ctx, root))
		assert.Equal(t, value, got)
	}
}

func TestExecuteRunsTheRoot(t *testing.T) {
	type key struct{}

	var got any

	check := &cobra.Command{Use: "check", Run: func(*cobra.Command, []string) {}}
	root := &cobra.Command{Use: "myapp"}

	root.AddCommand(check, &cobra.Command{
		Use: "serve",
		Run: func(cmd *cobra.Command, _ []string) {
			got = cmd.Context().Value(key{})
		},
	})
	root.SetArgs([]string{"serve"})

	require.Equal(t, 0, cli.Execute(context.WithValue(t.Context(), key{}, "value"), check))
	assert.Equal(t, "value", got)
}

func TestExitErrorWithoutCause(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{code: 2, want: "exit status 2"},
		{code: 0, want: "exit status 1"},
		{code: 256, want: "exit status 1"},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			assert.EqualError(t, cli.Exit(tt.code, nil), tt.want)
		})
	}
}

func TestExitErrorWrapsCause(t *testing.T) {
	cause := errors.New("boom")

	err := cli.Exit(3, cause)

	require.ErrorIs(t, err, cause)
	require.EqualError(t, err, "boom")

	exitErr, ok := errors.AsType[*cli.ExitError](err)
	require.True(t, ok)
	assert.Equal(t, 3, exitErr.Code)
}
