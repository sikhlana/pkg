package publish_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/sikhlana/pkg/tools/release/fixture"
	"github.com/sikhlana/pkg/tools/release/git"
	"github.com/sikhlana/pkg/tools/release/publish"
	"github.com/sikhlana/pkg/tools/release/workspace"
)

var errGitHub = errors.New("github unavailable")

// proxy records the paths requested from a fake module proxy, failing them
// all when fail is set.
type proxy struct {
	mu    sync.Mutex
	paths []string
	fail  bool
}

func (p *proxy) serve(t *testing.T) publish.Proxy {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()

		p.paths = append(p.paths, r.URL.Path)

		if p.fail {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return publish.Proxy{URL: srv.URL, Client: srv.Client()}
}

// setup returns a repository with logger released at 0.3.0 and metrics at
// 0.1.0 on main.
func setup(t *testing.T) fixture.Repo {
	t.Helper()

	f := fixture.New(t)
	f.AddModule("logger", "0.3.0")
	f.AddModule("metrics", "0.1.0")
	f.Commit("Release logger and metrics")
	f.Tag("logger/v0.3.0")
	f.Tag("metrics/v0.1.0")

	return f
}

func run(
	t *testing.T,
	f fixture.Repo,
	sha string,
	releases publish.Releases,
	p publish.Proxy,
) ([]publish.Outcome, error) {
	t.Helper()

	repo, err := git.Open(t.Context(), f.Root)
	require.NoError(t, err)

	ws, err := workspace.Load(f.Root)
	require.NoError(t, err)

	return publish.Run(t.Context(), publish.Options{Repo: repo, Workspace: ws, SHA: sha, Releases: releases, Proxy: p})
}

func TestOneModuleBumped(t *testing.T) {
	f := setup(t)
	f.Write("logger/extra.go", "package logger\n")
	f.Write("logger/VERSION", "0.4.0\n")
	sha := f.Commit("Add extra to logger (#2)")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.4.0").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), "metrics/v0.1.0").Return(true, nil)
	releases.EXPECT().Create(gomock.Any(), publish.Release{
		Tag:    "logger/v0.4.0",
		Target: sha,
		Notes:  "- Add extra to logger (#2)\n",
	}).Return(nil)

	p := &proxy{}

	outcomes, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)

	assert.Equal(t, []publish.Outcome{
		{Dir: "logger", Tag: "logger/v0.4.0", Status: publish.StatusCreated},
		{Dir: "metrics", Tag: "metrics/v0.1.0", Status: publish.StatusReleased},
	}, outcomes)
	assert.Equal(t, []string{"/github.com/release-fixture/pkg/logger/@v/v0.4.0.info"}, p.paths)
}

func TestSeveralModulesBumped(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.3.1\n")
	f.Write("metrics/VERSION", "0.1.1\n")
	sha := f.Commit("Bump both")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(false, nil).Times(2)
	releases.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, r publish.Release) error {
			assert.Equal(t, sha, r.Target)
			return nil
		}).Times(2)

	p := &proxy{}

	outcomes, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)

	require.Len(t, outcomes, 2)
	assert.Equal(t, "logger/v0.3.1", outcomes[0].Tag)
	assert.Equal(t, "metrics/v0.1.1", outcomes[1].Tag)
	assert.Equal(t, publish.StatusCreated, outcomes[0].Status)
	assert.Equal(t, publish.StatusCreated, outcomes[1].Status)
	assert.Len(t, p.paths, 2)
}

func TestRerunChangesNothing(t *testing.T) {
	f := setup(t)
	sha := f.Git("rev-parse", "HEAD")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)

	p := &proxy{}

	outcomes, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)

	for _, o := range outcomes {
		assert.Equal(t, publish.StatusReleased, o.Status)
	}

	assert.Empty(t, p.paths)
}

func TestMissedReleaseIsRepaired(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.4.0\n")
	f.Commit("Bump logger (#2)")
	f.Write("docs/notes.md", "notes\n")
	later := f.Commit("Add notes (#3)")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.4.0").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), "metrics/v0.1.0").Return(true, nil)
	releases.EXPECT().Create(gomock.Any(), publish.Release{
		Tag:    "logger/v0.4.0",
		Target: later,
		Notes:  "- Bump logger (#2)\n",
	}).Return(nil)

	p := &proxy{}

	_, err := run(t, f, later, releases, p.serve(t))
	require.NoError(t, err)
}

func TestTagWithoutRelease(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.4.0\n")
	f.Commit("Bump logger (#2)")
	f.Tag("logger/v0.4.0")
	f.Write("docs/notes.md", "notes\n")
	later := f.Commit("Add notes (#3)")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.4.0").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), "metrics/v0.1.0").Return(true, nil)
	releases.EXPECT().Create(gomock.Any(), publish.Release{
		Tag:   "logger/v0.4.0",
		Notes: "- Bump logger (#2)\n",
	}).Return(nil)

	p := &proxy{}

	outcomes, err := run(t, f, later, releases, p.serve(t))
	require.NoError(t, err)
	assert.Equal(t, publish.StatusCreated, outcomes[0].Status)
}

func TestNotesExcludeOtherModules(t *testing.T) {
	f := setup(t)
	f.AddModule("logger/sub", "0.1.0")
	f.Commit("Add logger/sub")
	f.Tag("logger/sub/v0.1.0")
	f.Write("logger/a.go", "package logger\n")
	f.Commit("Change logger (#2)")
	f.Write("metrics/a.go", "package metrics\n")
	f.Commit("Change metrics (#3)")
	f.Write("logger/sub/a.go", "package sub\n")
	f.Commit("Change logger/sub (#4)")
	f.Write("logger/VERSION", "0.3.1\n")
	sha := f.Commit("Bump logger (#5)")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.3.1").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	releases.EXPECT().Create(gomock.Any(), publish.Release{
		Tag:    "logger/v0.3.1",
		Target: sha,
		Notes:  "- Bump logger (#5)\n- Change logger (#2)\n",
	}).Return(nil)

	p := &proxy{}

	_, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)
}

func TestFirstReleaseListsEveryCommit(t *testing.T) {
	f := fixture.New(t)
	f.AddModule("logger", "0.1.0")
	f.Commit("Add logger (#1)")
	f.Write("logger/a.go", "package logger\n")
	sha := f.Commit("Change logger (#2)")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.1.0").Return(false, nil)
	releases.EXPECT().Create(gomock.Any(), publish.Release{
		Tag:    "logger/v0.1.0",
		Target: sha,
		Notes:  "- Change logger (#2)\n- Add logger (#1)\n",
	}).Return(nil)

	p := &proxy{}

	_, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)
}

func TestNoModulesCallsNoAPI(t *testing.T) {
	f := fixture.New(t)
	sha := f.Git("rev-parse", "HEAD")

	p := &proxy{}

	outcomes, err := run(t, f, sha, NewMockReleases(gomock.NewController(t)), p.serve(t))
	require.NoError(t, err)
	assert.Empty(t, outcomes)
	assert.Empty(t, p.paths)
	assert.Contains(t, publish.Markdown(outcomes), "No library modules.")
}

func TestOneFailureDoesntStopTheOthers(t *testing.T) {
	f := setup(t)
	f.AddModule("cache", "v0.1.0")
	f.Write("logger/VERSION", "0.3.1\n")
	f.Write("metrics/VERSION", "0.1.1\n")
	sha := f.Commit("Bump")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(false, nil).Times(2)
	releases.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, r publish.Release) error {
			if r.Tag == "logger/v0.3.1" {
				return errGitHub
			}

			return nil
		}).Times(2)

	p := &proxy{}

	outcomes, err := run(t, f, sha, releases, p.serve(t))
	require.ErrorIs(t, err, errGitHub)
	require.ErrorContains(t, err, "publishing cache: reading cache/VERSION")

	require.Len(t, outcomes, 3)
	assert.Equal(t, publish.StatusFailed, outcomes[0].Status)
	assert.Equal(t, publish.StatusFailed, outcomes[1].Status)
	assert.Equal(t, publish.StatusCreated, outcomes[2].Status)

	summary := publish.Markdown(outcomes)
	assert.Contains(t, summary, "| logger | logger/v0.3.1 | failed: github unavailable |")
	assert.Contains(t, summary, "| metrics | metrics/v0.1.1 | created |")
}

func TestProxyFailureIsOnlyAWarning(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.3.1\n")
	sha := f.Commit("Bump logger")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.3.1").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), "metrics/v0.1.0").Return(true, nil)
	releases.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

	p := &proxy{fail: true}

	outcomes, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)

	assert.Equal(t, publish.StatusCreated, outcomes[0].Status)
	require.ErrorIs(t, outcomes[0].ProxyErr, publish.ErrProxyStatus)
	assert.Contains(t, publish.Markdown(outcomes), "created; module proxy not updated")
}

func TestMajorMismatchIsRefused(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "2.0.0\n")
	sha := f.Commit("Bump logger to 2.0.0 in place")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "metrics/v0.1.0").Return(true, nil)

	p := &proxy{}

	outcomes, err := run(t, f, sha, releases, p.serve(t))
	require.ErrorIs(t, err, publish.ErrPathMajor)

	assert.Equal(t, publish.StatusFailed, outcomes[0].Status)
	assert.Empty(t, outcomes[0].Tag)
	assert.Empty(t, p.paths)
}

func TestMajorVersionDirectoryRelease(t *testing.T) {
	f := setup(t)
	f.AddModule("logger/v2", "2.0.0")
	f.Write("logger/a.go", "package logger\n")
	f.Write("logger/VERSION", "0.3.1\n")
	sha := f.Commit("Add logger/v2 (#2)")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.3.1").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), "logger/v2.0.0").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), "metrics/v0.1.0").Return(true, nil)
	releases.EXPECT().Create(gomock.Any(), publish.Release{
		Tag:    "logger/v0.3.1",
		Target: sha,
		Notes:  "- Add logger/v2 (#2)\n",
	}).Return(nil)
	releases.EXPECT().Create(gomock.Any(), publish.Release{
		Tag:    "logger/v2.0.0",
		Target: sha,
		Notes:  "- Add logger/v2 (#2)\n",
	}).Return(nil)

	p := &proxy{}

	outcomes, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)

	assert.Equal(t, "logger/v2.0.0", outcomes[1].Tag)
	assert.Equal(t, []string{
		"/github.com/release-fixture/pkg/logger/@v/v0.3.1.info",
		"/github.com/release-fixture/pkg/logger/v2/@v/v2.0.0.info",
	}, p.paths)
}

func TestLaterMajorDoesntBecomeThePreviousRelease(t *testing.T) {
	f := setup(t)
	f.AddModule("logger/v2", "2.0.0")
	f.Commit("Add logger/v2 (#2)")
	f.Tag("logger/v2.0.0")
	f.Write("logger/a.go", "package logger\n")
	f.Write("logger/VERSION", "0.3.1\n")
	sha := f.Commit("Change logger (#3)")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.3.1").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)
	releases.EXPECT().Create(gomock.Any(), publish.Release{
		Tag:    "logger/v0.3.1",
		Target: sha,
		Notes:  "- Change logger (#3)\n",
	}).Return(nil)

	p := &proxy{}

	_, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)
}

func TestSkippedVersionIsReported(t *testing.T) {
	f := setup(t)
	f.Write("logger/VERSION", "0.4.0\n")
	f.Commit("Bump logger to 0.4.0 (#2)")
	f.Write("logger/VERSION", "0.5.0\n")
	sha := f.Commit("Bump logger to 0.5.0 (#3)")

	releases := NewMockReleases(gomock.NewController(t))
	releases.EXPECT().Exists(gomock.Any(), "logger/v0.5.0").Return(false, nil)
	releases.EXPECT().Exists(gomock.Any(), "metrics/v0.1.0").Return(true, nil)
	releases.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

	p := &proxy{}

	outcomes, err := run(t, f, sha, releases, p.serve(t))
	require.NoError(t, err)

	assert.Equal(t, []string{"v0.4.0"}, outcomes[0].Skipped)
	assert.Equal(t,
		"logger: VERSION held v0.4.0 after its previous release, but they were never released; "+
			"logger/v0.5.0 includes their changes",
		outcomes[0].SkippedWarning())
	assert.Contains(t, publish.Markdown(outcomes), "| logger | logger/v0.5.0 | created; never released: v0.4.0 |")
	assert.Empty(t, outcomes[1].SkippedWarning())
}
