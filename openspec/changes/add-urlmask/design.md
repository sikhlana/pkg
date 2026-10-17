## Context

The sources are go-modules' `http/client` `sanitizeURL` and `http/accesslog` `maskQuery`, `maskURL` and `maskReferer`. See proposal.md for why they're rebuilt as one module and how the policy differs. The requirements are in specs/urlmask/spec.md. This document covers how they're met.

Facts that shape the approach, checked against Go 1.27's net/url:

- **`url.URL.String` percent-encodes `*` in user information and in `Path`,** but not in `RawQuery`, which it writes as is, or in `RawPath`, when `RawPath` is a valid encoding of `Path`. Setting `Path` alone to `/a;k=***` prints `/a;k=%2A%2A%2A`.
- **`url.URL.Redacted` replaces only the password, with `xxxxx`,** and leaves a username-only `User` as it is. It works on a copy, so it doesn't change the URL it's called on.
- **`url.URL.EscapedPath` keeps the escaping a path was written with** (`%2F` stays `%2F`) whenever `RawPath` is valid, and `;` is never escaped in a path.
- **`url.ParseQuery` loses order, can't tell a bare entry from an empty value, and fails on `;`** ("invalid semicolon separator in query"), so it can't serve here.
- **`url.Parse` fails on input such as a space in the host or an invalid `%` escape in the path,** and accepts relative references (`/p?x=1`) and opaque URLs (`https:user:pass@host`, where `Opaque` holds everything after the scheme).

## Goals / Non-Goals

**Goals:**

- One masking policy that any module logging a URL calls, with no fx wiring and no dependency beyond the standard library.
- Output that's still useful for debugging: the query's keys, their order and the URL's overall shape survive.
- Masking that costs nothing when the record carrying the URL is dropped.

**Non-Goals:**

- Telling secret path segments from slugs (`/reset/<token>`). Only path parameters, which RFC 3986 defines by syntax, are masked.
- An allow-list of query keys whose values are safe (`page`, `lang`). It can come later as an additive type, such as a `Masker` with options, without changing the functions.
- Finding and masking URLs inside free text (a log message, an error string).
- Masking headers, cookies or request bodies.

## Decisions

### 1. The shape of the package

```go
package urlmask

const Masked = "***"

func URL(u *url.URL) string
func Raw(s string) string
func Query(rawQuery string) string
func Value(u *url.URL) slog.LogValuer
```

Plain functions, since there's no state and nothing an operator would configure. `URL` is the core. `Raw` parses and calls it. `Query` exposes the entry masker on its own. `Value` defers `URL`. The code lives in `urlmask.go`, with tests in `urlmask_test.go`. Examples go in `example_test.go`, so pkg.go.dev shows `URL`, `Raw` and `Value` in use.

### 2. One entry masker for the query and for path parameters

A single unexported function walks a string, splitting it on `&` and `;` and keeping each separator as written. For each entry, it writes:

- nothing for an empty entry,
- `***` when there's no `=`, or the value is empty or only `=` characters,
- the raw key, then `=***`, otherwise.

`Query` is that function. For the path, each `/`-separated segment of `EscapedPath()` is cut at its first `;`. The head is kept, and the tail goes through the same function. Query entries and path parameters then follow one rule, and the spec's `;` separator holds in both places.

Alternatives: `url.ParseQuery` fails the order, bare-entry and `;` requirements (see Context). A regular expression would be harder to read than a 20-line loop, with no gain.

### 3. Work on the escaped forms, never the decoded ones

Keys and path heads are copied from `RawQuery` and `EscapedPath()` byte for byte. Nothing is unescaped, so there's no decode error to handle, a key can't be re-escaped differently than it was written, and `%2F` in a segment stays a `%2F` rather than turning into a segment boundary. The masked path is assigned to both `RawPath` and `Path` (through `url.PathUnescape`, which can't fail on a string built from a valid escaped path with `***` added). That keeps `RawPath` a valid encoding of `Path`, so `String` writes it as is, `*` included.

The source unescaped keys to sort them. With sorting gone (proposal.md, What Changes), unescaping has no purpose left.

### 4. The order of operations in `URL`

```
nil              → ""
Opaque != ""     → Scheme + ":" + Masked
copy *u          (the caller's URL is never written to)
RawQuery         → masked by the entry masker (left alone when empty)
Path, RawPath    → path parameters masked (left alone when there's no ";")
Fragment, RawFragment → ""
return copy.Redacted()
```

`Redacted` goes last because it's the one step that formats the URL. Every other step edits fields on the copy. A shallow copy is enough, since only `User` is a pointer and nothing writes to it.

`Raw` returns `""` for an empty string, `Masked` when `url.Parse` fails, and `URL(parsed)` otherwise. For an empty string, parsing would also yield `""`, but saying so up front keeps the rule visible.

### 5. Placeholders: `***`, `xxxxx` and `scheme:***`

`***` marks everything the module masks itself. The password uses `Redacted`'s `xxxxx` instead of building user information by hand to get `***` (see Context for why `*` is escaped there). The mixed placeholders are the price of not hand-formatting user information. Opaque URLs keep their scheme, which is never secret and tells a reader what kind of URL was there. Unparseable input becomes `***` rather than `""`, so it can't be mistaken for an absent URL.

### 6. `Value` defers masking through `slog.LogValuer`

`Value` returns an unexported struct holding the `*url.URL`, whose `LogValue` returns `slog.StringValue(URL(u))`. slog resolves a `LogValuer` only when a handler handles the record, so a disabled debug record never masks. That's the property `http/client` relied on in go-modules. A caller writes `slog.Any("url", urlmask.Value(u))`. That's the constitution's allowed `slog.Any`, since no typed constructor takes a `LogValuer`.

The struct holds the pointer rather than a copy, so a URL changed between building the record and handling it is logged as changed. Copying would cost an allocation on every call, logged or not. The godoc says to pass a URL that won't change, which a request's URL already is.

Alternative: an `Attr(key string, u *url.URL) slog.Attr` helper. It would hide the `slog.Any` at call sites, but it adds a second way to do the same thing. It can come later if call sites want it.

### 7. Tests

- A table test per spec requirement, run through `URL`, with the string-form scenarios run through `Raw` too, so every scenario checks that both forms agree.
- A test that the caller's URL is unchanged after `URL`.
- A test that runs `Value` through `slog.NewTextHandler` writing to a buffer.
- `FuzzRaw`, seeded from the table, checking properties that hold for any input: it never panics; `Raw(Raw(s)) == Raw(s)`, since masked output has nothing left to mask; and output from a parseable, non-opaque input parses back with no fragment, no password other than `xxxxx`, and every query entry either `***` or `<key>=***`. Checking structure, not substrings, keeps short passwords such as `a` from failing on a coincidental match elsewhere in the URL. Plain `go test` runs it over the seed corpus only, so it doesn't slow the `-short` sweep.

## Risks / Trade-offs

- **A secret in a query key, parameter name or path segment is logged** → the spec states it, and the README says so under Usage. Masking keys would lose most of what makes the output useful.
- **Values that help debugging (`page=2`, `lang=en`) are masked too** → it's the safe default. An allow-list is a non-goal for now and would be additive.
- **A path that uses `;` for something other than parameters (`/files/a;b.txt`) loses everything after the `;`** → that's masking too much, never too little, and such paths are rare.
- **`Value` holds the caller's pointer** → a URL changed before an async handler resolves it is logged as changed. The godoc says so. Request URLs don't change.
- **`FuzzRaw`'s idempotence property may find an input whose `EscapedPath` normalises on the first pass** → if it does, the property is weakened to compare the second and third passes, and design.md records the case at archive.

Durable rationale to roll into docs at archive: why keys are kept while values are masked, why `&` and `;` both separate entries, and why masking works on escaped forms. If more modules start masking for logs, these would sit in a `docs/logging.md`. Otherwise the module's README is enough.

## Migration Plan

Nothing consumes the module yet. One pull request carries the module, its root README row and its `go.work` entry. The module requires no sibling, so there's no release order. Merging publishes `url/urlmask/v0.1.0`.
