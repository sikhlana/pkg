# configvalidator

Checks a configuration struct with its `validate` tags and its own `Validate` methods, reporting every violation at once under the config key an operator would write, such as `http.port`, and never the value.

## Configuration

None. `configvalidator` checks other modules' Configs, and the loader's, and has nothing for an operator to set.

## Exported Members

| Name | Description |
|------|-------------|
| Validate | Checks a config, through a pointer, and returns every violation as an Errors, or nil. A nil config returns ErrNilConfig. |
| ValidateField | Validates a field beneath a parent, both through pointers, and returns each violation under the field's key within the parent, as an Errors, or nil. A nil field returns ErrNilConfig. Panics when the parent is nil or the field isn't beneath it. |
| Validator | A config type with its own checks, beyond what its tags can say. |
| Validator.Validate | Returns what's wrong with the value, or nil. Validate calls it on the outermost Validator in each branch. |
| Errors | Every violation Validate found, each an Error: failed tags in the order of their keys, then what Validate methods returned. A slice, so it ranges directly. |
| Errors.Error | Returns each violation's message on its own line, skipping nil entries. |
| Errors.Unwrap | Returns the violations, so errors.Is and errors.AsType check each one. |
| Error | One violation: a config key and what's wrong there. |
| Error.Key | The dotted config key, such as http.port, hosts[2] or databases.reporting, or empty for the config as a whole. |
| Error.Err | What's wrong: a RuleError for a failed tag, or what a Validator returned. |
| Error.Error | Returns key: cause, or the cause alone when Key is empty. A nil Err reads as invalid. |
| Error.Unwrap | Returns Err. |
| RuleError | The cause of a failed validate tag. Names the rule and never the value. |
| RuleError.Tag | The rule that failed, such as min or oneof. |
| RuleError.Param | The rule's parameter, such as 1024 or read write, or empty. |
| RuleError.Error | Returns fails tag=param, or fails tag when there's no parameter. |
| ErrNilConfig | Returned by Validate for a nil config, and by ValidateField for a nil field, under the field's key when the parent holds exactly one nil pointer of that type. |
| KeySegment | Returns the key segment a struct field adds, and false when it has no key. The key rules every Error follows. |
| DecodesText | Reports whether a type, or a pointer to it, implements encoding.TextUnmarshaler, making it a single config value. |

## Usage

### Standalone

Tag a Config with [go-playground/validator](https://pkg.go.dev/github.com/go-playground/validator/v10) rules and pass a pointer to it to `Validate`:

```go
type Config struct {
	HTTP HTTPConfig `mapstructure:"http"`
	Log  LogConfig  `mapstructure:"log"`
}

type HTTPConfig struct {
	Host string `mapstructure:"host" validate:"required,hostname"`
	Port int    `mapstructure:"port" validate:"min=1024,max=65535"`
}

type LogConfig struct {
	Format string `mapstructure:"format" validate:"oneof=text json"`
}

cfg := Config{
	HTTP: HTTPConfig{Host: "localhost", Port: 80},
	Log:  LogConfig{Format: "xml"},
}

if err := configvalidator.Validate(&cfg); err != nil {
	return fmt.Errorf("validating config: %w", err)
}
```

Every violation is reported, one per line:

```text
validating config: http.port: fails min=1024
log.format: fails oneof=text json
```

Keys follow the mapstructure tags, so they match what an operator writes in a config file. An untagged field uses its Go name, a squashed field adds no segment, a slice element is `hosts[2]` and a map value is `databases.reporting`. A map entry is named by its key's text form, or its underlying value, never a `String` method written for display, and it's kept whole even when it holds dots or brackets: `tenants.acme.eu`, `hosts.[::1]:5432`. A failed tag names the rule and its parameter, never the value, since config values include secrets. Tag violations come in the order of their keys, so the same config reports the same order on every run.

`Validate` returns a `configvalidator.Errors`, a slice holding one `*configvalidator.Error` per violation, even when there's only one. `errors.AsType` finds it through any wrapping:

```go
if errs, ok := errors.AsType[configvalidator.Errors](err); ok {
	for _, violation := range errs {
		v, _ := errors.AsType[*configvalidator.Error](violation)
		fmt.Println(v.Key)
	}
}
```

`errors.Is` and `errors.AsType` also look inside each violation, so `errors.AsType[*configvalidator.Error]` finds the first one, and `errors.AsType[*configvalidator.RuleError]` tells a failed tag apart from a failed `Validate` method.

#### Validate methods

Checks that tags can't express go in a `Validate() error` method. Return an exported sentinel so callers can match it with `errors.Is`, and an `*Error` to name the key it's about:

```go
var ErrMissingCert = errors.New("cert_path is required when ssl is enabled")

type SSLConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertPath string `mapstructure:"cert_path"`
}

func (c SSLConfig) Validate() error {
	if c.Enabled && c.CertPath == "" {
		return &configvalidator.Error{Key: "cert_path", Err: ErrMissingCert}
	}

	return nil
}
```

Under `http.ssl`, that reports `http.ssl.cert_path: cert_path is required when ssl is enabled`.

`Validate` calls the outermost `Validate` method in each branch of the config, with a value or a pointer receiver, and doesn't look for others beneath it: that method owns its branch. It isn't called when a tag failed on it or anything beneath it, since its checks may rely on what the tags guarantee. Methods in other branches still run. A method must not pass its own receiver to `Validate`, which would call it again.

#### Modes

A Config that picks one of several modes marks each mode's struct `validate:"-"`, so the tag pass never reaches a dormant one and its `required` rules are safe. Its `Validate` method checks the active one with `ValidateField`, passing its receiver and the field:

```go
type MessagingConfig struct {
	Mode string     `mapstructure:"mode" validate:"oneof=file amqp"`
	File FileConfig `mapstructure:"file" validate:"-"`
	AMQP AMQPConfig `mapstructure:"amqp" validate:"-"`
}

type AMQPConfig struct {
	URL   string `mapstructure:"url"   validate:"required,url"`
	Queue string `mapstructure:"queue" validate:"required"`
}

func (c MessagingConfig) Validate() error {
	switch c.Mode {
	case "amqp":
		return configvalidator.ValidateField(&c, &c.AMQP)
	default:
		return configvalidator.ValidateField(&c, &c.File)
	}
}
```

With mode `amqp` and no URL, that reports `messaging.amqp.url: fails required`. `ValidateField` adds the field's key, `amqp`, and the walk adds the method's, `messaging`. `Validate(&c.AMQP)` would report `messaging.url` instead. An unknown mode fails its `oneof` rule, so the method isn't called for one.

Return `ValidateField`'s result directly, or combine several with `errors.Join`, which drops the nil ones. Keys compose through `*Error` values, `Errors` and `errors.Join` only. Any other error, including `fmt.Errorf` with several `%w` verbs, keeps its text and is reported whole at the method's key. A nil `*Error` is no violation. `ValidateField` panics when the field isn't beneath the receiver: that's a bug in the method, found by the first test of that mode.

An optional section can be a pointer, nil when it's missing. Passing it as it is, `ValidateField(&c, c.AMQP)`, reports `messaging.amqp: config is nil` for a missing one, as long as the receiver holds no other nil `*AMQPConfig`. `ValidateField` also finds a field behind a map of pointers, so `ValidateField(&c, c.Databases[c.Active])` reports keys such as `databases.main.url`.

#### Slices and maps

Tags on a slice or map field apply to the field itself. Rules for the elements, including the tags inside struct elements, need `dive`:

```go
type Config struct {
	Hosts     []string            `mapstructure:"hosts"     validate:"min=1,dive,hostname"`
	Databases map[string]DBConfig `mapstructure:"databases" validate:"dive"`
}
```

Without `dive`, a bad host or a database missing a required field goes unreported. `Validate` methods on elements are called either way, each under its element's key.

A slice, array or map passed to `Validate` or `ValidateField` itself is different: it has no fields of its own, so each element's tags are checked as if it were validated alone. `ValidateField(&c, &c.Brokers)` reports `brokers[0].url` without `dive`.

#### Embedding a Validator

A struct that embeds a type with a `Validate` method gains that method, so it becomes a Validator itself:

```go
type ServerConfig struct {
	SSLConfig `mapstructure:",squash"`

	Limits LimitsConfig `mapstructure:"limits"`
}
```

Here `SSLConfig.Validate` is called for the whole `ServerConfig`, and `LimitsConfig.Validate` never is. Tags still run on every field. Give `ServerConfig` its own `Validate` method that checks both, or make the embedded struct a named field.

Embedded by pointer, as `*SSLConfig`, the section is missing when the pointer is nil, and its method has nothing to run on. `ServerConfig` then isn't treated as a Validator: the walk carries on into its fields, so `LimitsConfig.Validate` is called. Reflection can't tell that promoted method from one `ServerConfig` declares itself, so don't declare `Validate` on a type that embeds a pointer to another Validator: it's skipped whenever that pointer is nil.

An unexported embedded struct is walked like an exported one, since its exported fields are promoted into the parent, and squashing it adds no segment to their keys.

#### Text values

A type that decodes itself from text, such as `time.Time`, `netip.Prefix` or `slog.Level`, is a single value. Its own `Validate` method is called, and nothing inside it is searched. `KeySegment` and `DecodesText` expose these key rules for code that needs the same keys, as `config/configloader` does for env names.

### With fx

`configvalidator` doesn't import fx. Config is validated once, where it's loaded, and fx-wired constructors trust what they're given. A Provider closure builds clients from Configs it's handed at run time, so it validates each one:

```go
type ClientProvider func(Config) (*Client, error)

func NewClientProvider(p Params) ClientProvider {
	return func(cfg Config) (*Client, error) {
		if err := configvalidator.Validate(&cfg); err != nil {
			return nil, fmt.Errorf("validating client config: %w", err)
		}

		return newClient(cfg, p.Logger), nil
	}
}
```
