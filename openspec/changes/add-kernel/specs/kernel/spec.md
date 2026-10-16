## Purpose

Turns cobra commands into running apps: a root command with the logging flags, and commands that build each app from those flags, the program's shared modules and their own, with failures reported in the log's format and exit codes intact.

## ADDED Requirements

### Requirement: The root command carries the logging flags

The root command SHALL carry `cli`'s root flags and three persistent flags: `--debug`, `--quiet` (shorthand `-q`) and `--format`. Passing `--debug` and `--quiet` together SHALL fail before any app is built. `--format` SHALL accept `json` and `text`, reject any other value before any app is built, and default to the format the program chose.

#### Scenario: Inherited by a subcommand
- **WHEN** a subcommand runs with `-q`
- **THEN** its app logs from Warn up

#### Scenario: Debug and quiet together
- **WHEN** a command runs with `--debug --quiet`
- **THEN** it fails before building an app

#### Scenario: An unknown format
- **WHEN** a command runs with `--format yaml`
- **THEN** it fails before building an app

#### Scenario: The program's default
- **WHEN** the program chose text and a command runs without `--format`
- **THEN** its app logs text

### Requirement: Each command's app is built from the flags

Running a command SHALL build its app with the logger from the flags (level and format), rendering text with the text handler the program chose, or tint when it chose none. fx's own events SHALL be logged through that logger. The `--config` value SHALL reach the config loader as the file to read. The program's shared options SHALL be part of every command's app, followed by the command's own.

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
- **WHEN** the program's shared options load config and a command runs with `--config ./app.yaml`
- **THEN** the config is read from `./app.yaml`

#### Scenario: Shared and own options
- **WHEN** the program shares a module and a command adds a runner that uses it
- **THEN** the runner gets the shared module's value

### Requirement: A command without the kernel's flags fails

Running a command whose root wasn't built with the kernel's flags SHALL fail with the module's missing-flag error, naming the flag, before any app is built.

#### Scenario: A plain root
- **WHEN** a command under a root built without the kernel's flags is run through the kernel
- **THEN** it fails with an error matching the missing-flag error, naming `debug`

### Requirement: The command shortcut runs a fixed app

A command made with the shortcut SHALL take no arguments and SHALL run the app built from the flags, the shared options and the options it was made with.

#### Scenario: An argument
- **WHEN** a shortcut command is run with an argument
- **THEN** it fails before building an app

### Requirement: slog's default logger writes through the app while it runs

While a command's app is built and running, slog's default logger SHALL write through the app's logger. When the command returns, for any reason, the default logger SHALL be the one in place before it ran.

#### Scenario: During the run
- **WHEN** a runner logs through slog's default logger
- **THEN** the record is written through the app's logger, in its format

#### Scenario: Restored
- **WHEN** a command returns, after running cleanly or after failing at startup
- **THEN** slog's default logger is the one in place before it ran

### Requirement: Failures are reported once, in the log's format, with their exit code

When a command's app fails to start, crashes or fails to stop, the command SHALL NOT print the error itself. The failure SHALL reach the operator through the log, and the error SHALL still be returned, so the exit code is the one it carries. Failures before an app is built, such as flag errors, SHALL be printed as usual.

#### Scenario: A crash
- **WHEN** a command's runner returns an error
- **THEN** nothing is printed to the command's error output, the log holds the failure at Error, and the program exits 1

#### Scenario: A chosen exit code
- **WHEN** a command's runner returns an error carrying exit code 3
- **THEN** the program exits 3

#### Scenario: A startup failure
- **WHEN** a constructor in a command's app fails
- **THEN** nothing is printed to the command's error output, and the log holds the failure at Error

#### Scenario: A flag error
- **WHEN** a command runs with an unknown flag
- **THEN** the error is printed to the command's error output
