// Package urlmask turns a URL into a string that is safe to log. It masks query values, path parameter values and
// passwords, and drops the fragment, while keeping the scheme, host, path and query keys to debug with.
package urlmask

import (
	"log/slog"
	"net/url"
	"strings"
)

// Masked is the placeholder that replaces each query value, path parameter value and bare entry, and the part after
// the scheme of an opaque URL.
const Masked = "***"

// URL returns u as a string that is safe to log. Every query value and path parameter value becomes Masked, and a
// query entry or path parameter with no value becomes Masked whole. Both & and ; separate entries. A password becomes
// xxxxx, the placeholder url.URL.Redacted writes, and the fragment is dropped. An opaque URL becomes its scheme
// followed by :***, and a nil URL becomes "".
//
// The host, port and username, query keys, path parameter names and the rest of each path segment are passed through
// as written, escaped form included, and the scheme comes out lowercase. A secret placed in one of them isn't masked.
// Only a literal ; starts path parameters: an escaped one, %3B, is data under RFC 3986 and is passed through with the
// rest of its segment.
//
// u is never modified.
func URL(u *url.URL) string {
	if u == nil {
		return ""
	}

	if u.Opaque != "" {
		return u.Scheme + ":" + Masked
	}

	c := *u

	if c.RawQuery != "" {
		c.RawQuery = maskEntries(c.RawQuery)
	}

	if escaped := c.EscapedPath(); strings.Contains(escaped, ";") {
		masked := maskPath(escaped)

		// Unreachable: masking only replaces whole values, never part of a %XX escape. It fails closed all the same.
		path, err := url.PathUnescape(masked)
		if err != nil {
			return Masked
		}

		c.Path, c.RawPath = path, masked
	}

	c.Fragment, c.RawFragment = "", ""

	return c.Redacted()
}

// Raw parses s as a URL, such as a Referer or Location header, and masks it as URL does. An empty string returns "",
// and a string that can't be parsed returns Masked rather than being echoed.
func Raw(s string) string {
	if s == "" {
		return ""
	}

	u, err := url.Parse(s)
	if err != nil {
		return Masked
	}

	return URL(u)
}

// Query masks a raw query, without its leading ?, as URL masks a URL's query: each value becomes Masked, an entry
// with no value becomes Masked whole, and keys, separators and empty entries are kept as written.
func Query(rawQuery string) string {
	return maskEntries(rawQuery)
}

// Value returns an slog.LogValuer that resolves to URL(u). The masking happens when a handler handles the record,
// so a record the logger drops costs nothing. Value keeps u rather than a copy, so u shouldn't change between
// logging and handling, which an asynchronous handler can delay. Log it with slog.Any.
func Value(u *url.URL) slog.LogValuer {
	return value{u: u}
}

type value struct {
	u *url.URL
}

func (v value) LogValue() slog.Value {
	return slog.StringValue(URL(v.u))
}

// maskEntries masks each &- or ;-separated entry of s, keeping the separators and empty entries.
func maskEntries(s string) string {
	var b strings.Builder

	// Each entry grows by at most len(Masked)-1 bytes: a one-byte value or bare entry becomes Masked.
	b.Grow(len(s) + (strings.Count(s, "&")+strings.Count(s, ";")+1)*(len(Masked)-1))

	for {
		i := strings.IndexAny(s, "&;")

		if i < 0 {
			writeEntry(&b, s)

			return b.String()
		}

		writeEntry(&b, s[:i])
		b.WriteByte(s[i])

		s = s[i+1:]
	}
}

// writeEntry writes entry with its value masked. An entry with no value is often a token itself, so it's masked whole.
func writeEntry(b *strings.Builder, entry string) {
	if entry == "" {
		return
	}

	key, value, ok := strings.Cut(entry, "=")

	if !ok || strings.Trim(value, "=") == "" {
		b.WriteString(Masked)

		return
	}

	b.WriteString(key)
	b.WriteString("=" + Masked)
}

// maskPath masks the parameters after the first ; in each segment of an escaped path.
func maskPath(escaped string) string {
	segments := strings.Split(escaped, "/")

	for i, segment := range segments {
		if head, params, ok := strings.Cut(segment, ";"); ok {
			segments[i] = head + ";" + maskEntries(params)
		}
	}

	return strings.Join(segments, "/")
}
