package urlmask_test

import (
	"bytes"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/url/urlmask"
)

var queryScenarios = []struct {
	name  string
	query string
	want  string
}{
	{"keys keep their order", "token=abc&email=a@b.c", "token=***&email=***"},
	{"a repeated key keeps its count", "a=1&b=2&a=3", "a=***&b=***&a=***"},
	{"keys stay escaped as written", "a%20b=1&c+d=2", "a%20b=***&c+d=***"},
	{"a value with an inner equals sign", "sig=a=b", "sig=***"},
	{"a value that isn't validly escaped", "token=%zz&lang=en", "token=***&lang=***"},
	{"a bare token between keyed entries", "lang=en&eyJhbGciOiJIUzI1NiJ9.payload.sig&page=2", "lang=***&***&page=***"},
	{"a padded base64 token", "cmVzZXQ6dXNlcjQyOnMzY3IzdC1ub25jZSE=", "***"},
	{"an empty value", "token=", "***"},
	{"a value of equals signs", "token===", "***"},
	{"empty entries", "a=1&&b=2", "a=***&&b=***"},
	{"a token before a semicolon", "s3cr3t;a=1", "***;a=***"},
	{"mixed separators", "a=1;b=2&c=3", "a=***;b=***&c=***"},
	{"a bare query", "a=1;tok&b=2", "a=***;***&b=***"},
	{"a secret used as a query key", "sk_live_abc=1", "sk_live_abc=***"},
}

var urlScenarios = []struct {
	name string
	in   string
	want string
}{
	{
		"a session ID in a path parameter",
		"https://example.com/app/cart;jsessionid=8F3A1C/items",
		"https://example.com/app/cart;jsessionid=***/items",
	},
	{"several parameters, one bare", "https://example.com/a;x=1;tok/b", "https://example.com/a;x=***;***/b"},
	{"escaped characters in the path", "https://example.com/a%20b/p%2Fq;k=v", "https://example.com/a%20b/p%2Fq;k=***"},
	{"a username and password", "https://alice:hunter2@example.com/x", "https://alice:xxxxx@example.com/x"},
	{"a username alone", "https://alice@example.com/x", "https://alice@example.com/x"},
	{
		"an access token in the fragment",
		"https://example.com/cb?state=1#access_token=SECRET",
		"https://example.com/cb?state=***",
	},
	{"a URL with nothing to mask", "https://example.com:8443/a/b", "https://example.com:8443/a/b"},
	{
		"everything at once",
		"https://alice:pw@example.com:8443/a;sid=1/b?x=1&tok#frag",
		"https://alice:xxxxx@example.com:8443/a;sid=***/b?x=***&***",
	},
	{"an opaque URL", "mailto:alice@example.com", "mailto:***"},
	{"credentials in an opaque URL", "https:alice:hunter2@example.com/x", "https:***"},
	{"an unparseable URL string", "https://alice:hunter2@app example.com/?token=abc", "***"},
	{"an empty string", "", ""},
	{"a token in a path segment", "https://example.com/reset/8F3A1C", "https://example.com/reset/8F3A1C"},
	{"a relative reference", "/cb;sid=1?code=abc#state", "/cb;sid=***?code=***"},
	{"the scheme comes out lowercase", "HTTPS://Example.com/x?t=1", "https://Example.com/x?t=***"},
	{
		"an escaped semicolon in a path segment",
		"https://example.com/a%3Bjsessionid=8F3A1C",
		"https://example.com/a%3Bjsessionid=8F3A1C",
	},
}

type scenario struct {
	name string
	in   string
	want string
}

func scenarios() []scenario {
	all := make([]scenario, 0, len(queryScenarios)+len(urlScenarios))

	for _, s := range queryScenarios {
		all = append(all, scenario{
			name: "query: " + s.name,
			in:   "https://example.com/p?" + s.query,
			want: "https://example.com/p?" + s.want,
		})
	}

	for _, s := range urlScenarios {
		all = append(all, scenario(s))
	}

	return all
}

func TestMasking(t *testing.T) {
	for _, s := range scenarios() {
		t.Run(s.name, func(t *testing.T) {
			assert.Equal(t, s.want, urlmask.Raw(s.in), "Raw")

			u, err := url.Parse(s.in)
			if err != nil {
				return
			}

			assert.Equal(t, s.want, urlmask.URL(u), "URL")
		})
	}
}

func TestQuery(t *testing.T) {
	for _, s := range queryScenarios {
		t.Run(s.name, func(t *testing.T) {
			assert.Equal(t, s.want, urlmask.Query(s.query))
		})
	}
}

func TestURLNil(t *testing.T) {
	assert.Empty(t, urlmask.URL(nil))
}

func TestURLLeavesCallerUnchanged(t *testing.T) {
	u, err := url.Parse("https://alice:hunter2@example.com/a%2Fb;sid=1/c?token=abc#access_token=SECRET")
	require.NoError(t, err)

	before := *u
	user := *u.User

	urlmask.URL(u)

	assert.Equal(t, before, *u)
	assert.Equal(t, user, *u.User)
}

func TestValue(t *testing.T) {
	u, err := url.Parse("https://alice:pw@example.com/?t=1")
	require.NoError(t, err)

	tests := []struct {
		name string
		u    *url.URL
		want string
	}{
		{
			"a URL with a password and a query",
			u,
			`level=INFO msg=request url="https://alice:xxxxx@example.com/?t=***"` + "\n",
		},
		{"a nil URL", nil, `level=INFO msg=request url=""` + "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					if len(groups) == 0 && a.Key == slog.TimeKey {
						return slog.Attr{}
					}

					return a
				},
			}))

			logger.Info("request", slog.Any("url", urlmask.Value(tt.u)))

			assert.Equal(t, tt.want, buf.String())
		})
	}
}

func FuzzRaw(f *testing.F) {
	for _, s := range scenarios() {
		f.Add(s.in)
	}

	f.Fuzz(func(t *testing.T, s string) {
		masked := urlmask.Raw(s)

		// net/url doesn't always format a URL as something that parses back the same. A host-less path starting
		// with // comes out as "a:%2F/%20" from "A:/%2F ", which parses as opaque, or as "//%20" from "/%2F ",
		// which doesn't parse at all. A second pass may then mask more, never less, so stability is checked from
		// the second pass on, and the structural checks below skip output that doesn't parse back.
		twice := urlmask.Raw(masked)
		assert.Equal(t, twice, urlmask.Raw(twice), "idempotence")

		u, err := url.Parse(s)

		if err != nil || u.Opaque != "" {
			return
		}

		m, err := url.Parse(masked)
		if err != nil {
			return
		}

		assert.Empty(t, m.Fragment, "fragment")

		if password, ok := m.User.Password(); ok {
			assert.Equal(t, "xxxxx", password, "password")
		}

		assertMaskedEntries(t, m.RawQuery, "query entry")

		for segment := range strings.SplitSeq(m.EscapedPath(), "/") {
			if _, params, ok := strings.Cut(segment, ";"); ok {
				assertMaskedEntries(t, params, "path parameter")
			}
		}
	})
}

// assertMaskedEntries checks that every &- or ;-separated entry of s is Masked or <key>=Masked.
func assertMaskedEntries(t *testing.T, s, what string) {
	t.Helper()

	for entry := range strings.FieldsFuncSeq(s, func(r rune) bool { return r == '&' || r == ';' }) {
		_, value, _ := strings.Cut(entry, "=")
		assert.True(t, entry == urlmask.Masked || value == urlmask.Masked, "%s %q", what, entry)
	}
}
