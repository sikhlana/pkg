package vulncheck_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/parallel"
	"github.com/sikhlana/pkg/tools/release/vulncheck"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

var errScan = errors.New("vulnerability database unreachable")

// fake writes a shell script standing in for govulncheck.
func fake(t *testing.T, script string) vulncheck.Govulncheck {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "govulncheck")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o700))

	g, err := vulncheck.NewGovulncheck(bin)
	require.NoError(t, err)

	return g
}

// runs returns a RunFunc answering from outputs, keyed by the directory's
// base name, and failing with errScan for a directory it lacks.
func runs(outputs map[string][]byte) vulncheck.RunFunc {
	return func(_ context.Context, dir string) ([]byte, error) {
		out, ok := outputs[filepath.Base(dir)]

		if !ok {
			return nil, errScan
		}

		return out, nil
	}
}

func TestGovulncheckRunPassesJSONModeWithTheWorkspaceOff(t *testing.T) {
	dir := t.TempDir()

	out, err := fake(t, `echo "$* GOWORK=$GOWORK PWD=$PWD"`+"\n").Run(t.Context(), dir)
	require.NoError(t, err)

	// PWD is the scanned directory as given, not the caller's, and the tool pattern covers pinned tools.
	assert.Equal(t, "-format json ./... tool GOWORK=off PWD="+dir+"\n", string(out))
}

func TestGovulncheckRunFails(t *testing.T) {
	_, err := fake(t, "echo 'fetching database' >&2\nexit 1\n").Run(t.Context(), t.TempDir())
	require.ErrorContains(t, err, "running govulncheck in")
	require.ErrorContains(t, err, "fetching database")
}

func TestNewGovulncheckWithoutTheBinary(t *testing.T) {
	_, err := vulncheck.NewGovulncheck(filepath.Join(t.TempDir(), "missing"))
	require.ErrorContains(t, err, "finding govulncheck")
}

func TestNewGovulncheckResolvesARelativePath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "govulncheck"), []byte("#!/bin/sh\necho '{}'\n"), 0o700))

	t.Chdir(dir)

	g, err := vulncheck.NewGovulncheck("./govulncheck")
	require.NoError(t, err)

	// Neither the working directory nor the scanned one holds ./govulncheck any more.
	t.Chdir(t.TempDir())

	out, err := g.Run(t.Context(), t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "{}\n", string(out))
}

func TestDirsListsModulesThenTools(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.AddModule("http/server", "0.1.0")
	f.Write("tools/lint/go.mod", "module "+fixture.ModulePrefix+"/tools/lint\n\ngo "+fixture.GoVersion+"\n")
	f.Write("tools/scripts/run.sh", "#!/bin/sh\n")
	f.Write("tools/README.md", "# tools\n")

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	dirs, err := vulncheck.Dirs(ws)
	require.NoError(t, err)

	assert.Equal(t, []string{"http/server", "logger", "tools/lint", "tools/release"}, dirs)
}

func TestScan(t *testing.T) {
	results, err := vulncheck.Scan(
		t.Context(),
		t.TempDir(),
		[]string{"logger", "tools/release"},
		4,
		runs(map[string][]byte{
			"logger":  recorded(t),
			"release": nil,
		}),
	)
	require.NoError(t, err)

	assert.Equal(t, []vulncheck.Result{
		{Dir: "logger", Advisories: []vulncheck.Advisory{reachable}},
		{Dir: "tools/release"},
	}, results)
}

func TestScanFailsOnAnyError(t *testing.T) {
	results, err := vulncheck.Scan(
		t.Context(),
		t.TempDir(),
		[]string{"logger", "kernel", "cli"},
		4,
		runs(map[string][]byte{
			"logger": recorded(t),
			"cli":    []byte("{"),
		}),
	)

	require.ErrorIs(t, err, vulncheck.ErrIncomplete)
	require.ErrorIs(t, err, errScan)
	require.ErrorContains(t, err, "scanning kernel")
	require.ErrorContains(t, err, "scanning cli: decoding govulncheck output")
	assert.Equal(t, []vulncheck.Result{{Dir: "logger", Advisories: []vulncheck.Advisory{reachable}}}, results,
		"the scans that completed are still returned, for an incomplete report")
}

func TestScanOverlaps(t *testing.T) {
	dirs := []string{"cli", "kernel", "logger"}

	var wg sync.WaitGroup

	wg.Add(len(dirs))

	// Every scan waits for all of them to start, so they can only finish
	// by running at once.
	results, err := vulncheck.Scan(
		t.Context(),
		t.TempDir(),
		dirs,
		len(dirs),
		func(context.Context, string) ([]byte, error) {
			wg.Done()
			wg.Wait()

			return nil, nil
		},
	)
	require.NoError(t, err)

	assert.Equal(t, []vulncheck.Result{{Dir: "cli"}, {Dir: "kernel"}, {Dir: "logger"}}, results)
}

func TestScanCancelledKeepsWhatCompleted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	results, err := vulncheck.Scan(ctx, t.TempDir(), []string{"logger", "kernel"}, 1,
		func(context.Context, string) ([]byte, error) {
			// logger finishes, then the run is interrupted before kernel starts.
			cancel()

			return recorded(t), nil
		})

	require.ErrorIs(t, err, vulncheck.ErrIncomplete)
	require.ErrorIs(t, err, parallel.ErrNotRun)
	require.ErrorContains(t, err, "scanning kernel")
	assert.Equal(t, []vulncheck.Result{{Dir: "logger", Advisories: []vulncheck.Advisory{reachable}}}, results,
		"the completed scan is kept, and the one that never ran isn't reported as clean")
}

func TestScanPanicKeepsTheOthers(t *testing.T) {
	results, err := vulncheck.Scan(t.Context(), t.TempDir(), []string{"logger", "kernel", "cli"}, 2,
		func(_ context.Context, dir string) ([]byte, error) {
			if filepath.Base(dir) == "kernel" {
				panic("boom")
			}

			return nil, nil
		})

	require.ErrorIs(t, err, vulncheck.ErrIncomplete)
	require.ErrorIs(t, err, parallel.ErrPanic)
	require.ErrorContains(t, err, "scanning kernel: panicked: boom")
	assert.Equal(t, []vulncheck.Result{{Dir: "logger"}, {Dir: "cli"}}, results)
}
