# kernelrun Specification

## Purpose

Runs an fx app's registered runners between its start and stop: the first runner to return stops the rest, every ending shuts the app down cleanly, and OnStop hooks learn whether the run crashed.

## Requirements

### Requirement: Registered runners run concurrently once the app has started

The module SHALL start every registered runner, each in its own goroutine, after every OnStart hook has finished, and SHALL pass each one a context carrying the caller's values. Runners SHALL be registrable as a constructor that takes dependencies from the graph or as a ready function, each under a name.

#### Scenario: After start
- **WHEN** an app registers two runners and an OnStart hook
- **THEN** both runners start after the hook has finished, and both are running at once

#### Scenario: The caller's values
- **WHEN** the caller's context carries a value
- **THEN** each runner's context carries it too

#### Scenario: A runner from a constructor
- **WHEN** a runner is registered as a constructor that takes a value from the graph
- **THEN** the runner the constructor built is the one that runs

### Requirement: The set of runners is checked before the app starts

Running an app with no runners SHALL fail with the module's no-runners error. Two runners with the same name SHALL fail with its duplicate-runner error, and a runner with an empty name or no function with its invalid-runner error. Each failure SHALL happen before any OnStart hook runs.

#### Scenario: No runners
- **WHEN** an app registers no runners and has an OnStart hook
- **THEN** running it fails with an error matching the no-runners error, and the hook never runs

#### Scenario: A duplicate name
- **WHEN** two runners are registered under the name `api`
- **THEN** running the app fails with an error matching the duplicate-runner error, naming `api`

#### Scenario: An invalid runner
- **WHEN** a runner is registered with an empty name, or without a function
- **THEN** running the app fails with an error matching the invalid-runner error

### Requirement: The first runner to return stops the rest

When any runner returns, for any reason, the module SHALL cancel every other runner's context and SHALL wait for all of them to return before stopping the app.

#### Scenario: A clean return
- **WHEN** one runner returns nil while another waits on its context
- **THEN** the waiting runner's context is cancelled, it returns, and the app stops

#### Scenario: A crash
- **WHEN** one runner returns an error while another waits on its context
- **THEN** the waiting runner's context is cancelled, and the app stops after both have returned

#### Scenario: The caller cancels
- **WHEN** the caller's context is cancelled while runners wait on theirs
- **THEN** every runner's context is cancelled, and the app stops after they've all returned

### Requirement: Each runner's outcome is clean or a crash

A runner returning nil SHALL count as clean. A runner returning its context's own error SHALL count as clean once that context is done, whether the first return or the caller cancelled it or the caller's deadline passed. The same error returned while the context is live, and any other error, SHALL count as a crash, and the error returned for it SHALL name the runner and wrap the runner's error. The result SHALL be nil when every outcome is clean, and every crash error joined otherwise.

#### Scenario: Everything clean
- **WHEN** one runner returns nil and the others return `context.Canceled` after being cancelled
- **THEN** the result is nil

#### Scenario: A crash
- **WHEN** the runner `api` returns an error
- **THEN** the result names `api` and matches that error with `errors.Is`

#### Scenario: An early cancellation error
- **WHEN** a runner returns `context.Canceled` while the runners' context is still live
- **THEN** it counts as a crash

#### Scenario: The caller's deadline
- **WHEN** the caller's context has a deadline and every runner returns its context's error once the deadline passes
- **THEN** the result is nil

#### Scenario: An early deadline error
- **WHEN** a runner returns `context.DeadlineExceeded` while the runners' context is still live
- **THEN** it counts as a crash

#### Scenario: An exit code survives
- **WHEN** a runner returns an error wrapping a value of an error type the caller defines
- **THEN** the result exposes that value through `errors.As`

### Requirement: A panicking runner is a crash, and the app still stops cleanly

A panic in a runner SHALL be recovered and counted as that runner's crash, with an error naming the runner and the panic's value. A runner that exits without returning, through `runtime.Goexit`, SHALL count as a crash the same way. The other runners SHALL be stopped and the OnStop hooks SHALL run, as for any crash.

#### Scenario: A panic
- **WHEN** the runner `worker` panics with the value `boom` while another runner waits on its context
- **THEN** the other runner is cancelled, the OnStop hooks run, and the result names `worker` and `boom`

#### Scenario: An exit through Goexit
- **WHEN** the runner `worker` calls `runtime.Goexit` while another runner waits on its context
- **THEN** the other runner is cancelled, the OnStop hooks run, and the result names `worker` as having exited without returning

### Requirement: Startup is bounded, and its failures are reported by their cause

The OnStart hooks SHALL run under a start timeout, 15 seconds unless the caller sets another. A failure while building the app or starting it SHALL be returned as the failing function's own error, without the dependency-injection layers around it, and a build failure SHALL read differently from a hook failure. A panic in a constructor or an invoked function SHALL be such a failure, not a crash of the process. If the start fails because the caller's context ended, cancelled or past its deadline, the result SHALL be nil; a start failure with any other cause SHALL be returned, even if the caller's context has ended since.

#### Scenario: A failing constructor
- **WHEN** a constructor returns the error `validating config: port required`
- **THEN** running the app fails with an error ending in that message, which doesn't mention building arguments for a function

#### Scenario: A panicking constructor
- **WHEN** a constructor panics
- **THEN** running the app fails with an error, and the process keeps running

#### Scenario: A hook past the timeout
- **WHEN** an OnStart hook waits on its context and the start timeout is 50 milliseconds
- **THEN** running the app fails once the timeout has passed, and no runner starts

#### Scenario: A failing hook
- **WHEN** an OnStart hook returns the error `bind: address already in use`
- **THEN** running the app fails with an error ending in that message, which reads differently from a failing constructor's

#### Scenario: Cancelled during start
- **WHEN** the caller's context is cancelled while an OnStart hook waits on its context
- **THEN** the result is nil, and no runner starts

#### Scenario: The caller's deadline during start
- **WHEN** the caller's context reaches its deadline while an OnStart hook waits on its context, with a longer start timeout
- **THEN** the result is nil, and no runner starts

#### Scenario: Another failure after the caller ends
- **WHEN** a start fails with an error that isn't the caller's context's ending, and the caller's context has ended by the time the failure is judged
- **THEN** running the app fails with that error

### Requirement: OnStop hooks see the run's outcome on a live context

The OnStop hooks SHALL run under a stop timeout, 15 seconds unless the caller sets another, on a context that keeps the caller's values but isn't cancelled by the caller's cancellation. That context SHALL carry the run's outcome: nil when every runner was clean, and the joined crash errors otherwise. A failing OnStop hook SHALL add its error to the result, joined with any crash. OnStop hooks run to roll back a failed start SHALL read a not-started outcome, never nil.

#### Scenario: A clean outcome
- **WHEN** every runner was clean and an OnStop hook reads the run's outcome from its context
- **THEN** it reads nil

#### Scenario: A crash outcome
- **WHEN** a runner crashed with an error and an OnStop hook reads the run's outcome
- **THEN** it reads an error matching that runner's error

#### Scenario: After a signal
- **WHEN** the caller's context, carrying a value, was cancelled to stop the app
- **THEN** an OnStop hook's context isn't done until the stop timeout and carries the value

#### Scenario: A failing hook
- **WHEN** a runner crashed and an OnStop hook returns an error
- **THEN** the result matches both errors with `errors.Is`

#### Scenario: A rolled-back start
- **WHEN** a hook with an OnStop has started and a later OnStart hook fails
- **THEN** the first hook's OnStop reads an outcome matching the module's not-started error

#### Scenario: Attached by a test
- **WHEN** a test attaches an error to a context and reads the run's outcome back from it
- **THEN** it reads that error, and a context with nothing attached reads nil

### Requirement: Running an app never subscribes to operating-system signals

The module SHALL NOT subscribe to SIGINT or SIGTERM. The caller's context SHALL be the only way a signal reaches a running app, so a caller's own handling of a second signal keeps working.

#### Scenario: A signal nobody handles
- **WHEN** a program runs an app whose runner waits on its context, with a context no signal cancels, and the program receives SIGINT
- **THEN** the process ends by SIGINT

### Requirement: Failures and the lifecycle are logged through the app's logger

When the app's graph holds a `*slog.Logger`, the module SHALL log each crash and each stop failure through it at Error, naming the runner where there is one, with a panic's stack. It SHALL log the lifecycle (the runners starting, the first one returning, the shutdown's cause) at Debug. It SHALL NOT log startup failures, and SHALL log nothing when the graph holds no logger.

#### Scenario: A crash
- **WHEN** the runner `api` returns an error in an app whose graph holds a logger
- **THEN** a record at Error names `api` and the error

#### Scenario: The lifecycle
- **WHEN** an app runs and stops cleanly with a logger at Debug
- **THEN** Debug records show the runners starting, which runner returned first, and the shutdown's cause

#### Scenario: No logger
- **WHEN** an app whose graph holds no logger has a crashing runner
- **THEN** running it returns the crash and nothing panics
