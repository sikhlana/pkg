# Configuration

[config/configvalidator/README.md](../config/configvalidator/README.md) and [config/configloader/README.md](../config/configloader/README.md) say how to validate and load a Config. This page says why the two modules are shaped that way. The full discussion, including the alternatives rejected, is in the add-config change's design.md under [openspec/changes](../openspec/changes).

## Two modules, not one

Every module with a Config validates it, but only apps load one. A single module doing both makes every module that only validates inherit viper's dependency tree (viper, fsnotify, afero, the file-format parsers) for nothing. So validation lives in `config/configvalidator`, whose only dependency is go-playground/validator, and loading in `config/configloader`, which apps import.

The key rules sit in `config/configvalidator` and the loader imports them: how a struct field maps to a key segment (its mapstructure name, its Go name when untagged, nothing when squashed, excluded when tagged `-` or unexported) and which types decode themselves from text. Error keys, env names, defaults and the unknown-key check all come from those two functions, so the key in a validation error is always the key an operator would write in the file, and its env name follows from it. Duplicating them in the loader was rejected: the copies drift the first time one gains a case.

## Modes: skip the dormant struct, delegate to the active one

A Config that picks one of several implementations (a file queue or AMQP, say) holds a struct per mode, and only one is filled in. If the tag pass reached every mode's struct, a `required` rule in a dormant one would fail, which is why go-modules banned `required` outright and every module author paid for it.

Instead, each mode-specific field is tagged `validate:"-"`, so the tag pass never enters it, and the parent's `Validate` method checks the active one with `configvalidator.ValidateField(&c, &c.AMQP)`. That runs the full check (tags, then `Validate` methods) on the active struct, with each violation under the field's key, so the operator reads `messaging.amqp.url`, a key they can write, not `messaging.url`. `ValidateField` finds the field by address rather than taking its key as a string, so the key can't drift from the mapstructure tag, and it works with value receivers because both pointers point into the same copy.

## Where the search looks

An explicit config file is the only file read. Without one, the first file found wins, most specific place first: `config.<ext>` in the app's extra search directories, then `.<name>.<ext>` in the working directory, then `config.<ext>` in `$HOME/.config/<name>/`, `$HOME/.<name>/` and `/etc/<name>/`.

- **The working directory's file is named after the app,** so a project's unrelated `config.yaml` is never read. That's what lets it come ahead of the user's files, as a project-local `.golangci.yml` does.
- **The user's directories come before `/etc`.** With the first file winning, a system-wide file would otherwise shadow a user's own.
- **Only formats viper can decode are tried,** so a stray `config.ini` never stops the search.
- **`$HOME/.config` is used as written,** on macOS too, rather than `os.UserConfigDir()`, whose macOS answer is `~/Library/Application Support`.
- **A file that fails to parse is reported by format and position only.** Parsers quote what they choke on (dotenv quotes the whole line), and a typo beside a secret would put the secret in the error.

Viper's own search looks for one file name across directories, so the loader searches itself and hands viper the file it found.

## Layering

The loader builds the config from three layers, each overriding the one before for the keys it sets: typed defaults, the config file and the environment.

Defaults are a value of the config type rather than a `func(*viper.Viper)`, so viper stays out of the API and a default can't name a key that doesn't exist. They go in through `SetDefault`, one leaf at a time, rather than by decoding the file and the environment over a copy of the defaults, because viper then resolves each key on its own:

- **A slice is replaced as a whole.** mapstructure decoding over a pre-filled slice keeps its elements and decodes each new one over the old one at its index, so a file's `hosts: [{name: x}]` over default hosts would inherit the first default's port.
- **A map merges entry by entry.** Default `labels.team` survives a file that sets only `labels.zone`.
- **A nil section stays nil.** Leaves under a nil pointer in the defaults get no default, so a section is only allocated when the file or the environment sets something in it.
- **Defaults are copied.** Viper keeps what `SetDefault` is given, so the loader deep-copies each default first. Otherwise the loaded config would share maps and slices with `Options.Defaults`, and changing one would change every later load.

## Env names, bound one by one

Viper only decodes keys it already knows about, and `AutomaticEnv` doesn't change that: it answers lookups for keys in the file or the defaults, so a leaf mentioned in neither is never read from the environment. Worse, with `AutomaticEnv` on, a variable named after a section (`MY_SVC_HTTP`) shadows every key beneath it.

So the loader walks the config type, computes every leaf's env name and binds each one explicitly with `BindEnv(key, name)`, with no `AutomaticEnv`, env prefix or key replacer. Viper reads exactly the names the walk computed and nothing else. Map entries come from the file and the defaults, and each entry's leaves come from the map's value type, so an entry's field the file omits is still settable. An entry that neither defines can't be created from the environment; there's no name to bind until something says the entry exists.

A single underscore separates segments, as operators expect, which makes names ambiguous: `http.read_timeout` and `http_read.timeout` both become `MY_SVC_HTTP_READ_TIMEOUT`. Rather than an unconventional double-underscore separator, every name and its `_FILE` twin go into one map before anything is read, and a second key claiming a name fails the load naming both. The same check catches `tls.key` beside `tls.key_file`, and map entries from operator content such as `acme-corp` beside `acme_corp`, which surface at deploy time with both keys in the error.

## `_FILE` and rotation

Container platforms mount secrets as files, and putting a secret in an environment variable leaks it to child processes and crash dumps. So every key also reads `<ENV>_FILE`: the file it names is read once, during the load, and its contents go into viper's override layer with `Set`. Since `X` and `X_FILE` can't both be set, that lands exactly at environment priority. The loader never calls `os.Setenv`, which would put the secret back into the environment the feature exists to keep it out of, and would leak between loads in one process.

That rule is why a Config never has both `x` and `x_file`: the second's env name is the first's file twin. A module that reads a file itself names the field `*_file`, and a field holding a path used as a path, such as a socket or a directory, `*_path`.

`_FILE` is read once, so a rotating credential doesn't belong there. A module that needs one takes a `*_file` field and re-reads the file, as Azure's SDK does with `AZURE_FEDERATED_TOKEN_FILE`, and as `tls/tlsconfig` does with its key pairs and CA files ([tls.md](tls.md)).

## Handing sections to modules with fx.Out

`configloader.Provide` provides the app's whole Config type. When that type embeds `fx.Out`, fx provides each field instead, so `httpserver.Config` and `audit.Config` reach their modules directly, with no adapter function per section. The embedded `Out` field has no exported fields, so it adds no keys or env names. Two sections of the same type need `name` tags, as any fx result would.

`configloader` is the one module without a `Module` variable: its provider is generic over the app's config type, and a variable can't be.

## What the loader logs

The loader reports through a `*slog.Logger` it's given: `Options.Logger`, or under fx the app's logger, which `log.Module` provides. It doesn't import `log`, since `*slog.Logger` is the standard library's type, and there's no cycle: `log` builds its logger from developer wiring alone, so the logger exists before any config is loaded.

It reports, at Info, which file it read or that the search found none, with the places searched, which answers the question the search raises on every start. At Debug it names each key set from the environment or read through a `_FILE` variable, and at Warn each unknown key it ignored, which turns `AllowUnknownKeys` from silently swallowing typos into flagging them. Records name keys, variables and paths, never values, the same rule errors follow, because config holds secrets. Failures aren't logged: they're the returned error, and logging them as well would report them twice.
