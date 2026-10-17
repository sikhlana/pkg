# urlmask

Turns a URL into a string that's safe to log. Query and path parameter values, passwords and fragments are masked or dropped, while the scheme, host, path and query keys survive to debug with.

## Configuration

None. `urlmask` is a set of plain functions with one masking policy and nothing for an operator to set.

## Exported Members

| Name | Description |
|------|-------------|
| Masked | The placeholder, \*\*\*, that replaces each query value, path parameter value and bare entry, and everything after the scheme of an opaque URL. |
| URL | Returns a url.URL as a string that's safe to log. A nil URL returns an empty string. Never modifies the URL it's given. |
| Raw | Parses a URL string, such as a Referer or Location header, and masks it as URL does. An empty string returns an empty string, and a string that can't be parsed returns Masked. |
| Query | Masks a raw query, without its leading ?, as URL masks a URL's query. |
| Value | Returns an slog.LogValuer that resolves to URL of the given URL when a handler handles the record, so a dropped record costs no masking. |

## Usage

### Standalone

Mask a request's URL with `URL`, and a URL that arrives as a string, such as a header, with `Raw`:

```go
logger.Info("request",
	slog.String("url", urlmask.URL(r.URL)),
	slog.String("referer", urlmask.Raw(r.Referer())),
)
```

To mask only when the record is actually handled, log `Value` with `slog.Any`. A debug record that the logger drops then never masks:

```go
logger.Debug("calling upstream", slog.Any("url", urlmask.Value(req.URL)))
```

`Value` keeps the URL it's given rather than a copy, so don't change the URL between logging it and the handler handling the record. A request's URL doesn't change.

What each part of a URL becomes:

| Part | Before | After |
|------|--------|-------|
| Query value | ?token=abc&page=2 | ?token=\*\*\*&page=\*\*\* |
| Bare query entry | ?eyJhbGci.payload.sig&lang=en | ?\*\*\*&lang=\*\*\* |
| Empty query value | ?token= | ?\*\*\* |
| Semicolon separator | ?s3cr3t;a=1 | ?\*\*\*;a=\*\*\* |
| Path parameter | /cart;jsessionid=8F3A1C/items | /cart;jsessionid=\*\*\*/items |
| Password | https://alice:hunter2@host | https://alice:xxxxx@host |
| Fragment | /cb#access_token=SECRET | /cb |
| Opaque URL | mailto:alice@example.com | mailto:\*\*\* |
| Unparseable string | https://app example.com/?t=1 | \*\*\* |
| Scheme, host and port | HTTPS://Example.com:8443 | https://Example.com:8443 |
| Path segment | /reset/8F3A1C | /reset/8F3A1C |
| Escaped semicolon | /a%3Bjsessionid=8F3A1C | /a%3Bjsessionid=8F3A1C |
| Query key | ?sk_live_abc=1 | ?sk_live_abc=\*\*\* |

Both `&` and `;` separate query entries, so a token in front of a `;` can't end up inside a key. An entry with no value, or a value of only `=` characters, is masked whole, key included, since it's often a token itself. Keys stay in their order, repeated keys are kept and nothing is unescaped or re-escaped, so the query keeps its shape. Passwords become `xxxxx`, the placeholder `url.URL.Redacted` writes, since net/url escapes `*` in user information.

Masking doesn't cover query keys, path parameter names, path segments outside their parameters, usernames or hosts. They're logged as written, so a secret placed in one of them, such as a reset token in the path, isn't masked. Only a literal `;` starts path parameters: an escaped one, `%3B`, is data under RFC 3986, so `/a%3Bjsessionid=8F3A1C` is logged as written. Don't rely on `urlmask` for those.

### With fx

`urlmask` has no Module: it holds no state and needs no wiring. A module that logs URLs calls it directly, with its injected logger:

```go
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	c.logger.Debug("sending request",
		slog.String("method", req.Method),
		slog.Any("url", urlmask.Value(req.URL)),
	)

	return c.http.Do(req)
}
```
