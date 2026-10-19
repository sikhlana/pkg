package vulncheck_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/vulncheck"
)

func TestParseKeepsOnlyReachableFindings(t *testing.T) {
	advisories, err := vulncheck.Parse(bytes.NewReader(recorded(t)))
	require.NoError(t, err)

	assert.Equal(t, []vulncheck.Advisory{reachable}, advisories)
}

func TestParseMergesCallPaths(t *testing.T) {
	stream := `{"osv":{"id":"GO-1","summary":"S"}}
{"finding":{"osv":"GO-1","fixed_version":"v1.1.0","trace":[{"module":"m","version":"v1.0.0","function":"F"},{"function":"a"}]}}
{"finding":{"osv":"GO-1","fixed_version":"v1.1.0","trace":[{"module":"m","version":"v1.0.0","function":"F"},{"function":"b"}]}}
{"finding":{"osv":"GO-0","trace":[{"module":"stdlib","version":"v1.27.0","function":"G"}]}}
`

	advisories, err := vulncheck.Parse(strings.NewReader(stream))
	require.NoError(t, err)

	assert.Equal(t, []vulncheck.Advisory{
		{ID: "GO-0", URL: "https://pkg.go.dev/vuln/GO-0", Dependency: "stdlib", Version: "v1.27.0"},
		{
			ID:         "GO-1",
			Summary:    "S",
			URL:        "https://pkg.go.dev/vuln/GO-1",
			Dependency: "m",
			Version:    "v1.0.0",
			Fixed:      "v1.1.0",
		},
	}, advisories)
}

func TestParseEmptyStream(t *testing.T) {
	advisories, err := vulncheck.Parse(strings.NewReader(""))
	require.NoError(t, err)

	assert.Empty(t, advisories)
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := vulncheck.Parse(strings.NewReader(`{"config":`))
	require.ErrorContains(t, err, "decoding govulncheck output")
}

func TestFindings(t *testing.T) {
	findings := vulncheck.Findings([]vulncheck.Result{
		{Dir: "a", Advisories: []vulncheck.Advisory{{ID: "GO-2"}, {ID: "GO-1"}}},
		{Dir: "b"},
		{Dir: "c/d", Advisories: []vulncheck.Advisory{{ID: "GO-2"}, {ID: "GO-2"}}},
	})

	assert.Equal(t, []string{"a:GO-1", "a:GO-2", "c/d:GO-2"}, findings)
}

func TestFindingsMarkFixedAdvisories(t *testing.T) {
	findings := vulncheck.Findings([]vulncheck.Result{
		{Dir: "a", Advisories: []vulncheck.Advisory{
			{ID: "GO-1", Fixed: "v1.2.3"},
			{ID: "GO-2"},
			// One dependency fixed, another not: the advisory has a fix to apply.
			{ID: "GO-3", Dependency: "x"},
			{ID: "GO-3", Dependency: "y", Fixed: "v0.2.0"},
		}},
	})

	assert.Equal(t, []string{"a:GO-1+fix", "a:GO-2", "a:GO-3+fix"}, findings)
}

func TestIDs(t *testing.T) {
	ids := vulncheck.IDs([]vulncheck.Result{
		{Dir: "a", Advisories: []vulncheck.Advisory{{ID: "GO-2"}, {ID: "GO-1"}}},
		{Dir: "b"},
		{Dir: "c", Advisories: []vulncheck.Advisory{{ID: "GO-2"}, {ID: "GO-3"}}},
	})

	assert.Equal(t, []string{"GO-1", "GO-2", "GO-3"}, ids)
}
