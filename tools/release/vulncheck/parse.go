package vulncheck

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Advisory is a known vulnerability reachable from a module's code.
type Advisory struct {
	// ID is the Go vulnerability database ID, such as GO-2026-4441.
	ID string
	// Summary is the advisory's one-line summary.
	Summary string
	// URL is the advisory's page on pkg.go.dev.
	URL string
	// Dependency is the vulnerable module's path, or stdlib for the standard
	// library.
	Dependency string
	// Version is the version of Dependency the module builds with.
	Version string
	// Fixed is the lowest version of Dependency with the fix, or empty when
	// there's none yet.
	Fixed string
}

// Result is one module's scan.
type Result struct {
	// Dir is the module's slash-separated directory relative to the
	// repository root.
	Dir string
	// Advisories are the reachable vulnerabilities, sorted by ID and then
	// dependency.
	Advisories []Advisory
}

// The fields of govulncheck's -format json messages that Parse reads. The
// stream holds other messages (config, progress, SBOM), which decode as
// empty.
type (
	message struct {
		OSV     *osv     `json:"osv"`
		Finding *finding `json:"finding"`
	}

	osv struct {
		ID               string `json:"id"`
		Summary          string `json:"summary"`
		DatabaseSpecific struct {
			URL string `json:"url"`
		} `json:"database_specific"`
	}

	finding struct {
		OSV          string  `json:"osv"`
		FixedVersion string  `json:"fixed_version"`
		Trace        []frame `json:"trace"`
	}

	frame struct {
		Module   string `json:"module"`
		Version  string `json:"version"`
		Function string `json:"function"`
	}
)

// Parse reads govulncheck's -format json stream and returns the advisories
// reachable from the scanned module's code: those with a finding whose trace
// starts at a vulnerable function, which is what govulncheck's text output
// reports as affecting the code. Findings that stop at a required module or
// an imported package are dropped.
func Parse(r io.Reader) ([]Advisory, error) {
	osvs := map[string]osv{}

	var findings []finding

	dec := json.NewDecoder(r)

	for {
		var m message

		err := dec.Decode(&m)

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("decoding govulncheck output: %w", err)
		}

		if m.OSV != nil {
			osvs[m.OSV.ID] = *m.OSV
		}

		if m.Finding != nil && len(m.Finding.Trace) > 0 && m.Finding.Trace[0].Function != "" {
			findings = append(findings, *m.Finding)
		}
	}

	var advisories []Advisory

	for _, f := range findings {
		vulnerable := f.Trace[0]

		a := Advisory{
			ID:         f.OSV,
			Summary:    osvs[f.OSV].Summary,
			URL:        cmp.Or(osvs[f.OSV].DatabaseSpecific.URL, "https://pkg.go.dev/vuln/"+f.OSV),
			Dependency: vulnerable.Module,
			Version:    vulnerable.Version,
			Fixed:      f.FixedVersion,
		}

		// govulncheck reports a finding per call path; the advisory is the same.
		if !slices.Contains(advisories, a) {
			advisories = append(advisories, a)
		}
	}

	slices.SortFunc(advisories, func(a, b Advisory) int {
		return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Dependency, b.Dependency))
	})

	return advisories, nil
}

// fixSuffix ends a finding whose advisory has a fixed version.
const fixSuffix = "+fix"

// Findings returns every module's advisories in results as dir:ID pairs, such as kernel:GO-2026-1234, sorted, each
// once, with +fix appended when the advisory has a fixed version (kernel:GO-2026-1234+fix), so a fix landing counts
// as a change. An advisory reachable from two modules is two findings.
func Findings(results []Result) []string {
	fixed := make(map[string]bool)

	for _, r := range results {
		for _, a := range r.Advisories {
			key := r.Dir + ":" + a.ID
			fixed[key] = fixed[key] || a.Fixed != ""
		}
	}

	findings := make([]string, 0, len(fixed))

	for key, hasFix := range fixed {
		if hasFix {
			key += fixSuffix
		}

		findings = append(findings, key)
	}

	slices.Sort(findings)

	return findings
}

// splitFinding returns a finding's dir:ID key and whether its advisory has a fix.
func splitFinding(finding string) (string, bool) {
	return strings.CutSuffix(finding, fixSuffix)
}

// IDs returns the IDs of every advisory in results, sorted, each once.
func IDs(results []Result) []string {
	var ids []string

	for _, r := range results {
		for _, a := range r.Advisories {
			ids = append(ids, a.ID)
		}
	}

	slices.Sort(ids)

	return slices.Compact(ids)
}
