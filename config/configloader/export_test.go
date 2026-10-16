package configloader

import "path/filepath"

// SearchPatterns exposes the search's candidates to the external test package, as the patterns it logs, so their
// order is tested without files in /etc.
func SearchPatterns(name string, searchPaths []string) []string {
	l := &loader{name: name}
	candidates := l.candidates(searchPaths)
	patterns := make([]string, 0, len(candidates))

	for _, c := range candidates {
		patterns = append(patterns, filepath.Join(c.dir, c.base+".*"))
	}

	return patterns
}
