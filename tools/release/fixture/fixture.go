// Package fixture builds temporary git repositories laid out like this one,
// for the release tool's tests.
package fixture

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
)

// ModulePrefix prefixes every fixture module path.
const ModulePrefix = "github.com/release-fixture/pkg"

// GoVersion is the fixture's .go-version.
const GoVersion = "1.27"

// Repo is a temporary git repository with a .go-version, a go.work and the
// release tool's go.mod (which names the repository's module path), on
// branch main.
type Repo struct {
	// Root is the repository's directory, with symlinks resolved.
	Root string

	t         testing.TB
	dir       *os.Root
	gitconfig string
}

// New creates a Repo with one commit. It points git at a fresh global config
// for the rest of the test, so the user's settings (signing, hooks, default
// branch) can't leak in; tests using it can't run in parallel.
func New(t testing.TB) Repo {
	t.Helper()

	gitconfig := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(gitconfig, []byte(
		"[user]\n\tname = Fixture\n\temail = fixture@example.com\n"+
			"[commit]\n\tgpgSign = false\n[tag]\n\tgpgSign = false\n"), 0o600))

	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	dir, err := os.OpenRoot(root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dir.Close() })

	r := Repo{Root: root, t: t, dir: dir, gitconfig: gitconfig}
	r.Git("init", "--quiet", "--initial-branch=main")
	r.Write(".go-version", GoVersion+"\n")
	r.Write("go.work", "go "+GoVersion+"\n")
	r.Write("tools/release/go.mod", fmt.Sprintf("module %s/tools/release\n\ngo %s\n", ModulePrefix, GoVersion))
	r.Commit("Initial commit")

	return r
}

// Git runs git in the repository and returns its trimmed standard output.
func (r Repo) Git(args ...string) string {
	r.t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(r.t.Context(), "git", args...)
	cmd.Dir = r.Root
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(r.t, cmd.Run(), "git %s: %s", strings.Join(args, " "), stderr.String())

	return strings.TrimSpace(stdout.String())
}

// Write writes content to the slash-separated path, creating directories.
func (r Repo) Write(name, content string) {
	r.t.Helper()

	p := filepath.FromSlash(name)
	require.NoError(r.t, r.dir.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(r.t, r.dir.WriteFile(p, []byte(content), 0o600))
}

// Remove deletes the slash-separated path and anything under it.
func (r Repo) Remove(name string) {
	r.t.Helper()

	require.NoError(r.t, r.dir.RemoveAll(filepath.FromSlash(name)))
}

// AddModule creates a library module in dir with a go.mod for
// ModulePrefix/dir, a README.md, a Go file exporting Hello, and a VERSION
// holding version unless version is empty, and registers it in go.work.
func (r Repo) AddModule(dir, version string) {
	r.t.Helper()

	name := path.Base(dir)
	r.Write(dir+"/go.mod", fmt.Sprintf("module %s/%s\n\ngo %s\n", ModulePrefix, dir, GoVersion))
	r.Write(dir+"/README.md", "# "+name+"\n")
	r.Write(
		dir+"/"+name+".go",
		fmt.Sprintf("package %s\n\n// Hello says hello.\nfunc Hello() string { return \"hello\" }\n", name),
	)

	if version != "" {
		r.Write(dir+"/VERSION", version+"\n")
	}

	r.Use(dir)
}

// Use adds dir to go.work.
func (r Repo) Use(dir string) {
	r.t.Helper()

	data, err := r.dir.ReadFile("go.work")
	require.NoError(r.t, err)

	work, err := modfile.ParseWork("go.work", data, nil)
	require.NoError(r.t, err)
	require.NoError(r.t, work.AddUse("./"+dir, ""))

	r.Write("go.work", string(modfile.Format(work.Syntax)))
}

// Unuse removes dir from go.work.
func (r Repo) Unuse(dir string) {
	r.t.Helper()

	data, err := r.dir.ReadFile("go.work")
	require.NoError(r.t, err)

	work, err := modfile.ParseWork("go.work", data, nil)
	require.NoError(r.t, err)
	require.NoError(r.t, work.DropUse("./"+dir))

	work.Cleanup()
	r.Write("go.work", string(modfile.Format(work.Syntax)))
}

// Commit stages every change and commits it, and returns the commit's SHA.
func (r Repo) Commit(message string) string {
	r.t.Helper()

	r.Git("add", "--all")
	r.Git("commit", "--quiet", "--allow-empty", "--message", message)

	return r.Git("rev-parse", "HEAD")
}

// Tag tags HEAD.
func (r Repo) Tag(name string) {
	r.t.Helper()

	r.Git("tag", name)
}

// ServeModules lets the go command fetch fixture modules straight from this
// repository's tags, isolated from the user's go env, proxy and module
// cache, for the rest of the test.
func (r Repo) ServeModules() {
	r.t.Helper()

	f, err := os.OpenFile(r.gitconfig, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(r.t, err)

	_, err = fmt.Fprintf(f, "[url \"file://%s\"]\n\tinsteadOf = https://%s\n", r.Root, ModulePrefix)
	require.NoError(r.t, err)
	require.NoError(r.t, f.Close())

	r.t.Setenv("GOENV", "off")
	r.t.Setenv("GOPRIVATE", ModulePrefix)
	r.t.Setenv("GONOSUMDB", ModulePrefix)
	r.t.Setenv("GONOPROXY", ModulePrefix)
	r.t.Setenv("GOPROXY", "off")
	r.t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	r.t.Setenv("GOMODCACHE", r.t.TempDir())
	r.t.Setenv("GOFLAGS", "-modcacherw")
	r.t.Setenv("GOTOOLCHAIN", "local")
}
