## Why

Any module that logs a URL (an access log, an HTTP client, a webhook sender) can leak secrets in it: passwords in the user information, tokens in query values, session IDs in path parameters, OAuth tokens in the fragment. go-modules masks URLs twice, with two policies that have drifted apart: `http/client`'s `sanitizeURL` and `http/accesslog`'s stricter `maskQuery` and `maskReferer`. Neither can be reused by the other. Both have gaps. A token can leak through a query key when `;` separates entries, and neither masks path parameters. Rebuilding the policy here as one small module, before `http/accesslog` and `http/httpclient` arrive, gives every module that logs a URL the same masking.

## What Changes

- New module `url/urlmask` (`github.com/sikhlana/pkg/url/urlmask`), starting at 0.1.0. It turns a URL into a string that is safe to log:
  - **Query values are masked.** Every value becomes `***`. Keys keep their raw, escaped form and their original order, and a repeated key keeps every occurrence.
  - **Bare entries are masked whole.** An entry with no `=`, or whose value is empty or only `=` characters, becomes `***`, since it's often a token itself (a JWT, padded base64). It stays where it was in the query.
  - **`&` and `;` both separate entries**, so a token in front of a `;` can't show up inside a key.
  - **Path parameters are masked.** In each path segment, the value of every `;`-separated parameter (RFC 3986's matrix parameters, as in `;jsessionid=…`) is masked the same way query values are. The rest of the path is passed through.
  - **Passwords are redacted, usernames kept.** User information goes through `url.URL.Redacted`, so the password becomes `xxxxx` and the username stays. `xxxxx` rather than `***` because net/url percent-encodes `*` in user information.
  - **The fragment is dropped**, since it carries the OAuth implicit flow's `#access_token=…`.
  - **Scheme, host and port are passed through.**
  - **Unusual input is masked, not echoed.** An opaque URL (`mailto:…`, `https:user:pass@host`) becomes `scheme:***`. An unparseable string becomes `***`. A nil or empty URL becomes `""`.
  - **What isn't guaranteed is stated.** Query keys, parameter names and path segments outside parameters are logged as written. A secret placed there isn't masked.
  - **Entry points:** `URL` for a `*url.URL`, `Raw` for a URL string (a Referer or Location header), `Query` for a bare raw query, and `Value`, an `slog.LogValuer` that masks only when a record is actually emitted. `Masked` exports the `***` placeholder.
- How it differs from go-modules:
  - One shared module instead of two private copies.
  - Keys aren't sorted, unescaped or re-escaped. Sorting was left over from `url.Values` being a map. Dropping it keeps the query's shape and removes the "undecodable key masks the whole query" path.
  - Bare entries stay in place rather than moving to the end.
  - Usernames are kept and passwords redacted, where `maskReferer` dropped all user information.
  - Opaque and unparseable input give `scheme:***` and `***` instead of `""`, so they can't be mistaken for an absent URL.
  - New: `;` as a query separator, path parameter masking, and lazy masking through `Value`.
- The root README gains a Modules row, and `go.work` registers the module.

## Capabilities

### New Capabilities

- `urlmask`: query value and bare entry masking, with key order and repeated keys kept; `&` and `;` as separators; path parameter masking; password redaction keeping the username; fragment removal; pass-through of scheme, host, port and the rest of the path; handling of opaque, unparseable, nil and empty input; lazy masking through a log value; and what the masking explicitly doesn't cover.

### Modified Capabilities

None.

## Impact

- **`url/urlmask` (new module):** all of its exported API is new. Its exposure shape is plain functions (`URL`, `Raw`, `Query`, `Value`) plus the `Masked` constant. It's stateless, so it has no fx Module, no Config and no runner.
- **Other modules:** none touched. Future `http/accesslog` and `http/httpclient` modules are expected to depend on it.
- **New dependencies:** none beyond the standard library. testify is used by tests only. Parsing goes through `net/url`. Only the masking policy is hand-written.
- **Docs and repo files:** `README.md` (a Modules row), `go.work` (`use ./url/urlmask`).
- **Release:** one pull request, since the module requires no sibling. Merging publishes `url/urlmask/v0.1.0`.
