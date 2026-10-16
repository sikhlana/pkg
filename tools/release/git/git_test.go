package git_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/git"
)

func open(t *testing.T, f fixture.Repo) git.Repo {
	t.Helper()

	repo, err := git.Open(t.Context(), f.Root)
	require.NoError(t, err)

	return repo
}

func TestOpenFindsRootFromSubdirectory(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Commit("Add logger")

	repo, err := git.Open(t.Context(), filepath.Join(f.Root, "logger"))
	require.NoError(t, err)
	assert.Equal(t, f.Root, repo.Root)
}

func TestOpenOutsideRepository(t *testing.T) {
	_, err := git.Open(t.Context(), t.TempDir())
	require.Error(t, err)
}

func TestCommit(t *testing.T) {
	f := fixture.New(t)
	sha := f.Commit("Second")
	f.Tag("logger/v0.1.0")

	repo := open(t, f)

	got, err := repo.Commit(t.Context(), "HEAD")
	require.NoError(t, err)
	assert.Equal(t, sha, got)

	got, err = repo.Commit(t.Context(), "logger/v0.1.0")
	require.NoError(t, err)
	assert.Equal(t, sha, got)

	_, err = repo.Commit(t.Context(), "missing")
	require.Error(t, err)
}

func TestMergeBaseAndChangedFiles(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.AddModule("metrics", "0.1.0")
	base := f.Commit("Add modules")

	f.Git("switch", "--quiet", "--create", "feature")
	f.Write("logger/extra.go", "package logger\n")
	f.Git("mv", "metrics/metrics.go", "logger/moved.go")
	f.Commit("Change logger")

	f.Git("switch", "--quiet", "main")
	f.Write("docs/notes.md", "notes\n")
	f.Commit("Unrelated change on main")
	f.Git("switch", "--quiet", "feature")

	repo := open(t, f)

	mergeBase, err := repo.MergeBase(t.Context(), "main", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, base, mergeBase)

	files, err := repo.ChangedFiles(t.Context(), mergeBase, "HEAD")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"logger/extra.go", "logger/moved.go", "metrics/metrics.go"}, files,
		"a rename lists both paths")
}

func TestFileAt(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.3.0")
	f.Commit("Add logger")
	f.Write("logger/VERSION", "0.3.1\n")
	f.Commit("Bump logger")

	repo := open(t, f)

	content, found, err := repo.FileAt(t.Context(), "HEAD~1", "logger/VERSION")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "0.3.0\n", string(content))

	_, found, err = repo.FileAt(t.Context(), "HEAD~2", "logger/VERSION")
	require.NoError(t, err)
	assert.False(t, found)

	_, _, err = repo.FileAt(t.Context(), "missing", "logger/VERSION")
	require.Error(t, err)
}

func TestVersions(t *testing.T) {
	f := fixture.New(t)

	for _, tag := range []string{
		"logger/v0.2.0", "logger/v0.10.0", "logger/v0.1.0", "logger/v2.0.0",
		"logger/v1.0.0-rc.1", "logger/sub/v0.5.0", "logger/latest", "metrics/v9.0.0", "v1.0.0",
	} {
		f.Tag(tag)
	}

	repo := open(t, f)

	versions, err := repo.Versions(t.Context(), "logger")
	require.NoError(t, err)
	assert.Equal(t, []string{"v0.1.0", "v0.2.0", "v0.10.0", "v2.0.0"}, versions,
		"every major under the prefix; logger/v2's v2.0.0 included")

	versions, err = repo.Versions(t.Context(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"v1.0.0"}, versions)

	versions, err = repo.Versions(t.Context(), "cache")
	require.NoError(t, err)
	assert.Empty(t, versions)
}

func TestFiles(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Commit("Add logger")
	f.Remove("logger/README.md")
	f.Commit("Drop README")

	repo := open(t, f)

	files, err := repo.Files(t.Context(), "HEAD~1")
	require.NoError(t, err)
	assert.Contains(t, files, "logger/README.md")
	assert.Contains(t, files, "go.work")

	files, err = repo.Files(t.Context(), "HEAD")
	require.NoError(t, err)
	assert.NotContains(t, files, "logger/README.md")
	assert.Contains(t, files, "logger/go.mod")
}

func TestClean(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Commit("Add logger")

	repo := open(t, f)

	clean, err := repo.Clean(t.Context())
	require.NoError(t, err)
	assert.True(t, clean)

	f.Write("scratch.txt", "untracked\n")

	clean, err = repo.Clean(t.Context())
	require.NoError(t, err)
	assert.True(t, clean, "untracked files don't count")

	f.Write("logger/VERSION", "0.2.0\n")

	clean, err = repo.Clean(t.Context())
	require.NoError(t, err)
	assert.False(t, clean)
}

func TestCommits(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	first := f.Commit("Add logger")
	f.Tag("logger/v0.1.0")
	f.Write("logger/VERSION", "0.2.0\n")
	second := f.Commit("Bump to 0.2.0")
	f.Write("logger/a.go", "package logger\n")
	f.Commit("Not a bump")
	f.Write("logger/VERSION", "0.3.0\n")
	third := f.Commit("Bump to 0.3.0")

	repo := open(t, f)

	commits, err := repo.Commits(t.Context(), "logger/v0.1.0", "HEAD", "logger/VERSION")
	require.NoError(t, err)
	assert.Equal(t, []string{third, second}, commits)

	commits, err = repo.Commits(t.Context(), "", "HEAD", "logger/VERSION")
	require.NoError(t, err)
	assert.Equal(t, []string{third, second, first}, commits)
}

func TestTagExists(t *testing.T) {
	f := fixture.New(t)
	f.Tag("logger/v0.1.0")

	repo := open(t, f)

	exists, err := repo.TagExists(t.Context(), "logger/v0.1.0")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = repo.TagExists(t.Context(), "logger/v0.1.1")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSubjects(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("http", "0.1.0")
	f.AddModule("http/server", "0.1.0")
	f.Commit("Add http")
	f.Tag("http/v0.1.0")

	f.Write("http/client.go", "package http\n")
	f.Commit("Add client")
	f.Write("http/server/handler.go", "package server\n")
	f.Commit("Add handler")
	f.Write("docs/http.md", "http\n")
	f.Commit("Document http")
	f.Write("http/README.md", "# http, again\n")
	f.Commit("Reword README")

	repo := open(t, f)

	subjects, err := repo.Subjects(t.Context(), "http/v0.1.0", "HEAD", "http", ":(exclude)http/server")
	require.NoError(t, err)
	assert.Equal(t, []string{"Reword README", "Add client"}, subjects)

	subjects, err = repo.Subjects(t.Context(), "", "HEAD", "http/server")
	require.NoError(t, err)
	assert.Equal(t, []string{"Add handler", "Add http"}, subjects)
}
