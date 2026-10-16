## Purpose

Gives a Cobra program's `main` what every command-line binary needs: shutdown on a signal, exit codes that commands return instead of calling os.Exit, consistent root and version commands, and version information that works with or without link-time flags.

## ADDED Requirements

### Requirement: The first signal cancels, a second one terminates

The signal-aware context SHALL be cancelled when the process receives SIGINT or SIGTERM, when its parent is done, or when its stop function is called, whichever comes first. When a signal cancels it, its cause SHALL identify the signal. Once it's cancelled, the module SHALL stop diverting those signals, so a second SIGINT or SIGTERM gets the operating system's default behaviour and ends the process, unless something else in the process also subscribes to it.

#### Scenario: Starts live
- **WHEN** a caller creates the context and no signal has arrived
- **THEN** the context is not done

#### Scenario: First signal cancels with a cause
- **WHEN** the process receives SIGINT
- **THEN** the context is done, the program keeps running, and the context's cause names the interrupt signal

#### Scenario: Second signal terminates
- **WHEN** the process receives SIGINT, and then SIGINT again while still shutting down
- **THEN** the process is terminated by the second signal

#### Scenario: Parent cancellation propagates
- **WHEN** the parent context is cancelled
- **THEN** the signal-aware context is done too

### Requirement: Execute turns a command's error into an exit code

Execute SHALL run the root command with the caller's context, so every command observes that context's values and cancellation. It SHALL return 0 when the command succeeds. When the error chain contains an exit error, it SHALL return that error's code, or 1 if the code is zero or negative. It SHALL return 1 for any other error.

#### Scenario: Success
- **WHEN** the command returns no error
- **THEN** Execute returns 0

#### Scenario: Plain error
- **WHEN** the command returns an error that carries no exit code
- **THEN** Execute returns 1

#### Scenario: Wrapped exit code
- **WHEN** the command returns an error wrapping an exit error with code 7
- **THEN** Execute returns 7

#### Scenario: Zero code can't report success
- **WHEN** the command returns an exit error with code 0
- **THEN** Execute returns 1

#### Scenario: Context reaches commands
- **WHEN** the caller passes a context carrying a value
- **THEN** the command observes that value through its context

### Requirement: Exit errors wrap an optional cause

An exit error SHALL carry a code and an optional underlying error. Its message SHALL be the underlying error's message, or `exit status N` when there's no underlying error. Error inspection SHALL reach the underlying error and find the exit error anywhere in a wrapping chain. Building or printing an exit error without an underlying error SHALL NOT panic.

#### Scenario: Cause is reachable
- **WHEN** a command returns an exit error with code 3 wrapping cause C
- **THEN** errors.Is reports C in the chain, and the message is C's message

#### Scenario: No cause
- **WHEN** an exit error with code 2 has no underlying error
- **THEN** its message is `exit status 2`

### Requirement: Failures print the error, never the usage text

A root command built by the module SHALL print a failing command's error once on standard error, and SHALL NOT print usage text for any failure, flag errors included. A command that silences its errors through Cobra before returning SHALL exit with its code and print nothing. Asking for `--help` SHALL still print help.

#### Scenario: Runtime failure
- **WHEN** a subcommand returns an error
- **THEN** the error is printed once on standard error and no usage text is printed

#### Scenario: Quiet exit
- **WHEN** a subcommand silences its errors and returns an exit error with code 1 and no underlying error
- **THEN** nothing is printed and Execute returns 1

### Requirement: Every command accepts --config

A root command built by the module SHALL accept a string flag `--config`, shorthand `-c`, defaulting to empty, on itself and every subcommand.

#### Scenario: Subcommand reads the flag
- **WHEN** a program runs `myapp serve -c /etc/myapp.yaml`
- **THEN** the serve command reads `/etc/myapp.yaml` as the config flag's value

### Requirement: One version line

A root command built by the module SHALL accept `--version`, and the module's version subcommand SHALL print the same single line. The line SHALL be the root command's display name and the version, followed by a parenthesised, comma-separated list of the revision, the time, and the word `modified`, each included only when known or true. The parentheses SHALL be omitted when none of them is.

#### Scenario: Flag and subcommand agree
- **WHEN** a user runs `myapp --version` and then `myapp version`
- **THEN** both print the same line

#### Scenario: Full details
- **WHEN** the version is `v1.2.3`, the revision `abc123`, the time `2026-10-16T09:00:00Z`, and the tree was modified
- **THEN** the line is `myapp v1.2.3 (abc123, 2026-10-16T09:00:00Z, modified)`

#### Scenario: Nothing known
- **WHEN** the binary was built with `-buildvcs=false` and no link-time values
- **THEN** the line is `myapp dev`

### Requirement: Build info prefers link-time values, then what the go command stamped

The module SHALL resolve the version, revision and time independently. Each SHALL be the value set at link time (`-ldflags -X`) when non-empty. Failing that, it SHALL be the value the go command stamped into the binary: the main module's version unless it's `(devel)`, the VCS revision, and the VCS commit time. Failing that, the version SHALL be `dev`, and the revision and time SHALL be unknown. Whether the tree was modified SHALL come only from the stamped VCS data.

#### Scenario: Plain build from a tagged checkout
- **WHEN** a program is built with `go build` and no link-time values from a clean checkout tagged `v1.2.3`
- **THEN** the version is `v1.2.3`, the revision is the checked-out commit and the time is its commit time

#### Scenario: Link-time values win
- **WHEN** the version is set at link time to `1.0.0-rc1` and the go command also stamped `v1.2.3`
- **THEN** the version is `1.0.0-rc1`, and the revision and time still come from the stamped data

#### Scenario: No VCS data
- **WHEN** a program is built with `-buildvcs=false` and the version alone is set at link time to `1.0.0`
- **THEN** the version is `1.0.0`, the revision and time are unknown, and the tree isn't reported as modified
