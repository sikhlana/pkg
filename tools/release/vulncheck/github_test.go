package vulncheck_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/tools/release/repository"
	"github.com/sikhlana/pkg/tools/release/vulncheck"
)

var _ vulncheck.Tracker = vulncheck.GitHub{}

func newGitHub(t *testing.T, handler http.Handler) vulncheck.GitHub {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := github.NewClient(
		github.WithHTTPClient(srv.Client()),
		github.WithURLs(new(srv.URL+"/"), new(srv.URL+"/")),
	)
	require.NoError(t, err)

	g, err := vulncheck.NewGitHub(client, "sikhlana/pkg")
	require.NoError(t, err)

	return g
}

// decode records each request body handled by the returned handler, which
// answers status with response.
func decode(t *testing.T, bodies *[]map[string]any, status int, response string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		*bodies = append(*bodies, body)

		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}
}

func TestNewGitHubRejectsInvalidRepository(t *testing.T) {
	// repository.Parse's own tests cover every invalid form.
	_, err := vulncheck.NewGitHub(nil, "sikhlana")
	require.ErrorIs(t, err, repository.ErrInvalid)
}

func TestGitHubFindReadsLaterPages(t *testing.T) {
	var pages []string

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sikhlana/pkg/issues", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)

		if page == "" {
			// A full first page of labelled pull requests, then a link to the next.
			w.Header().Set("Link", `<`+"http://"+r.Host+r.URL.Path+`?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"number": 3, "pull_request": {"url": "x"}}]`))

			return
		}

		_, _ = w.Write([]byte(`[{"number": 5, "body": "the issue"}]`))
	})

	issue, ok, err := newGitHub(t, mux).Find(t.Context(), vulncheck.Label)
	require.NoError(t, err)

	assert.True(t, ok)
	assert.Equal(t, vulncheck.Issue{Number: 5, Body: "the issue"}, issue)
	assert.Equal(t, []string{"", "2"}, pages)
}

func TestGitHubFindSkipsPullRequests(t *testing.T) {
	var query string

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sikhlana/pkg/issues", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`[
			{"number": 3, "body": "a pull request", "pull_request": {"url": "x"}},
			{"number": 5, "body": "the issue"},
			{"number": 9, "body": "a newer issue"}
		]`))
	})

	issue, ok, err := newGitHub(t, mux).Find(t.Context(), "govulncheck")
	require.NoError(t, err)

	assert.True(t, ok)
	assert.Equal(t, vulncheck.Issue{Number: 5, Body: "the issue"}, issue)
	assert.Equal(t, "direction=asc&labels=govulncheck&per_page=100&sort=created&state=open", query)
}

func TestGitHubFindNone(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sikhlana/pkg/issues", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	_, ok, err := newGitHub(t, mux).Find(t.Context(), "govulncheck")
	require.NoError(t, err)

	assert.False(t, ok)
}

func TestGitHubFindError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sikhlana/pkg/issues", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
	})

	_, _, err := newGitHub(t, mux).Find(t.Context(), "govulncheck")
	require.ErrorContains(t, err, "listing issues labelled govulncheck")
}

func TestGitHubEnsureLabel(t *testing.T) {
	var created []map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/sikhlana/pkg/labels/govulncheck", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"govulncheck"}`))
	})
	mux.HandleFunc("GET /repos/sikhlana/pkg/labels/missing", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("GET /repos/sikhlana/pkg/labels/broken", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
	})
	mux.HandleFunc("POST /repos/sikhlana/pkg/labels", decode(t, &created, http.StatusCreated, `{}`))

	g := newGitHub(t, mux)

	require.NoError(t, g.EnsureLabel(t.Context(), "govulncheck"))
	assert.Empty(t, created)

	require.NoError(t, g.EnsureLabel(t.Context(), "missing"))
	assert.Equal(t, []map[string]any{{
		"name":        "missing",
		"color":       "b60205",
		"description": "Reachable vulnerabilities found by the weekly govulncheck scan",
	}}, created)

	require.ErrorContains(t, g.EnsureLabel(t.Context(), "broken"), "looking up label broken")
}

func TestGitHubCreate(t *testing.T) {
	var bodies []map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/sikhlana/pkg/issues", decode(t, &bodies, http.StatusCreated, `{"number": 12}`))

	number, err := newGitHub(t, mux).Create(t.Context(), "Title", "Body", "govulncheck")
	require.NoError(t, err)

	assert.Equal(t, 12, number)
	assert.Equal(t, []map[string]any{{"title": "Title", "body": "Body", "labels": []any{"govulncheck"}}}, bodies)
}

func TestGitHubEditCommentAndClose(t *testing.T) {
	var updates, comments []map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /repos/sikhlana/pkg/issues/12", decode(t, &updates, http.StatusOK, `{}`))
	mux.HandleFunc("POST /repos/sikhlana/pkg/issues/12/comments", decode(t, &comments, http.StatusCreated, `{}`))

	g := newGitHub(t, mux)

	require.NoError(t, g.Edit(t.Context(), 12, "New body"))
	require.NoError(t, g.Comment(t.Context(), 12, "A comment"))
	require.NoError(t, g.Close(t.Context(), 12))

	assert.Equal(t, []map[string]any{
		{"body": "New body"},
		{"state": "closed", "state_reason": "completed"},
	}, updates)
	assert.Equal(t, []map[string]any{{"body": "A comment"}}, comments)
}

func TestGitHubErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Validation Failed"}`, http.StatusUnprocessableEntity)
	})

	g := newGitHub(t, mux)

	_, err := g.Create(t.Context(), "Title", "Body", "govulncheck")
	require.ErrorContains(t, err, "creating issue")
	require.ErrorContains(t, g.Edit(t.Context(), 12, "Body"), "editing issue #12")
	require.ErrorContains(t, g.Comment(t.Context(), 12, "Body"), "commenting on issue #12")
	require.ErrorContains(t, g.Close(t.Context(), 12), "closing issue #12")
}
