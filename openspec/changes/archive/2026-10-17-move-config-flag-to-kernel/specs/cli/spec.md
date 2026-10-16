## REMOVED Requirements

### Requirement: Every command accepts --config

**Reason**: The config file is the kernel's concern: the kernel is the only reader of the path, and it now adds `--config` only to programs that load config.
**Migration**: Build the root with `kernel.App.RootCommand` and set `kernel.Options.Config`, or define the flag on the root with `cmd.PersistentFlags().String("config", "", …)` and read it by that name.

## ADDED Requirements

### Requirement: The root adds no flags of its own

A root command built by the module SHALL add no flags of its own beyond cobra's help and version flags, and SHALL claim no single-letter shorthand beyond cobra's: `-h` for help, and `-v` for `--version` only while no other flag takes it. Every other flag name and letter SHALL stay free for the program's commands.

#### Scenario: Subcommand uses -c
- **WHEN** a subcommand defines its own flag with shorthand `-c` and a program runs it with `-c 4`
- **THEN** the subcommand runs and reads 4 from its own flag

#### Scenario: No config flag
- **WHEN** a program runs `myapp serve --config /etc/myapp.yaml` under a root built by the module alone
- **THEN** the command fails as an unknown flag
