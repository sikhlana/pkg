package main

import (
	"fmt"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"
)

// jobsFlag adds --jobs to cmd, defaulting to GOMAXPROCS, and returns where
// its value lands. A value below 1 fails as the flag is parsed, before cmd
// does any work.
func jobsFlag(cmd *cobra.Command, usage string) *int {
	n := runtime.GOMAXPROCS(0)
	cmd.Flags().Var(jobsValue{n: &n}, "jobs", usage)

	return &n
}

// jobsValue is --jobs: a count of at least 1, checked when it's set.
type jobsValue struct {
	n *int
}

func (v jobsValue) Set(s string) error {
	n, err := strconv.Atoi(s)

	switch {
	case err != nil:
		return fmt.Errorf("%w, not %q", errJobs, s)
	case n < 1:
		return fmt.Errorf("%w, not %d", errJobs, n)
	}

	*v.n = n

	return nil
}

func (v jobsValue) String() string {
	return strconv.Itoa(*v.n)
}

func (jobsValue) Type() string {
	return "int"
}
