## Purpose

Reads an application's configuration from typed defaults, a config file and the environment into its config struct, so every field is settable from the file or the environment, secrets can come from files, and a misconfiguration stops the program at startup with errors naming the keys.

## ADDED Requirements

### Requirement: Defaults, then the file, then the environment

The loader SHALL build the config from three layers, each overriding the one before for the keys it sets: the caller's defaults, the config file, and the environment. A key no layer sets SHALL keep its zero value. A slice SHALL be replaced as a whole by the highest layer that sets it. A map's entries SHALL merge across layers, entry by entry.

#### Scenario: Default only
- **WHEN** the defaults set `http.port` to 8080 and neither the file nor the environment sets it
- **THEN** the loaded `http.port` is 8080

#### Scenario: File over default
- **WHEN** the defaults set `http.port` to 8080 and the file sets it to 9090
- **THEN** the loaded `http.port` is 9090

#### Scenario: Environment over file
- **WHEN** the file sets `http.port` to 9090 and the environment sets it to 9191
- **THEN** the loaded `http.port` is 9191

#### Scenario: Slice is replaced
- **WHEN** the defaults set `hosts` to a, b and c and the file sets it to x
- **THEN** the loaded `hosts` holds only x

#### Scenario: Map entries merge
- **WHEN** the defaults set `labels.team` and `labels.tier` and the file sets `labels.tier` and `labels.zone`
- **THEN** the loaded `labels` has `team` from the defaults, and `tier` and `zone` from the file

### Requirement: An explicit config file must exist

When the caller supplies a config file path, the loader SHALL read that file, with its extension choosing the format, and SHALL fail, naming the path, when the file is missing, unreadable or malformed, or its extension names no supported format. A file that can't be parsed SHALL fail naming its format and, where the parser reports one, the position, and the error SHALL NOT quote the file's contents.

#### Scenario: Missing file
- **WHEN** the supplied path doesn't exist
- **THEN** the load fails with an error naming the path

#### Scenario: Malformed file
- **WHEN** the supplied file has a syntax error
- **THEN** the load fails with an error naming the path

#### Scenario: Malformed file isn't quoted
- **WHEN** the supplied file has a syntax error on the line holding `password = "hunter2"`
- **THEN** the load fails with an error naming the path, the format and the line, and the error doesn't contain "hunter2"

#### Scenario: Unsupported extension
- **WHEN** the supplied path ends in an extension no supported format uses
- **THEN** the load fails with an error naming the path

### Requirement: Without one, a config file is searched for

Without an explicit path, the loader SHALL search, most specific place first, and SHALL read the first file it finds: `config.<ext>` in each of the caller's extra search directories, in the order given; then `.<name>.<ext>` in the working directory; then `config.<ext>` in `$HOME/.config/<name>/`, `$HOME/.<name>/` and `/etc/<name>/`. Each place SHALL be tried with every format the loader can decode, and a file in any other format SHALL be passed over. An explicit path SHALL be the only file read. Finding none SHALL NOT be an error; the config then comes from the defaults and the environment. A file it finds but can't read or parse SHALL fail the load, with an error naming that file's path.

#### Scenario: Extra directory comes first
- **WHEN** the caller adds a search directory containing `config.toml` and the working directory holds `.my-svc.toml`
- **THEN** the loader reads the file in the caller's directory

#### Scenario: Working directory's file is named after the app
- **WHEN** the working directory holds `.my-svc.toml` and nothing else is found
- **THEN** the loader reads it

#### Scenario: Generic file in the working directory
- **WHEN** the working directory holds only `config.toml`
- **THEN** no file is read

#### Scenario: Most specific place wins
- **WHEN** `.my-svc.yaml` in the working directory, `$HOME/.config/my-svc/config.yaml` and `$HOME/.my-svc/config.yaml` all exist
- **THEN** the loader reads the working directory's file; without it, the one in `$HOME/.config/my-svc/`; without that, the one in `$HOME/.my-svc/`

#### Scenario: Format without a decoder
- **WHEN** `$HOME/.config/my-svc/config.ini` and `$HOME/.my-svc/config.yaml` exist
- **THEN** the loader passes over the `.ini` file and reads the YAML one

#### Scenario: Explicit path alone
- **WHEN** the caller supplies a config file path and the working directory also holds `.my-svc.yaml`
- **THEN** only the supplied file is read

#### Scenario: No file anywhere
- **WHEN** no search directory has a config file and the environment sets `http.port`
- **THEN** the load succeeds with `http.port` from the environment

#### Scenario: Found file is malformed
- **WHEN** the search finds a config file with a syntax error
- **THEN** the load fails with an error naming that file's full path

### Requirement: The name sets the environment prefix

The loader SHALL fail when the name is empty, starts with a digit, or contains anything other than ASCII letters, digits, `-` and `_`. A key's env name SHALL be the name, uppercased with `-` replaced by `_`, followed by `_` and the key, uppercased with every character other than ASCII letters, digits and `_` replaced by `_`.

#### Scenario: Env name of a nested key
- **WHEN** the name is `my-svc` and the key is `http.read_timeout`
- **THEN** the key's env name is `MY_SVC_HTTP_READ_TIMEOUT`

#### Scenario: Empty name
- **WHEN** the name is empty
- **THEN** the load fails

#### Scenario: Name starting with a digit
- **WHEN** the name is `1password-sync`
- **THEN** the load fails, since no shell can export the env names it would give

#### Scenario: Name with a path separator
- **WHEN** the name is `../etc`
- **THEN** the load fails

### Requirement: Every key is settable from the environment

Every leaf the config struct declares SHALL be settable through its env name, whether or not the defaults or the file mention it. A leaf is a field whose type is neither a struct nor a map, or a field whose type decodes itself from text. Nested structs and pointers to structs SHALL contribute their leaves under their own key, and squashed embedded structs under their parent's key. Unexported fields and fields excluded from decoding SHALL contribute none. A map field SHALL contribute, for each entry the file or the defaults define, the leaves of the map's value type under the map's key and the entry's key, or the entry itself when the value type is a leaf type. An entry that neither the file nor the defaults define SHALL NOT be created from the environment.

#### Scenario: Nested leaf with no default or file entry
- **WHEN** neither the defaults nor the file mention `cache.ttl` and `MY_SVC_CACHE_TTL` is `60s`
- **THEN** the loaded `cache.ttl` is 60 seconds

#### Scenario: Squashed leaf binds at the parent's key
- **WHEN** a struct with a `host` field is squashed into the `broadcast.http` section and `MY_SVC_BROADCAST_HTTP_HOST` is set
- **THEN** the loaded host is that value

#### Scenario: Text-decoded leaf
- **WHEN** a `time.Time` field `cutoff` and a `netip.Prefix` field `trusted` exist and their env names are set
- **THEN** both fields are loaded from the environment

#### Scenario: Map entry from the file is overridden
- **WHEN** the file defines `databases.reporting.password` and `MY_SVC_DATABASES_REPORTING_PASSWORD` is set
- **THEN** the loaded reporting database's password is the environment's value

#### Scenario: Map entry gains a field the file omits
- **WHEN** the file's `databases.reporting` entry sets only `host` and `MY_SVC_DATABASES_REPORTING_PASSWORD` is set
- **THEN** the loaded reporting database has the file's host and the environment's password

#### Scenario: Map entry can't be created from the environment
- **WHEN** neither the file nor the defaults define a `databases.audit` entry and `MY_SVC_DATABASES_AUDIT_PASSWORD` is set
- **THEN** the loaded `databases` map has no `audit` entry

### Requirement: Only known env names are read

The loader SHALL read no environment variable other than the env names of known keys and their `_FILE` names. A variable named after a section SHALL NOT affect the keys beneath it.

#### Scenario: Section-named variable
- **WHEN** the file sets `http.port` to 9090 and `MY_SVC_HTTP` is set
- **THEN** the loaded `http.port` is 9090

#### Scenario: Unknown prefixed variable
- **WHEN** `MY_SVC_HTTP_PROT` is set and no key has that env name
- **THEN** the load is unaffected

### Requirement: Env names never collide

When two keys would share an env name, or one key's env name equals another key's `_FILE` name, the load SHALL fail with an error naming both keys. This SHALL apply to map entries from the file and the defaults as well as to struct leaves.

#### Scenario: Key beside its file twin
- **WHEN** the config declares both `tls.key` and `tls.key_file`
- **THEN** the load fails naming both keys

#### Scenario: Underscore ambiguity
- **WHEN** the config declares both `http.read_timeout` and `http_read.timeout`
- **THEN** the load fails naming both keys

#### Scenario: Map entries that normalise alike
- **WHEN** the file defines map entries `tenants.acme-corp` and `tenants.acme_corp` that both have a `url`
- **THEN** the load fails naming both keys

### Requirement: Secrets can come from files

For every key, when its env name followed by `_FILE` is set to a non-empty path, the loader SHALL use the contents of that file as the key's environment value, with trailing `\n` and `\r` characters removed and nothing else changed. The file SHALL be read once, during the load. A `_FILE` variable set to the empty string SHALL count as unset. When both a key's env variable and its `_FILE` variable are non-empty, the load SHALL fail naming both variables. When the file can't be read or is larger than 1 MiB, the load SHALL fail naming the variable and the path. No error SHALL contain the file's contents.

#### Scenario: Trailing newline is stripped
- **WHEN** `MY_SVC_DB_PASSWORD_FILE` names a file containing "s3cret\n"
- **THEN** the loaded `db.password` is "s3cret"

#### Scenario: Other whitespace is kept
- **WHEN** the file contains " pass word \r\n"
- **THEN** the loaded value is " pass word "

#### Scenario: File over the config file
- **WHEN** the config file sets `db.password` and `MY_SVC_DB_PASSWORD_FILE` names a file
- **THEN** the loaded `db.password` is the file's contents

#### Scenario: Non-string field
- **WHEN** `MY_SVC_HTTP_PORT_FILE` names a file containing "8080\n"
- **THEN** the loaded `http.port` is 8080

#### Scenario: Both set
- **WHEN** `MY_SVC_DB_PASSWORD` and `MY_SVC_DB_PASSWORD_FILE` are both non-empty
- **THEN** the load fails naming both variables

#### Scenario: Empty file variable
- **WHEN** `MY_SVC_DB_PASSWORD_FILE` is set to the empty string and `MY_SVC_DB_PASSWORD` is set
- **THEN** the loaded `db.password` is the value of `MY_SVC_DB_PASSWORD`

#### Scenario: Missing file
- **WHEN** `MY_SVC_DB_PASSWORD_FILE` names a path that doesn't exist
- **THEN** the load fails naming the variable and the path

#### Scenario: Oversized file
- **WHEN** `MY_SVC_DB_PASSWORD_FILE` names a file larger than 1 MiB
- **THEN** the load fails naming the variable and the path, and the error contains none of the file's contents

### Requirement: Unknown keys in the file fail the load

A key in the config file that matches no field SHALL fail the load, with an error naming it, unless the caller allows unknown keys, in which case it SHALL be ignored. Entries of a map field SHALL never count as unknown.

#### Scenario: Typo
- **WHEN** the file sets `htpp.port` and the config has no `htpp` field
- **THEN** the load fails naming `htpp.port`

#### Scenario: Unknown keys allowed
- **WHEN** the caller allows unknown keys and the file sets `htpp.port`
- **THEN** the load succeeds and the key is ignored

#### Scenario: Map entries
- **WHEN** the file defines any entries under a map field
- **THEN** none of them count as unknown

### Requirement: Values decode by type

Wherever a value comes from, the loader SHALL decode a duration from a string such as `30s`, a slice from a comma-separated string with the spaces around each element trimmed, a byte slice from a string's bytes as they are, and any type that decodes itself from text from its text form. A value that fails to decode SHALL fail the load with an error naming the key and the expected type, and SHALL NOT contain the value.

#### Scenario: Duration
- **WHEN** `MY_SVC_HTTP_READ_TIMEOUT` is `30s`
- **THEN** the loaded read timeout is 30 seconds

#### Scenario: Comma-separated slice
- **WHEN** `MY_SVC_HOSTS` is `a,b`
- **THEN** the loaded `hosts` holds a and b

#### Scenario: Spaces after commas
- **WHEN** `MY_SVC_HOSTS` is `a, b`
- **THEN** the loaded `hosts` holds a and b, without a leading space

#### Scenario: Byte slice from a secret file
- **WHEN** `MY_SVC_KEY_FILE` names a file holding `abc,def` for a `[]byte` field
- **THEN** the loaded key is those seven bytes, unsplit

#### Scenario: Text-decoded types
- **WHEN** the file sets `log.level` to `debug` for a `slog.Level` field and `trusted` to `10.0.0.0/8` for a `netip.Prefix` field
- **THEN** both decode to those values

#### Scenario: Bad number
- **WHEN** `MY_SVC_HTTP_PORT` is "hunter2"
- **THEN** the load fails with an error naming `http.port` and an integer type, and the error doesn't contain "hunter2"

#### Scenario: Bad text-decoded value
- **WHEN** `trusted` is set to "hunter2"
- **THEN** the load fails with an error naming `trusted`, and the error doesn't contain "hunter2"

### Requirement: The loaded config is validated

The loader SHALL validate the decoded config with the shared validation rules before returning it. When the config isn't valid, the load SHALL fail with every violation, each still reachable with its key. A failed load SHALL return the zero value, never a partially loaded config.

#### Scenario: Invalid value
- **WHEN** the loaded `http.port` fails its `min=1024` rule
- **THEN** the load fails with a violation for `http.port`

#### Scenario: Dormant mode struct
- **WHEN** the messaging mode is `file`, its `amqp` field is marked `validate:"-"`, and the AMQP config's `required` url is empty
- **THEN** the load succeeds

#### Scenario: Zero value on failure
- **WHEN** any step of the load fails
- **THEN** the returned config is the zero value

### Requirement: Loading has no side effects

A load SHALL NOT modify the process environment or any state shared with other loads, and the loaded config SHALL share no map, slice or pointer with the caller's defaults. Loads with different names or config types in one process SHALL NOT affect each other.

#### Scenario: Secret stays out of the environment
- **WHEN** `db.password` is loaded through `MY_SVC_DB_PASSWORD_FILE`
- **THEN** `MY_SVC_DB_PASSWORD` is still unset in the process environment afterwards

#### Scenario: Loaded config doesn't alias the defaults
- **WHEN** a load's result has a slice or map from the defaults changed, and the same options are loaded again
- **THEN** the second load returns the defaults unchanged

#### Scenario: Independent loads
- **WHEN** a process loads two config types under two names
- **THEN** each load sees only its own defaults, file and env names

### Requirement: Loading is reported through the caller's logger

The loader SHALL report through the logger it's given, or under fx through the app's logger when the app provides one. It SHALL report at Info which config file it read, or that the search found none, naming the directories it searched. It SHALL report at Debug each key set from its env variable or read through its file variable, naming the key, the variable and, for a file variable, the file's path. It SHALL report at Warn each unknown file key it ignores because unknown keys are allowed, naming the key. No record SHALL contain a config value or a secret file's contents. Failures SHALL be returned, not logged. Without a logger, the loader SHALL load the same way and report nothing.

#### Scenario: File read
- **WHEN** the search finds a config file
- **THEN** an Info record names that file's path

#### Scenario: No file found
- **WHEN** the search finds no config file
- **THEN** an Info record says so and names each place searched

#### Scenario: Key from the environment
- **WHEN** `MY_SVC_HTTP_PORT` sets `http.port` to 9191
- **THEN** a Debug record names `http.port` and `MY_SVC_HTTP_PORT`, and no record contains 9191

#### Scenario: Key from a secret file
- **WHEN** `db.password` is read through `MY_SVC_DB_PASSWORD_FILE`
- **THEN** a Debug record names the key, the variable and the file's path, and no record contains the file's contents

#### Scenario: Ignored unknown key
- **WHEN** unknown keys are allowed and the file sets `htpp.port`
- **THEN** a Warn record names `htpp.port`

#### Scenario: Failure is returned, not logged
- **WHEN** the loaded `http.port` fails its `min=1024` rule
- **THEN** the violation is in the returned error, and no record mentions it

#### Scenario: No logger
- **WHEN** the caller gives no logger
- **THEN** the load behaves the same and writes nothing

#### Scenario: The fx app's logger
- **WHEN** an fx app provides a `*slog.Logger` and loads its config through the loader's provider
- **THEN** the loader reports through that logger

### Requirement: An fx app loads its config once, from the path it supplies

Providing a config type to an fx app SHALL load it once, from the config file path the app supplies as developer wiring when it supplies one, and by searching otherwise. A failed load SHALL fail the app's start with the load error.

#### Scenario: Supplied path
- **WHEN** the app supplies a config file path
- **THEN** the provided config is loaded from that file

#### Scenario: No path
- **WHEN** the app supplies no path
- **THEN** the provided config is loaded by searching

#### Scenario: Failed load
- **WHEN** the load fails
- **THEN** the app fails to start, with the load error
