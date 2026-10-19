## 1. Forwarding

No mocked interface changes, so there's no go generate task.

- [x] 1.1 `kernelrun`: `ProvideRunner(name, constructor, anns...)`, forwarded after its result tag; godoc; README row and a named-instance example; tests for a tagged dependency and a repeated result tag. Bump `kernelrun/VERSION` (minor).
- [x] 1.2 Forwarding added to `log.ProvideMiddleware` and the `As*` helpers in log, telemetry and configloader, then reverted (design decision 2).

## 2. Tag rename

- [x] 2.1 Separate action segments with a double colon: `kernelrun:runner::built`, `log:middleware::built`, `telemetry/trace:exporter::wired`, `telemetry/trace:provider::supplied`. Bump `log/VERSION` and `telemetry/VERSION` (patch); kernelrun's minor in 1.1 covers its rename.

## 3. Constitution

- [x] 3.1 In `openspec/config.yaml`: forwarding only where a constructor may need a tag built at wiring time, action segments, instance names, and the positional-parameter exception.

## 4. Verification

- [x] 4.1 In `kernelrun`, `log` and `telemetry`: `modernize -fix ./...`, `golangci-lint run --fix ./...` (and again without `--fix` if it changed files), the full `go test ./...`, then `govulncheck ./...`.
- [x] 4.2 Run `go test -short ./...` across every module in `go.work`. If it fails, fix it and repeat 4.1–4.2.
