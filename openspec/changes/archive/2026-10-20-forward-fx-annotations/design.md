## Context

See proposal.md for why. The requirement is in the kernelrun delta spec. This change was worked out on the `add-httpserver` branch, during that module's second code review, and is released first so `http/httpserver` can forward its runner's name tag through `kernelrun.ProvideRunner`.

fx facts it relies on (go.uber.org/fx v1.24.0):

- `fx.Annotate(f, anns...)` applies its annotations in one pass, and an annotated function can't be annotated again.
- Two `fx.ResultTags`, or two `fx.ParamTags`, in one `fx.Annotate` fail when the app is built, as does an `fx.As` beside a second one.
- `fx.Supply` takes an annotated value; parameter annotations on a value that isn't a function fail when the app is built.

## Goals / Non-Goals

**Goals:**

- A runner whose constructor needs a tag built at wiring time goes through `kernelrun.ProvideRunner`, rather than a private adapter type copied into each named-instance module.

**Non-Goals:**

- Typed annotation helpers per module. fx's own annotations are the vocabulary; wrapping them would only rename them.
- Filtering forwarded annotations. fx already rejects a conflicting one with an error that names it.

## Decisions

### 1. `anns ...fx.Annotation`, forwarded after the helper's own

```go
func ProvideRunner(name string, constructor any, anns ...fx.Annotation) fx.Option {
	return fx.Module("kernelrun",
		fx.Provide(fx.Private, fx.Annotate(constructor, append([]fx.Annotation{fx.ResultTags(builtRunner)}, anns...)...)),
		...
	)
}
```

The helper's annotations come first so its result tag is always applied; a caller's conflicting one then fails rather than replacing it. The variadic goes last, so every existing call compiles unchanged.

**Alternatives:** a `paramTags ...string` parameter (narrower, but `fx.From` and hooks are just as reasonable to forward, and a second parameter later would be another signature change); an options struct (heavier than the problem); providing a `kernelrun.Runner` into `kernelrun.RunnerGroup` directly from each named-instance module (works without changing kernelrun, but every such module re-implements `ProvideRunner`).

### 2. Only where tags are built at wiring time

Forwarding was first added to every helper that annotates a caller's constructor: `log.ProvideMiddleware` and the `As*` helpers in log, telemetry and configloader too. It was reverted for those after review. A constructor with fixed dependencies already takes tagged ones through an `fx.In` struct with static tags; forwarding only earns its place where the tag is built from a name the helper is given, which is registration from named-instance modules (`kernelrun.ProvideRunner`) and the named-instance helpers themselves (`httpserver.ProvideServer`, `ProvideMiddleware`). `log` has one logger per app, so its middleware has no instance names either. The constitution's rule draws the same line.

### 3. Action segments and the tag rename

A tag segment naming an action done to the value (`built`, `wired`, `supplied`) is separated by a double colon. Instance names can't contain `:`, so a module's private `…::built` tag can't equal a name tag built from an instance name; `httpserver:server:built` did, for a server named `built`. Applied to every such tag in the repo. The trace tags are reached only through `AsExporter` and `AsTracerProvider`, so the rename changes nothing for callers who use the helpers, as the constitution says callers do.

### 4. Instance names and the positional exception

Both came from `http/httpserver`'s review and are constitution-wide: a later module with named instances needs the same name check (letters, digits, `-`, `_`, `.`; `ErrInvalidName` through `fx.Error`) and the same exception to the single-Params rule for constructors whose tags are built from a name.

### Rollup at archive

Decisions 1 and 2 (why forward rather than wrap or bypass, and only where tags are built at wiring time) into `docs/kernel.md`, beside the runner registration they describe.

## Risks / Trade-offs

- [The signature change breaks code storing `ProvideRunner` as a value] → Pre-1.0, a minor bump with `!`; no known caller does.
- [A caller passes `fx.ResultTags` expecting to rename the result] → fx fails startup naming the conflict; the godoc lists what's meant to be forwarded.

## Migration Plan

One pull request, cherry-picked from the `add-httpserver` branch onto a branch off `origin/main`: kernelrun's forwarding, the tag rename and the constitution rules. The log, telemetry and configloader forwarding commits and their reverts are left out of the replay. Merging publishes kernelrun 0.3.0, log 0.1.1 and telemetry 0.1.1. `add-httpserver` follows in its own pull request, requiring kernelrun v0.3.0.

## What happened

- **Worked out on the `add-httpserver` branch, released first** in #27 (kernelrun 0.3.0, log 0.1.1, telemetry 0.1.1), cherry-picked onto a branch off `main`. `add-httpserver` followed in #28, its runner forwarding its name tag through `kernelrun.ProvideRunner`.
- **Narrowed during review (decision 2).** Forwarding first went into `log.ProvideMiddleware` and the `AsX` helpers in log, telemetry and configloader too, each with a minor bump; those commits were reverted and left out of the replay, so log and telemetry shipped only the tag rename, as patches, and configloader didn't change.
- **Two more constitution rules came along** from `add-httpserver` (named-instance runners as `<module>:<name>`, per-name groups as functions), so the constitution changed in one release.
- **Rolled into `docs/kernel.md`:** decisions 1, 2 and 3, under "Named instances register through ProvideRunner too".
- **Follow-ups left open:** none.
