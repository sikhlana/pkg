package vulncheck_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/vulncheck"
)

func TestMarkdownWithFindings(t *testing.T) {
	md := vulncheck.Markdown([]vulncheck.Result{
		{Dir: "cli"},
		{Dir: "kernel", Advisories: []vulncheck.Advisory{
			reachable,
			{
				ID:         "GO-2026-0001",
				Summary:    "A | B",
				URL:        "https://pkg.go.dev/vuln/GO-2026-0001",
				Dependency: "stdlib",
				Version:    "v1.27.0",
			},
		}},
	})

	assert.Equal(t, `## Vulnerabilities

govulncheck scanned 2 modules: cli, kernel.

2 reachable vulnerabilities in 1 module.

### kernel

| Advisory | Summary | Dependency | Fixed in |
|---|---|---|---|
| [GO-2026-4441](https://pkg.go.dev/vuln/GO-2026-4441) | Infinite parsing loop in golang.org/x/net | golang.org/x/net v0.22.0 | v0.45.0 |
| [GO-2026-0001](https://pkg.go.dev/vuln/GO-2026-0001) | A \| B | stdlib v1.27.0 | no fix yet |

<!-- govulncheck-findings: kernel:GO-2026-0001,kernel:GO-2026-4441+fix -->
`, md)
}

func TestMarkdownWithoutFindings(t *testing.T) {
	md := vulncheck.Markdown([]vulncheck.Result{{Dir: "cli"}})

	assert.Equal(t, `## Vulnerabilities

govulncheck scanned 1 module: cli.

No reachable vulnerabilities.

<!-- govulncheck-findings:  -->
`, md)
}

func TestMarkerRoundTrip(t *testing.T) {
	results := []vulncheck.Result{
		{Dir: "a", Advisories: []vulncheck.Advisory{{ID: "GO-2"}, {ID: "GO-1"}}},
		{Dir: "b", Advisories: []vulncheck.Advisory{{ID: "GO-3"}}},
	}

	ids, ok := vulncheck.ParseMarker("Edited by hand.\n\n" + vulncheck.Markdown(results))

	assert.True(t, ok)
	assert.Equal(t, []string{"a:GO-1", "a:GO-2", "b:GO-3"}, ids)

	ids, ok = vulncheck.ParseMarker(vulncheck.Markdown([]vulncheck.Result{{Dir: "a"}}))

	assert.True(t, ok)
	assert.Empty(t, ids)
}

func TestParseMarkerWithoutAMarker(t *testing.T) {
	ids, ok := vulncheck.ParseMarker("An issue someone wrote by hand.")

	assert.False(t, ok)
	assert.Nil(t, ids)
}

func TestParseMarkerReadsTheLastMarker(t *testing.T) {
	body := "Pasted from an older report:\n<!-- govulncheck-findings: log:GO-9 -->\n\n" +
		vulncheck.Markdown(findings("GO-1"))

	ids, ok := vulncheck.ParseMarker(body)

	require.True(t, ok)
	assert.Equal(t, []string{"kernel:GO-1"}, ids)
}

func TestBodyFitsInAnIssue(t *testing.T) {
	small := findings("GO-1")

	assert.Equal(t, vulncheck.Markdown(small), vulncheck.Body(small, runURL), "a report that fits is used as is")

	var large []vulncheck.Result

	for i := range 400 {
		advisories := make([]vulncheck.Advisory, 0, 20)

		for j := range 20 {
			advisories = append(advisories, vulncheck.Advisory{
				ID:         fmt.Sprintf("GO-2026-%04d", j),
				Summary:    strings.Repeat("A long advisory summary. ", 10),
				Dependency: "golang.org/x/net",
				Version:    "v0.22.0",
			})
		}

		large = append(large, vulncheck.Result{Dir: fmt.Sprintf("module%03d", i), Advisories: advisories})
	}

	require.Greater(t, len(vulncheck.Markdown(large)), vulncheck.MaxBody)

	body := vulncheck.Body(large, runURL)

	assert.LessOrEqual(t, len(body), vulncheck.MaxBody)
	assert.Contains(t, body, "see the step summary in [this run]("+runURL+")")
	assert.Contains(t, body, "- …\n", "the module list is cut short")

	ids, ok := vulncheck.ParseMarker(body)
	require.True(t, ok)
	assert.Equal(t, vulncheck.MarkerEntries(large), ids, "the marker survives the cut")
	require.Len(t, ids, 1)
	assert.True(t, strings.HasPrefix(ids[0], "sha256:"), "too many findings to list become a digest")
}

func TestMarkerEntriesListFindingsWhenTheyFit(t *testing.T) {
	results := findings("GO-1", "GO-2")

	assert.Equal(t, vulncheck.Findings(results), vulncheck.MarkerEntries(results))
}
