package cli_test

import (
	"context"
	"errors"
	"fmt"
	"io"
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

	root := &cobra.Command{Use: "myapp"}

	root.AddCommand(&cobra.Command{
		Use: "serve",
		Run: func(cmd *cobra.Command, _ []string) {
			got = cmd.Context().Value(key{})
		},
	})

	root.SetArgs([]string{"serve"})

	ctx := context.WithValue(t.Context(), key{}, "value")

	require.Equal(t, 0, cli.Execute(ctx, root))
	assert.Equal(t, "value", got)
}

func TestExitErrorWithoutCause(t *testing.T) {
	assert.EqualError(t, cli.Exit(2, nil), "exit status 2")
}

func TestExitErrorWrapsCause(t *testing.T) {
	cause := errors.New("boom")

	err := cli.Exit(3, cause)

	require.ErrorIs(t, err, cause)
	assert.EqualError(t, err, "boom")
}
