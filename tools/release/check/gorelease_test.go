package check_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/check"
	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/gorelease"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

// released returns a repository whose go command fetches fixture modules
// locally, with logger released at v on main, checked out on a feature
// branch.
func released(t *testing.T, v string) fixture.Repo {
	t.Helper()

	f := fixture.New(t)
	f.ServeModules()
	f.AddModule("logger", v)
	f.Commit("Release logger")
	f.Tag("logger/v" + v)
	f.Git("switch", "--quiet", "--create", "feature")

	return f
}

// runAPI commits the working tree and checks the branch against main with
// the API comparison.
func runAPI(t *testing.T, f fixture.Repo, runner gorelease.Runner) check.Report {
	t.Helper()

	f.Commit("Change")

	repo, err := git.Open(t.Context(), f.Root)
	require.NoError(t, err)

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	r, err := check.Run(t.Context(), check.Options{
		Repo:      repo,
		Workspace: ws,
		Base:      "main",
		Gorelease: func(context.Context) (gorelease.Runner, error) { return runner, nil },
		Jobs:      4,
	})
	require.NoError(t, err)

	return r
}

func loggerSource(exports ...string) string {
	var b strings.Builder

	b.WriteString("package logger\n")

	for _, name := range exports {
		fmt.Fprintf(&b, "\n// %s says hello.\nfunc %s() string { return \"hello\" }\n", name, name)
	}

	return b.String()
}

func TestAPIMinimum(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs gorelease")
	}

	toolDir, err := filepath.Abs("..")
	require.NoError(t, err)

	runner, err := gorelease.Build(t.Context(), toolDir, t.TempDir())
	require.NoError(t, err)

	t.Run("no API change", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Write("logger/README.md", "# logger, reworded\n")
		f.Write("logger/VERSION", "0.3.1\n")

		r := runAPI(t, f, runner)

		assert.Empty(t, r.Problems)
		require.Len(t, r.Modules, 1)
		assert.Equal(t, "v0.3.0", r.Modules[0].APIBase)
		assert.Contains(t, r.Modules[0].APIReport, "Suggested version: v0.3.1")
	})

	t.Run("new export with a patch bump", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Write("logger/logger.go", loggerSource("Hello", "Goodbye"))
		f.Write("logger/VERSION", "0.3.1\n")

		r := runAPI(t, f, runner)

		assert.Equal(
			t,
			[]string{"logger's API changes since v0.3.0 need at least 0.4.0, but VERSION is 0.3.1"},
			messages(r),
		)
		assert.Equal(t, "logger/VERSION", r.Problems[0].File)
	})

	t.Run("breaking change before 1.0", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Write("logger/logger.go", loggerSource())
		f.Write("logger/VERSION", "0.4.0\n")

		r := runAPI(t, f, runner)

		assert.Empty(t, r.Problems)
		assert.Contains(t, r.Markdown(), "## incompatible changes\nHello: removed")
	})

	t.Run("breaking change after 1.0", func(t *testing.T) {
		for _, v := range []string{"1.2.1", "1.3.0"} {
			t.Run(v, func(t *testing.T) {
				f := released(t, "1.2.0")
				f.Write("logger/logger.go", loggerSource())
				f.Write("logger/VERSION", v+"\n")

				r := runAPI(t, f, runner)

				assert.Equal(t, []string{
					"logger has incompatible API changes since v1.2.0, which need a new major module path (logger/v2)",
				}, messages(r))
				assert.Equal(t, "logger/go.mod", r.Problems[0].File)
			})
		}
	})

	t.Run("breaking change after 1.0, VERSION 2.0.0 in place", func(t *testing.T) {
		f := released(t, "1.2.0")
		f.Write("logger/logger.go", loggerSource())
		f.Write("logger/VERSION", "2.0.0\n")

		r := runAPI(t, f, runner)

		require.Len(t, r.Problems, 1)
		assert.Contains(t, r.Problems[0].Message, "v2 lives in logger/v2")
		assert.Empty(t, r.Modules[0].APIReport, "the major check fails before gorelease runs")
	})

	t.Run("new major version directory", func(t *testing.T) {
		f := released(t, "1.2.0")
		f.AddModule("logger/v2", "2.0.0")

		r := runAPI(t, f, runner)

		assert.Empty(t, r.Problems)
		require.Len(t, r.Modules, 1)
		assert.Equal(t, gorelease.BaseNone, r.Modules[0].APIBase)
		assert.Contains(t, r.Modules[0].APIReport, "Suggested version: v2.0.0 (with tag logger/v2.0.0)")
	})

	t.Run("unpublished sibling requirement", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Git("switch", "--quiet", "main")
		f.AddModule("kernel", "0.1.0")
		f.Commit("Release kernel")
		f.Tag("kernel/v0.1.0")
		f.Git("switch", "--quiet", "--force-create", "feature")
		f.Write("logger/go.mod", fmt.Sprintf(
			"module %s/logger\n\ngo 1.27\n\nrequire %s/kernel v0.2.0\n", fixture.ModulePrefix, fixture.ModulePrefix))
		f.Write("logger/logger.go", fmt.Sprintf(
			"package logger\n\nimport %q\n\n// Hello says hello.\nfunc Hello() string { return kernel.Hello() }\n",
			fixture.ModulePrefix+"/kernel"))
		f.Write("logger/VERSION", "0.3.1\n")

		r := runAPI(t, f, runner)

		require.Len(t, r.Problems, 1)
		assert.Equal(t, "logger/go.mod", r.Problems[0].File)
		assert.Equal(t, "the API comparison against v0.3.0 failed", r.Problems[0].Message)
		assert.Contains(t, r.Problems[0].Detail, "kernel@v0.2.0")
	})

	t.Run("the comparison itself fails", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.Write("logger/broken.go", "package logger\n\nfunc broken() { undefined() }\n")
		f.Write("logger/VERSION", "0.3.1\n")

		r := runAPI(t, f, runner)

		require.Len(t, r.Problems, 1)
		assert.Equal(t, "the API comparison against v0.3.0 failed", r.Problems[0].Message)
		assert.Contains(t, r.Problems[0].Detail, "undefined: undefined")
		assert.Contains(t, r.Markdown(), "undefined: undefined")
	})

	t.Run("module with no release", func(t *testing.T) {
		f := released(t, "0.3.0")
		f.AddModule("metrics", "0.1.0")

		r := runAPI(t, f, runner)

		assert.Empty(t, r.Problems)
		require.Len(t, r.Modules, 1)
		assert.Equal(t, gorelease.BaseNone, r.Modules[0].APIBase)
	})
}
