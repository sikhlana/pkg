package vulncheck_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/vulncheck"
)

const runURL = "https://github.com/sikhlana/pkg/actions/runs/42"

func findings(ids ...string) []vulncheck.Result {
	advisories := make([]vulncheck.Advisory, 0, len(ids))

	for _, id := range ids {
		advisories = append(advisories, vulncheck.Advisory{ID: id, Dependency: "golang.org/x/net", Version: "v0.22.0"})
	}

	return []vulncheck.Result{{Dir: "cli"}, {Dir: "kernel", Advisories: advisories}}
}

func clean() []vulncheck.Result {
	return []vulncheck.Result{{Dir: "cli"}, {Dir: "kernel"}}
}

// recorded is govulncheck v1.9.0's -format json output for a module that calls
// golang.org/x/net/html.Parse at v0.22.0 (GO-2026-4441, reachable) and
// requires golang.org/x/crypto v0.30.0 without importing its vulnerable
// package (GO-2024-3321, unreachable), trimmed to those two advisories.
func recorded(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "govulncheck.json"))
	require.NoError(t, err)

	return data
}

var reachable = vulncheck.Advisory{
	ID:         "GO-2026-4441",
	Summary:    "Infinite parsing loop in golang.org/x/net",
	URL:        "https://pkg.go.dev/vuln/GO-2026-4441",
	Dependency: "golang.org/x/net",
	Version:    "v0.22.0",
	Fixed:      "v0.45.0",
}

var errScan = errors.New("vulnerability database unreachable")

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
