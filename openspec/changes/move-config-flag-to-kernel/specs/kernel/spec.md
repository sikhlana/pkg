## MODIFIED Requirements

### Requirement: Each command's app is built from the flags

Running a command SHALL build its app with the logger from the flags (level and format), rendering text with the text handler the program chose, or tint when it chose none. fx's own events SHALL be logged through that logger. When the program loads config, its config loading SHALL be part of every command's app, given `--config`'s path, or an empty path when the operator passed none; when it doesn't, nothing of config loading SHALL be part of the app. The program's shared options SHALL be part of every command's app, followed by the command's own.

#### Scenario: The level
- **WHEN** a command runs with `--debug` and its runner logs at Debug
- **THEN** the record is written

#### Scenario: The text handler
- **WHEN** the program chose a text handler and a command runs with `--format text`
- **THEN** its records are rendered by that handler

#### Scenario: Tint by default
- **WHEN** the program chose no text handler and a command runs with `--format text`
- **THEN** its records are rendered by tint

#### Scenario: fx's events
- **WHEN** a command runs with `--debug`
- **THEN** records for fx's provides, invokes and lifecycle hooks are written through the app's logger

#### Scenario: The config file
- **WHEN** the program loads config and a command runs with `--config ./app.yaml`
- **THEN** the program's config loading is given `./app.yaml`, and the config is read from it

#### Scenario: No config file passed
- **WHEN** the program loads config and a command runs without `--config`
- **THEN** the program's config loading is given an empty path

#### Scenario: Config loading that adds nothing
- **WHEN** the program's config loading returns no option for a path
- **THEN** the command's app is built and runs without it

#### Scenario: Shared and own options
- **WHEN** the program shares a module and a command adds a runner that uses it
- **THEN** the runner gets the shared module's value

## ADDED Requirements

### Requirement: --config exists only when the program loads config

The root command SHALL carry a persistent string flag `--config`, without a shorthand and with a value placeholder in help, when the program loads config, and SHALL NOT carry it when the program doesn't. A program that doesn't load config SHALL run its commands without the flag being looked for.

#### Scenario: A program that loads config
- **WHEN** the program loads config and a command's help is printed
- **THEN** it shows `--config` with a value placeholder

#### Scenario: A program that doesn't
- **WHEN** the program loads no config and a command runs with `--config ./app.yaml`
- **THEN** it fails before building an app, naming `--config`

#### Scenario: No flag in help
- **WHEN** the program loads no config and a command's help is printed
- **THEN** it doesn't show `--config`

#### Scenario: Running without config
- **WHEN** the program loads no config and a command runs
- **THEN** its app runs, and fails over no missing flag
