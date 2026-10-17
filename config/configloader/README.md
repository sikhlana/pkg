# configloader

Reads an application's configuration from typed defaults, a config file and the environment into its config struct, with [viper](https://github.com/spf13/viper), so every field is settable from the file or the environment and secrets can come from files. The result is validated with [config/configvalidator](../configvalidator) before it's returned, so a misconfiguration stops the program at startup with errors naming the keys.

## Configuration

None of its own. `configloader` loads the app's Config; what it reads that Config from (the app's name, an explicit file, extra search directories, defaults) is developer wiring, passed in `Options` and, under fx, through `WithFilename`.

## Exported Members

| Name | Description |
|------|-------------|
| Load | Reads a T from the defaults, then the config file, then the environment, and validates it. Returns the zero T and an error on any failure. |
| Options | Says where Load reads a T from. Generic over the config type, so T is inferred from the literal. |
| Options.Name | Required. Sets the search directories and the env prefix. Only ASCII letters, digits, - and _, and not starting with a digit. |
| Options.Filename | The config file to read, which must exist. It's the only file read. Empty means search. |
| Options.SearchPaths | Directories searched for config.ext, in order, before any other place. |
| Options.Defaults | A T holding the value of every key neither the file nor the environment sets. The zero value means none. Load copies what it takes, so the loaded config shares nothing with it. |
| Options.AllowUnknownKeys | Ignores, and logs at Warn, file keys that match no field, instead of failing with ErrUnknownKeys. |
| Options.Logger | Where loading is reported. Nil reports nothing. |
| Provide | An fx option that loads a T once, with Load, and provides it. A T embedding fx.Out provides each of its fields instead. |
| Params | What Provide's constructor takes from the graph. |
| Params.Filename | The config file supplied with AsFilename or WithFilename. Wins over Options.Filename; empty falls back to it. |
| Params.Logger | The app's *slog.Logger, optional. Wins over Options.Logger. |
| AsFilename | Annotates a string, or a constructor returning one, as the config file Provide reads. Wrap it in fx.Supply or fx.Provide. |
| WithFilename | Supplies the config file Provide reads, such as a --config value. |
| ErrInvalidName | Options.Name is empty, starts with a digit, or holds a character other than ASCII letters, digits, - and _. |
| ErrEnvNameCollision | Two keys would share an env name, or one key's env name is another's _FILE name. Names both keys. |
| ErrMalformedFile | The config file can't be parsed. Names the file, its format and the position the parser reports, never the parser's message. |
| ErrUnknownKeys | The file sets keys that match no field. Lists them all, in full and sorted. |
| ErrEnvAndFileSet | Both a key's env variable and its _FILE variable are non-empty. Names both variables. |
| ErrSecretFileTooLarge | A _FILE variable names a file larger than 1 MiB. Names the variable and the path, never the contents. |

## Usage

### Standalone

Pass `Load` the app's name and, optionally, its defaults. T is inferred from the `Options` literal:

```go
type Config struct {
	HTTP      HTTPConfig          `mapstructure:"http"`
	Hosts     []string            `mapstructure:"hosts"`
	Databases map[string]DBConfig `mapstructure:"databases" validate:"dive"`
	Log       LogConfig           `mapstructure:"log"`
}

type HTTPConfig struct {
	Port        int           `mapstructure:"port"         validate:"min=1024"`
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
}

type DBConfig struct {
	Host     string `mapstructure:"host"     validate:"required"`
	Password string `mapstructure:"password"`
}

type LogConfig struct {
	Level slog.Level `mapstructure:"level"`
}

cfg, err := configloader.Load(configloader.Options[Config]{
	Name:     "my-svc",
	Filename: path, // empty means search
	Defaults: Config{HTTP: HTTPConfig{Port: 8080, ReadTimeout: 5 * time.Second}},
	Logger:   logger,
})
if err != nil {
	return fmt.Errorf("loading config: %w", err)
}
```

#### Layers

Each layer overrides the one before for the keys it sets: `Defaults`, then the config file, then the environment. A key no layer sets keeps its zero value. A slice is replaced as a whole by the highest layer that sets it, so a file's `hosts: [x]` replaces default hosts `a, b, c` rather than merging with them. A map merges entry by entry, so default `labels.team` survives a file that sets only `labels.zone`. `Load` copies each default it takes, so changing the loaded config never changes `Defaults`, and a reused `Options` loads the same every time. Leaves under a nil pointer in `Defaults` get no default, so a nil section stays nil unless the file or the environment sets something in it. The exception is a squashed pointer embed, which mapstructure always allocates. An unexported embedded pointer can't be decoded at all, so `Load` rejects a config type that squashes one.

#### The config file

An explicit `Filename` must exist, and its extension chooses the format. Errors name the path, and a missing file matches `fs.ErrNotExist`.

A file that can't be parsed fails with `ErrMalformedFile`, naming its format and, where the parser reports one, the position: `malformed config file: toml syntax error at line 3, column 9`. The parser's own message is dropped, since parsers quote the offending text, and a typo beside a secret would put the secret in the error. A file whose extension viper accepts but can't decode, such as `config.ini`, fails saying so.

Without one, `Load` searches, most specific place first, and reads the first file it finds:

1. `config.<ext>` in each directory of `SearchPaths`, in order;
2. `.my-svc.<ext>` in the working directory;
3. `config.<ext>` in `$HOME/.config/my-svc/`, then `$HOME/.my-svc/`, then `/etc/my-svc/`.

`<ext>` is any format viper can decode, such as `yaml`, `toml` or `json`, tried in turn in each place. A file in a format it can't decode, such as `config.ini`, is passed over. The working directory's file is named after the app, so a project's unrelated `config.yaml` is never picked up. Finding none is fine: the config then comes from the defaults and the environment. A file it finds but can't parse fails the load the same way, naming its full path.

#### Environment

Every leaf has an env name: the app's name uppercased with `-` replaced by `_`, then `_`, then the key uppercased with every character other than ASCII letters, digits and `_` replaced by `_`. Under `my-svc`, `http.read_timeout` is `MY_SVC_HTTP_READ_TIMEOUT`. A leaf is any field that isn't a struct or a map, and any type that decodes itself from text, such as `time.Time`, `netip.Prefix` or `slog.Level`. Every leaf is settable this way, whether or not the defaults or the file mention it.

Names that normalise alike, such as `my-svc` and `my_svc`, share a prefix, so two loads in one process need names that don't.

Only those names, and their `_FILE` twins, are read. A variable named after a section, such as `MY_SVC_HTTP`, changes nothing, and neither does a misspelt one.

Two keys that would share an env name fail the load with `ErrEnvNameCollision`, naming both: `http.read_timeout` beside `http_read.timeout`, or `tls.key` beside `tls.key_file`, whose env name is `tls.key`'s `_FILE` twin. Name a field that holds a file the module reads itself `*_file`, with no `x` beside it, and a path used as a path `*_path`.

Values decode by type wherever they come from: `30s` into a `time.Duration`, `a, b` into a slice with the spaces around each element trimmed, and a text-decoded type from its text form. A `[]byte` takes the string's bytes as they are, commas included, so a key can come from a `_FILE` secret. A value that fails to decode fails the load, naming the key and the type, never the value.

#### Secrets from files

`MY_SVC_DB_PASSWORD_FILE` names a file whose contents become `db.password`'s value, at environment priority, so it beats the config file. The file is read once, during the load, with trailing `\r` and `\n` removed and nothing else changed. It must be no larger than 1 MiB. Setting both `MY_SVC_DB_PASSWORD` and `MY_SVC_DB_PASSWORD_FILE` fails with `ErrEnvAndFileSet`; an empty `_FILE` variable counts as unset. The secret never goes into the process environment, and no error contains it.

The file isn't re-read, so a credential that rotates while the program runs belongs in a `*_file` field that the module re-reads itself.

#### Map entries

A map field's entries come from the file and the defaults. Each entry's leaves get env names under the map's key and the entry's, so `MY_SVC_DATABASES_REPORTING_PASSWORD` sets the password of a `reporting` entry the file defines, even when the file's entry sets only `host`. An entry neither the file nor the defaults define can't be created from the environment.

Viper lowercases keys, so map entries come back lowercased, the default ones too: a file's `databases.Reporting` loads as `databases["reporting"]`, and so does a default entry named `Reporting`. Entries whose names normalise to the same env name, such as `acme-corp` and `acme_corp`, fail with `ErrEnvNameCollision`, and so do default entries that differ only in case. A default entry is named by its key's text form when its type has one, and otherwise by its underlying value, never by a `String` method written for display, as `configvalidator` names entries. An entry name can't hold a dot, which viper reads as nesting, or be empty, so `Load` rejects such a default entry.

A config type that contains itself, such as a linked node with a `*Node` field, loads from the file, but only its first level gets env names.

#### Unknown keys

A key in the file that matches no field fails the load with `ErrUnknownKeys`, listing every such key in full, so a typo such as `htpp.port` can't silently leave `http.port` at its default. Map entries never count as unknown, nor does anything beneath a field of interface type or at the level of a `,remain` field. `AllowUnknownKeys` ignores them instead, logging each at Warn.

#### Validation

The decoded config goes through `configvalidator.Validate`, and every violation comes back wrapped as `validating config: …`, each still reachable with `errors.AsType[*configvalidator.Error]`. On any failure `Load` returns the zero value, never a partly loaded config.

#### What gets logged

| When | Level | Message | Attributes |
|------|-------|---------|------------|
| A config file is read, given or found | Info | loaded config file | path |
| The search finds no file | Info | no config file found | searched |
| A key is set from its env variable | Debug | config key set from environment | key, env |
| A key is read through its _FILE variable | Debug | config key read from file | key, env, path |
| An unknown key is ignored | Warn | ignoring unknown config key | key |

Records name keys, env variables and paths, never a value. Failures are returned, not logged. Without a logger, `Load` loads the same way and reports nothing.

### With fx

`Provide` loads the config once, when something in the graph needs it, and provides it. A program built with [kernel](../../kernel) returns it from `kernel.Options.Config`, passing `--config`'s path as `Options.Filename`; the kernel lists `log.Module` ahead of it, so the loader reports through the app's logger:

```go
app := kernel.New(kernel.Options{
	Config: func(path string) fx.Option {
		return configloader.Provide(configloader.Options[Config]{
			Name:     "my-svc",
			Filename: path,
			Defaults: Config{HTTP: HTTPConfig{Port: 8080}},
		})
	},
})
```

A program without `kernel` defines the flag itself and passes its value through `WithFilename`, listing `log.Module` for the same reason:

```go
root := cli.NewRootCommand("my-svc", "My service")
root.PersistentFlags().String("config", "", "path to the config `file`")
root.RunE = func(cmd *cobra.Command, _ []string) error {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}

	app := fx.New(
		log.Module,
		configloader.Provide(configloader.Options[Config]{
			Name:     "my-svc",
			Defaults: Config{HTTP: HTTPConfig{Port: 8080}},
		}),
		configloader.WithFilename(path),
		fx.Invoke(func(cfg Config) { /* ... */ }),
	)

	return app.Err()
}
```

The file comes from `WithFilename`, then `Options.Filename`, then the search; an empty `--config` falls through. A failed load fails the app's start with the load error. `log.Module` builds its logger from developer wiring alone, so it exists before the config is loaded.

#### Handing sections to modules

Embed `fx.Out` in the config type and fx provides each field instead of the whole, so each module's Config reaches it without an adapter:

```go
type Config struct {
	fx.Out

	HTTP  httpserver.Config `mapstructure:"http"`
	Audit audit.Config      `mapstructure:"audit"`
}
```

`fx.Out` contributes no keys and no env names. Two fields of the same type need `name` tags.
