package vulncheck

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/sikhlana/pkg/tools/release/report"
)

const markerPrefix = "<!-- govulncheck-findings:"

// MaxBody is the longest issue body Body returns, in bytes: GitHub refuses bodies over 65,536 characters, and this
// leaves room to spare.
const MaxBody = 60000

// maxMarkerList is the longest list of findings the marker holds, in bytes. A longer one is replaced by its digest,
// so the marker always leaves room in MaxBody for the report.
const maxMarkerList = 20000

// digestPrefix starts a marker entry that stands for a list of findings too long to hold.
const digestPrefix = "sha256:"

var marker = regexp.MustCompile(`<!-- govulncheck-findings:([^>]*)-->`)

// Markdown renders results as the report the job summary and the issue body
// show: the modules scanned, a table of advisories per affected module, and
// a hidden marker holding the findings, as Findings returns them, which ParseMarker reads back.
func Markdown(results []Result) string {
	var b strings.Builder

	b.WriteString("## Vulnerabilities\n\n")

	dirs := make([]string, 0, len(results))
	affected := 0

	for _, r := range results {
		dirs = append(dirs, r.Dir)

		if len(r.Advisories) > 0 {
			affected++
		}
	}

	ids := IDs(results)

	fmt.Fprintf(&b, "govulncheck scanned %d %s: %s.\n\n",
		len(results), plural(len(results), "module", "modules"), strings.Join(dirs, ", "))

	if len(ids) == 0 {
		b.WriteString("No reachable vulnerabilities.\n\n")
	} else {
		fmt.Fprintf(&b, "%d reachable %s in %d %s.\n\n",
			len(ids), plural(len(ids), "vulnerability", "vulnerabilities"),
			affected, plural(affected, "module", "modules"))
	}

	for _, r := range results {
		if len(r.Advisories) == 0 {
			continue
		}

		fmt.Fprintf(&b, "### %s\n\n", r.Dir)
		b.WriteString("| Advisory | Summary | Dependency | Fixed in |\n")
		b.WriteString("|---|---|---|---|\n")

		for _, a := range r.Advisories {
			fixed := a.Fixed

			if fixed == "" {
				fixed = "no fix yet"
			}

			// module@version, written bare, would turn into a mailto link.
			fmt.Fprintf(&b, "| [%s](%s) | %s | %s %s | %s |\n",
				a.ID, a.URL, report.Cell(a.Summary), a.Dependency, a.Version, fixed)
		}

		b.WriteString("\n")
	}

	b.WriteString(markerLine(MarkerEntries(results)))

	return b.String()
}

// MarkerEntries returns what the marker holds for results, and what ParseMarker reads back: the findings, as Findings
// returns them, or, when listing them would take more than maxMarkerList bytes, a single sha256: digest of them.
func MarkerEntries(results []Result) []string {
	findings := Findings(results)
	list := strings.Join(findings, ",")

	if len(list) <= maxMarkerList {
		return findings
	}

	sum := sha256.Sum256([]byte(list))

	return []string{digestPrefix + hex.EncodeToString(sum[:])}
}

// isDigest reports whether entries is a digest rather than a list of findings.
func isDigest(entries []string) bool {
	return len(entries) == 1 && strings.HasPrefix(entries[0], digestPrefix)
}

// Body returns the report as an issue body: Markdown when it fits in MaxBody, and otherwise a short form listing each
// affected module's advisory IDs, which points at runURL's step summary for the full report. Either way it ends with
// the marker.
func Body(results []Result, runURL string) string {
	if full := Markdown(results); len(full) <= MaxBody {
		return full
	}

	ids := IDs(results)
	marker := markerLine(MarkerEntries(results))

	var b strings.Builder

	fmt.Fprintf(&b, "## Vulnerabilities\n\n%d reachable %s. The full report is too long for an issue; see the step "+
		"summary %s.\n\n", len(ids), plural(len(ids), "vulnerability", "vulnerabilities"), inRun(runURL))

	// Room for the marker and the line that says the list was cut short.
	const cut = "- …\n"

	for _, r := range results {
		if len(r.Advisories) == 0 {
			continue
		}

		advisories := make([]string, 0, len(r.Advisories))

		for _, a := range r.Advisories {
			advisories = append(advisories, a.ID)
		}

		line := "- " + r.Dir + ": " + strings.Join(advisories, ", ") + "\n"

		// The marker follows a blank line.
		if b.Len()+len(line)+len(cut)+len("\n")+len(marker) > MaxBody {
			b.WriteString(cut)

			break
		}

		b.WriteString(line)
	}

	b.WriteString("\n" + marker)

	return b.String()
}

// markerLine returns the hidden line holding entries, as MarkerEntries returns them, which ParseMarker reads back.
func markerLine(entries []string) string {
	return markerPrefix + " " + strings.Join(entries, ",") + " -->\n"
}

// ParseMarker returns the sorted entries in the marker Markdown wrote into
// body, as MarkerEntries returns them, and false when body has no marker. The marker is the body's last
// line, so ParseMarker reads the last one, ignoring any older report or note
// pasted above it.
func ParseMarker(body string) ([]string, bool) {
	matches := marker.FindAllStringSubmatch(body, -1)

	if len(matches) == 0 {
		return nil, false
	}

	match := matches[len(matches)-1]

	var ids []string

	for id := range strings.SplitSeq(match[1], ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)

	return ids, true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}
