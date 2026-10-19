## ADDED Requirements

### Requirement: Pull requests scan their modules for known vulnerabilities

CI SHALL scan every library module it selects for lint and test, and `tools/release` together with the tools its go.mod pins, for known vulnerabilities in the code it builds, using a pinned scanner version, and SHALL fail that module's job when a vulnerability is reachable from the module's code. A vulnerability in a dependency the module never calls SHALL NOT fail it. The scan SHALL roll into the pull request's overall status like lint and test, and SHALL be skipped, counting as passing, when no module is selected.

#### Scenario: A reachable vulnerability in a changed module
- **WHEN** a pull request changes `logger`, and `logger` calls a function in a dependency version with a known vulnerability
- **THEN** `logger`'s vulnerability job fails, and the overall status fails

#### Scenario: An unreachable vulnerability
- **WHEN** a changed module requires a dependency version with a known vulnerability in a function the module never calls
- **THEN** its vulnerability job passes

#### Scenario: An unchanged module
- **WHEN** a pull request changes only `logger`, and `metrics` has a reachable vulnerability
- **THEN** `metrics` isn't scanned, and the pull request isn't blocked by it

#### Scenario: Root configuration changes
- **WHEN** a pull request edits `.go-version`
- **THEN** every library module is scanned

### Requirement: A weekly scan reports vulnerabilities in a single issue

A scheduled workflow SHALL scan every library module in `go.work` and every tool under `tools/`, including the tools each one's go.mod pins, once a week, and SHALL also be runnable on demand. The issue tracks the default branch: a run on any other branch SHALL report its findings without touching the issue tracker. Its findings, counting only vulnerabilities reachable from each module's code, SHALL be reported in at most one open issue carrying the `govulncheck` label:

- with findings and no such open issue, it SHALL open one listing, per affected module, each advisory's ID, summary, affected dependency and fixed version;
- with findings and such an open issue, it SHALL replace the issue's body with the new report, and SHALL comment on the issue only when the advisories found in each module differ from the previous report's, so an advisory reaching another module, or an advisory gaining or losing a fixed version, counts as a change; when the issue's body no longer records a previous report, it SHALL replace the body without commenting;
- with no findings and such an open issue, it SHALL close the issue with a comment linking the run;
- with no findings and no such issue, it SHALL leave the issue tracker untouched.

When any module's scan fails to run, the workflow SHALL fail and SHALL leave the issue tracker untouched, and its report SHALL still show the other modules' findings, marked incomplete. A run that finds vulnerabilities and reports them SHALL succeed.

#### Scenario: A new advisory
- **WHEN** the weekly scan finds a reachable vulnerability in `kernel` and no open issue has the `govulncheck` label
- **THEN** an issue labelled `govulncheck` is opened listing `kernel` with the advisory's ID, summary, affected dependency and fixed version, and the run succeeds

#### Scenario: The same findings a week later
- **WHEN** the next weekly scan finds the same advisories
- **THEN** the issue's body is replaced with the new report, and no comment is added

#### Scenario: Another advisory appears
- **WHEN** a later scan finds an advisory the previous report didn't list
- **THEN** the issue's body is replaced, and a comment is added

#### Scenario: An advisory reaches another module
- **WHEN** an advisory the report listed for `log` is now also reachable from `kernel`
- **THEN** the issue's body is replaced, and a comment names the advisory in `kernel`

#### Scenario: A fix becomes available
- **WHEN** an advisory the report listed for `kernel` with no fix now has a fixed version
- **THEN** the issue's body is replaced, and a comment says a fix is available, naming the fixed version

#### Scenario: A run on another branch
- **WHEN** the workflow is started by hand on a branch other than the default
- **THEN** its findings appear in the run's report, and the issue isn't opened, edited, commented on or closed

#### Scenario: A hand-edited body
- **WHEN** someone rewrote the issue's body so it no longer records the previous report, and a scan finds the same advisories
- **THEN** the body is replaced with the new report, and no comment is added

#### Scenario: Too many findings to list
- **WHEN** the findings are too many to list in an issue body
- **THEN** the body is cut down to fit, and a later scan still comments when the findings change

#### Scenario: Everything is fixed
- **WHEN** a scan finds no reachable vulnerabilities and the issue is open
- **THEN** the issue is closed with a comment linking the run

#### Scenario: The scanner can't run
- **WHEN** a module's scan fails, for example because the vulnerability database is unreachable
- **THEN** the run fails, no issue is opened, edited, commented on or closed, and the report shows the other modules' findings marked incomplete
