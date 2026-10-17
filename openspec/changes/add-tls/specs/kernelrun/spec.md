## MODIFIED Requirements

### Requirement: A panicking runner is a crash, and the app still stops cleanly

A panic in a runner SHALL be recovered and counted as that runner's crash, with an error naming the runner and the panic's value. A runner that exits without returning, through `runtime.Goexit`, SHALL count as a crash the same way, with an error naming the runner and matching the module's exited error. The other runners SHALL be stopped and the OnStop hooks SHALL run, as for any crash.

#### Scenario: A panic
- **WHEN** the runner `worker` panics with the value `boom` while another runner waits on its context
- **THEN** the other runner is cancelled, the OnStop hooks run, and the result names `worker` and `boom`

#### Scenario: An exit through Goexit
- **WHEN** the runner `worker` calls `runtime.Goexit` while another runner waits on its context
- **THEN** the other runner is cancelled, the OnStop hooks run, and the result names `worker` as having exited without returning and matches the exited error
