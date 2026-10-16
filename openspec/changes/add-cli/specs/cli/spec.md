## Purpose

Gives a Cobra program's `main` what every command-line binary needs: shutdown on a signal, exit codes that commands return instead of calling os.Exit, consistent root and version commands, and version information that works with or without link-time flags.

## ADDED Requirements

### Requirement: The first signal cancels, a second one terminates

The signal-aware context SHALL be cancelled when the process receives SIGINT or SIGTERM, when its parent is done, or when its stop function is called, whichever comes first. When a signal cancels it, its cause SHALL be an error that carries the signal and matches context.Canceled. The module SHALL stop diverting those signals before the context is marked done, so by the time anything observes the cancellation, a second SIGINT or SIGTERM gets the action the process started with. That action is normally the default one, which ends the process. A signal the process started with ignored SHALL be ignored again. A signal that something else in the process also subscribes to SHALL keep reaching that subscriber.

#### Scenario: Starts live
- **WHEN** a caller creates the context and no signal has arrived
- **THEN** the context is not done

#### Scenario: First signal cancels with a cause
- **WHEN** the process receives SIGINT or SIGTERM
- **THEN** the context is done, the program keeps running, and the context's cause carries that signal and matches context.Canceled

#### Scenario: Second signal terminates
- **WHEN** the process receives SIGINT, sees the context done, and then receives SIGTERM while still shutting down
- **THEN** the process is terminated by SIGTERM, with no delay needed between observing the cancellation and sending the second signal

#### Scenario: Started with SIGINT ignored
- **WHEN** a process started with SIGINT ignored, such as a background job in a script, receives SIGINT
- **THEN** the first SIGINT still cancels the context, and later SIGINTs are ignored again

#### Scenario: Parent cancellation propagates
- **WHEN** the parent context is cancelled
- **THEN** the signal-aware context is done too

### Requirement: Execute turns a command's error into an exit code

Execute SHALL run the root of the command tree it's given with the caller's context, and every command in the tree SHALL observe that context's values and cancellation, even when the tree has been executed before. It SHALL return 0 when the command succeeds. When the error chain contains an exit error, it SHALL return that error's code, or 1 if the code is outside 1 to 255: zero would report success, and Unix truncates an exit code to its low 8 bits. It SHALL return 1 for any other error.

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

#### Scenario: Code that would truncate to zero
- **WHEN** the command returns an exit error with code 256
- **THEN** Execute returns 1

#### Scenario: Context reaches commands
- **WHEN** the caller passes a context carrying a value
- **THEN** the command observes that value through its context

#### Scenario: Reused command tree
- **WHEN** the caller executes the same tree twice, with a different context each time
- **THEN** the second run's commands observe the second context

#### Scenario: Given a subcommand
- **WHEN** the caller passes a subcommand instead of the root
- **THEN** Execute runs the root, and whichever command the arguments select observes the caller's context

### Requirement: Exit errors wrap an optional cause

An exit error SHALL carry a code and an optional underlying error. Its message SHALL be the underlying error's message, or `exit status N` when there's no underlying error, where N is the code Execute returns for it. Error inspection SHALL reach the underlying error and find the exit error anywhere in a wrapping chain. Building or printing an exit error without an underlying error SHALL NOT panic.

#### Scenario: Cause is reachable
- **WHEN** a command returns an exit error with code 3 wrapping cause C
- **THEN** errors.Is reports C in the chain, and the message is C's message

#### Scenario: No cause
- **WHEN** an exit error with code 2 has no underlying error
- **THEN** its message is `exit status 2`

#### Scenario: Message matches the exit code
- **WHEN** an exit error with code 0 has no underlying error
- **THEN** its message is `exit status 1`

### Requirement: Failures print the error, never the usage text

A root command built by the module SHALL print a failing command's error once on standard error, and SHALL NOT print usage text for any failure, flag errors included. A command that silences its errors through Cobra before returning SHALL exit with its code and print nothing. Asking for `--help` SHALL still print help.

#### Scenario: Runtime failure
- **WHEN** a subcommand returns an error
- **THEN** the error is printed once on standard error and no usage text is printed

#### Scenario: Quiet exit
- **WHEN** a subcommand silences its errors and returns an exit error with code 1 and no underlying error
- **THEN** nothing is printed and Execute returns 1

### Requirement: Every command accepts --config

A root command built by the module SHALL accept a string flag `--config`, defaulting to empty, on itself and every subcommand. The module SHALL NOT claim any single-letter shorthand on the program's commands.

#### Scenario: Subcommand reads the flag
- **WHEN** a program runs `myapp serve --config /etc/myapp.yaml`
- **THEN** the serve command reads `/etc/myapp.yaml` as the config flag's value

#### Scenario: Subcommand uses -c
- **WHEN** a subcommand defines its own flag with shorthand `-c` and a program runs it with `-c 4`
- **THEN** the subcommand runs and reads 4 from its own flag

### Requirement: One version line

A root command built by the module SHALL accept `--version`, and the module's version subcommand SHALL print the same single line, even after the program changes the root command's version. The line SHALL be the root command's display name and the root command's version, which starts as the build info version, followed by a parenthesised, comma-separated list of the revision, the time, and the word `modified`, each included only when known or true. The parentheses SHALL be omitted when none of them is.

#### Scenario: Flag and subcommand agree
- **WHEN** a user runs `myapp --version` and then `myapp version`
- **THEN** both print the same line

#### Scenario: Program overrides the version
- **WHEN** a program sets its root command's version to `1.4.0`
- **THEN** `myapp --version` and `myapp version` both print `myapp 1.4.0`

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
