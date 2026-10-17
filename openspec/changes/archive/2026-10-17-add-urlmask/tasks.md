## 1. Scaffold

The module is new, so it starts at 0.1.0 with no bump task. It has no mocked interface, so there's no go generate task. Nothing is copied from go-modules' `sanitizeURL` or `accesslog` maskers: code, godoc and comments are all written here, from what this code does.

- [x] 1.1 Branch `add-urlmask` off `origin/main` after `git fetch`, carrying this change's artifacts over.
- [x] 1.2 Create `url/urlmask/` with:
  - a `go.mod` for `github.com/sikhlana/pkg/url/urlmask`, with the go directive from `.go-version`;
  - a `VERSION` containing `0.1.0`;
  - stretchr/testify as the only requirement, for tests.
  - Done when `go build ./...` passes.
- [x] 1.3 Register `./url/urlmask` in the root `go.work`.

## 2. Implementation

- [x] 2.1 `urlmask.go`: `Masked` and the unexported entry masker that splits on `&` and `;`, keeps separators and empty entries, writes `***` for bare entries and `<raw key>=***` otherwise. See design decision 2.
- [x] 2.2 `Query`, which is the entry masker, and the unexported path masker, which cuts each segment of the escaped path at its first `;` and masks the tail, assigning the result to both `RawPath` and `Path`. See design decision 3.
- [x] 2.3 `URL`, following design decision 4's order: nil, opaque, a copy, query, path, fragment, then `Redacted`. `Raw`: `""` for an empty string, `Masked` when parsing fails, `URL` otherwise.
- [x] 2.4 `Value`: an unexported struct holding the `*url.URL` whose `LogValue` returns `slog.StringValue(URL(u))`. Its godoc says the URL is masked when the record is handled, so it shouldn't change in between. See design decision 6.
- [x] 2.5 Godoc for every export, written fresh: what's masked, what's passed through, and what isn't guaranteed (keys, parameter names, path segments, usernames, hosts).

## 3. Tests

- [x] 3.1 `urlmask_test.go`: a table test for every scenario in specs/urlmask/spec.md. Scenarios given as URL strings run through both `Raw` and `URL` of the parsed URL, and must agree. Query scenarios also run through `Query`.
- [x] 3.2 A test that `URL` leaves the caller's URL unchanged: password, `RawQuery`, `Path`, `RawPath` and fragment.
- [x] 3.3 A `Value` test through `slog.NewTextHandler` writing to a buffer, for a URL with a password and a query, and for a nil URL.
- [x] 3.4 `FuzzRaw`, seeded from the table, checking that `Raw` never panics, that `Raw(Raw(s)) == Raw(s)`, and that output from a parseable, non-opaque input parses back with no fragment, no password other than `xxxxx`, and every query entry `***` or `<key>=***`. Run it with `-fuzz=FuzzRaw -fuzztime=60s` once, and add any crasher it finds to the seed corpus. If idempotence fails because of normalisation, weaken the property as design Risks says and record why.
- [x] 3.5 `example_test.go`: examples for `URL`, `Raw` and `Value` with their output.

## 4. Docs

- [x] 4.1 Write `url/urlmask/README.md` in the constitution's order:
  - the overview;
  - Configuration: none, so say so;
  - Exported Members: `Masked`, `URL`, `Raw`, `Query` and `Value`;
  - Usage, Standalone: masking a request URL, a Referer header and a log value, with a before→after table of what's masked, what's passed through, and what isn't guaranteed;
  - Usage, With fx: none of its own, so say a consumer calls it directly;
  - no backticks in table cells.
- [x] 4.2 In the root `README.md`, add the `url/urlmask` row to the Modules table in alphabetical order.

## 5. Verification

- [x] 5.1 In `url/urlmask/`: `modernize -fix ./...`, then `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), then the full `go test ./...`.
- [x] 5.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 5.1 and 5.2.
