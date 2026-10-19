package workspace

import (
	"path"
	"strings"

	"golang.org/x/mod/module"
)

// Files every library module carries.
const (
	GoModFile   = "go.mod"
	VersionFile = "VERSION"
	ReadmeFile  = "README.md"
)

// Module is a library module: a go.work entry outside ToolsDir with a
// go.mod.
type Module struct {
	// Dir is the module's slash-separated directory relative to the
	// repository root, such as http/server.
	Dir string
	// Path is the module path declared in go.mod.
	Path string
	// GoVersion is go.mod's go directive.
	GoVersion string
	// Version is the VERSION file's contents, unvalidated.
	Version string
	// HasVersion reports whether the module has a VERSION file.
	HasVersion bool
	// HasReadme reports whether the module has a README.md.
	HasReadme bool
}

// Tag returns the module's tag for v, such as http/server/v0.2.0. v may
// carry a v prefix or not. A module in a major version directory drops it,
// as the go command expects: logger/v2 at 2.1.0 is tagged logger/v2.1.0.
func (m Module) Tag(v string) string {
	v = "v" + strings.TrimPrefix(v, "v")

	if prefix := m.TagPrefix(); prefix != "" {
		return prefix + "/" + v
	}

	return v
}

// TagPrefix returns the directory the module's tags start with: Dir, minus
// a trailing major version directory matching the module path's /vN.
func (m Module) TagPrefix() string {
	major := m.PathMajor()

	switch {
	case major == "":
		return m.Dir
	case m.Dir == strings.TrimPrefix(major, "/"):
		return ""
	}

	if prefix, ok := strings.CutSuffix(m.Dir, major); ok {
		return prefix
	}

	return m.Dir
}

// PathMajor returns the module path's major version suffix, such as /v2,
// or "" for a v0 or v1 module path or an invalid one.
func (m Module) PathMajor() string {
	_, major, ok := module.SplitPathVersion(m.Path)

	if !ok {
		return ""
	}

	return major
}

// Owns reports whether v, such as v2.1.0, is a version of this module: its
// major must match the module path, so logger owns v0 and v1 and logger/v2
// owns v2, though both share the tag prefix logger.
func (m Module) Owns(v string) bool {
	return module.CheckPathMajor(v, m.PathMajor()) == nil
}

// Releases returns the versions the module owns, keeping their order.
func (m Module) Releases(versions []string) []string {
	var owned []string

	for _, v := range versions {
		if m.Owns(v) {
			owned = append(owned, v)
		}
	}

	return owned
}

// FirstVersion returns the version a new module starts at: v0.1.0, or
// vN.0.0 for a module path ending in /vN.
func (m Module) FirstVersion() string {
	if major := module.PathMajorPrefix(m.PathMajor()); major != "" {
		return major + ".0.0"
	}

	return "v0.1.0"
}

// File returns the slash-separated path, relative to the repository root, of
// the named file in the module.
func (m Module) File(name string) string {
	return path.Join(m.Dir, name)
}
