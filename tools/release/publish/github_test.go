package publish_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/publish"
)

var _ publish.Releases = publish.GitHub{}

func newGitHub(t *testing.T, handler http.Handler) publish.GitHub {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := github.NewClient(
		github.WithHTTPClient(srv.Client()),
		github.WithURLs(new(srv.URL+"/"), new(srv.URL+"/")),
	)
	require.NoError(t, err)

	g, err := publish.NewGitHub(client, "sikhlana/pkg")
	require.NoError(t, err)

	return g
}

func TestNewGitHubRejectsInvalidRepository(t *testing.T) {
	for _, repository := range []string{"", "sikhlana", "/pkg", "sikhlana/", "a/b/c"} {
		_, err := publish.NewGitHub(nil, repository)
		require.ErrorIs(t, err, publish.ErrRepository, repository)
	}
}

func TestGitHubExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sikhlana/pkg/releases/tags/logger/v0.3.0", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"logger/v0.3.0"}`))
	})
	mux.HandleFunc("GET /repos/sikhlana/pkg/releases/tags/logger/v0.4.0", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("GET /repos/sikhlana/pkg/releases/tags/logger/v0.5.0", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
	})

	g := newGitHub(t, mux)

	exists, err := g.Exists(t.Context(), "logger/v0.3.0")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = g.Exists(t.Context(), "logger/v0.4.0")
	require.NoError(t, err)
	assert.False(t, exists)

	_, err = g.Exists(t.Context(), "logger/v0.5.0")
	require.ErrorContains(t, err, "looking up release logger/v0.5.0")
}

func TestGitHubCreate(t *testing.T) {
	var bodies []map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/sikhlana/pkg/releases", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		bodies = append(bodies, body)

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})

	g := newGitHub(t, mux)

	require.NoError(
		t,
		g.Create(t.Context(), publish.Release{Tag: "logger/v0.4.0", Target: "abc123", Notes: "- Change\n"}),
	)
	require.NoError(t, g.Create(t.Context(), publish.Release{Tag: "logger/v0.3.0", Notes: "- Old\n"}))

	assert.Equal(t, []map[string]any{
		{
			"tag_name":         "logger/v0.4.0",
			"target_commitish": "abc123",
			"name":             "logger/v0.4.0",
			"body":             "- Change\n",
			"make_latest":      "false",
		},
		{
			"tag_name":    "logger/v0.3.0",
			"name":        "logger/v0.3.0",
			"body":        "- Old\n",
			"make_latest": "false",
		},
	}, bodies)
}

func TestGitHubCreateError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/sikhlana/pkg/releases", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Validation Failed"}`, http.StatusUnprocessableEntity)
	})

	err := newGitHub(t, mux).Create(t.Context(), publish.Release{Tag: "logger/v0.4.0"})
	require.ErrorContains(t, err, "creating release logger/v0.4.0")
}
