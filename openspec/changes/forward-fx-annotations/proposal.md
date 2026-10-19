## Why

`kernelrun.ProvideRunner` runs `fx.Annotate` on the caller's constructor itself, and fx can't annotate a constructor twice. So a runner whose constructor needs a dependency under a tag built at wiring time, such as one of several named servers, can't go through it: `http/httpserver` had to resolve its server's name tag through a private adapter type before handing its runner to kernelrun, and every later module with named instances would copy it. This change lets `ProvideRunner` take the caller's annotations and forward them.

## What Changes

- `kernelrun.ProvideRunner(name, constructor, anns...)` takes `anns ...fx.Annotation` after its other parameters and passes them to `fx.Annotate` after its own result tag. They're for the constructor's inputs and hooks (`fx.ParamTags`, `fx.From`, `fx.OnStart`, `fx.OnStop`); one that repeats the result tag fails startup with fx's error. **BREAKING** for the function's signature: callers compile unchanged, but code that stores `ProvideRunner` as a value of its old type doesn't.
- Private fx tags whose last segment names an action done to the value (`built`, `wired`, `supplied`) separate it with a double colon: `kernelrun:runner::built`, `log:middleware::built`, `telemetry/trace:exporter::wired`, `telemetry/trace:provider::supplied`. Only the helpers build these tags, so callers using them see no change.
- Constitution (with two rules from `add-httpserver`, released here so the constitution lands in one tier):
  - A module whose loops are named instances takes the name in `ProvideRunner(name)` and registers its runner as `<module>:<name>`, in place of `DefaultRunnerName`.
  - A group keyed by an instance name is exposed as a function returning its tag (`MiddlewareGroup(name)`), since it can't be a constant.
  - A helper whose caller's constructor may need a tag built at wiring time (registering into another module's machinery, or building named instances) takes and forwards `anns ...fx.Annotation`. `AsX` helpers, which annotate a value or a constructor with fixed dependencies, don't: an `fx.In` struct with static tags covers that.
  - A segment naming an action done to the value is separated by a double colon.
  - Instance names (one of several named servers) are non-empty and limited to letters, digits, `-`, `_` and `.`, checked by every helper taking one, failing startup with an exported `ErrInvalidName`.
  - A constructor whose tags depend on an instance name given at wiring time takes positional parameters with `fx.ParamTags`, the one exception to the single-Params rule.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `kernelrun`: a runner's constructor can take tagged dependencies through annotations forwarded by `ProvideRunner`.

## Impact

- **`kernelrun`:** `ProvideRunner`'s signature gains the variadic (exported API changes, minor bump); `builtRunner`'s tag is renamed (internal).
- **`log`:** `builtMiddleware`'s tag is renamed (internal, patch bump).
- **`telemetry`:** the trace exporter and supplied-provider tags are renamed (reached only through `AsExporter` and `AsTracerProvider`, patch bump).
- **No new dependencies.**
- **Docs and repo files:** `openspec/config.yaml` (the six rules), kernelrun's README (the helper's row, a self-contained loop-module example, and a named-instance example).
- **Release:** one tier, merged before `add-httpserver`, whose runner then forwards its name tag through `kernelrun.ProvideRunner`.
