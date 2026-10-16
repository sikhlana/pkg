//go:build unix

package tint_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/sikhlana/pkg/log/tint"
)

func TestNewLeavesANonBlockingFileNonBlocking(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	r, w, err := os.Pipe()
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})

	require.True(t, nonBlocking(t, w), "os.Pipe returns a non-blocking file")

	tint.New(w, nil)

	assert.True(t, nonBlocking(t, w))

	_ = w.Fd()

	assert.False(t, nonBlocking(t, w), "os.File.Fd, which New avoids, makes it blocking")
}

// nonBlocking reports whether f's descriptor is in non-blocking mode.
func nonBlocking(t *testing.T, f *os.File) bool {
	t.Helper()

	conn, err := f.SyscallConn()
	require.NoError(t, err)

	var (
		flags    int
		flagsErr error
	)

	require.NoError(t, conn.Control(func(fd uintptr) {
		flags, flagsErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
	}))
	require.NoError(t, flagsErr)

	return flags&unix.O_NONBLOCK != 0
}
