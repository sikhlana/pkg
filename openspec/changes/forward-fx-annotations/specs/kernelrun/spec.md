## ADDED Requirements

### Requirement: A runner's constructor can take tagged dependencies

Registering a runner from a constructor SHALL accept annotations for the constructor's inputs and hooks, applied with the registration's own, so a constructor can take a dependency provided under a name tag. An annotation that repeats one the registration sets itself SHALL fail startup.

#### Scenario: A tagged dependency
- **WHEN** a runner is registered from a constructor that takes a string, with an annotation tagging that parameter as `name:"greeting"`, and the app provides `"hello"` under that tag
- **THEN** the runner the constructor built runs with `"hello"`

#### Scenario: A repeated result tag
- **WHEN** a runner is registered with an annotation tagging the constructor's result
- **THEN** startup fails
