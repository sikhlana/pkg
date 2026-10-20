## 1. log middleware

No mocked interface changes in either module, so there's no go generate task.

- [x] 1.1 `log/middleware.go`: `Middleware` as an interface with `Handle`; `OrderedMiddleware` as an interface adding `Order() int`; `MiddlewareFunc`; `Ordered`, unwrapping an existing wrapper; the unexported wrapper and `orderOf`, per design decisions 1 and 2.
- [x] 1.2 `log/middleware.go`: `sortMiddleware` filters with `isNil` (unwrapping `Ordered`) and stable-sorts by `orderOf`; `chain` calls `m.Handle`, per design decisions 3 and 4.
- [x] 1.3 `log/middleware.go`: `WithMiddleware(m)` supplying `m` as `Middleware` into `MiddlewareGroup`, returning `fx.Options()` for a nil interface; `ProvideMiddleware(constructor)` annotating with `fx.As(new(Middleware))` and the group tag; `builtMiddleware`, `builtParams` and the `fx.Module` wrapper removed, per design decision 5.
- [x] 1.4 `log/log.go`: `Params.Middleware` as `[]Middleware` with its field comment; `isNil` widened to `any`, `reflect.Interface` dropped from `nilableKinds`; the package doc's mention of middleware still accurate.
- [x] 1.5 Update the existing tests in `handler_test.go`, `middleware_test.go`, `writer_test.go` and `helpers_test.go` to the new types.
- [x] 1.6 Tests for the modified requirement: a declared order; an order set with `Ordered` overriding a declared one; `Ordered` of an `Ordered`; an undeclared order counting as 0; equal orders starting and running both; a nil constructor result, a nil pointer and a nil wrapped by `Ordered` left out; `WithMiddleware(nil)` supplying nothing; values and constructors ordered together; the same ordering and leaving out through `New(Params{...})` without fx.
## 2. telemetry log middleware

- [x] 2.1 `telemetry/log.go`: `LogMiddleware` as an empty struct with `Handle` and `Order() int` returning 500; `LogMiddlewareOrder` removed, per design decision 6.
- [x] 2.2 `telemetry/log_test.go`: call `LogMiddleware{}.Handle` in place of the function; assert through a local `interface{ Order() int }` that it declares 500, without importing log, per design decision 7.

## 3. Docs and constitution

- [x] 3.1 `log/README.md`: members table (`Middleware`, `OrderedMiddleware`, `Ordered`, `MiddlewareFunc`, `Params.Middleware`, `MiddlewareGroup`, `WithMiddleware`, `ProvideMiddleware`); the Standalone and With fx examples; the Middleware section's bullets on order (declared, overridden with `Ordered`, 0 by default), nil left out, and the bands kept.
- [x] 3.2 `telemetry/README.md`: the `LogMiddleware` row as a type at order 500, the `LogMiddlewareOrder` row removed, and the registration example as `log.WithMiddleware(telemetry.LogMiddleware{})`.
- [x] 3.3 `docs/middleware.md` (new): dig's shuffle, order on the value with an override, ties, nil meaning off, per design decision 8. The alternatives-considered rollup waits for archive.
- [x] 3.4 `docs/log.md`: replace "The app orders the middleware" with a pointer to `docs/middleware.md`; update "Middleware is a step a record passes through" for the interface and the `Handle` clash.
- [x] 3.5 `docs/http.md`: trim "Middleware order comes from the middleware" and "Leaving middleware out" to what's specific to httpserver, linking `docs/middleware.md`.
- [x] 3.6 `openspec/config.yaml`: the ordered-group rule beside the group-tag rules, per design decision 8.
- [x] 3.7 Bump `log/VERSION` (minor) to 0.2.0.
- [x] 3.8 Bump `telemetry/VERSION` (minor) to 0.2.0.

## 4. Verification

- [x] 4.1 In `log`: `modernize -fix ./...`, `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), the full `go test ./...`, then `govulncheck ./...`.
- [x] 4.2 In `telemetry`: the same chain as 4.1.
- [x] 4.3 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 4.1–4.3.

## 5. Code review fixes

- [x] 5.1 Reviewed and rejected: `WithMiddleware` and `WithDestination` keep `fx.Supply` though it panics on a value that also implements `error`. No realistic middleware or writer does, and the constitution has every `WithX` wrap `fx.Supply`.
- [x] 5.2 `nilableKinds`' comment says what the list now holds, with `reflect.Interface` gone.
- [x] 5.3 Re-run 4.1 on `log`, then 4.3 across `go.work`.
