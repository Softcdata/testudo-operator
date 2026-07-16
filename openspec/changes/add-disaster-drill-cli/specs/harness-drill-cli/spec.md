## ADDED Requirements

### Requirement: Instance drill command contract

The Harness SHALL provide a Bash CLI accepting exactly one of `create`, `execute`, `cleanup`, or `status` plus a DisasterInstance name.

#### Scenario: Valid command invocation
- **WHEN** the user invokes a supported subcommand with a non-empty instance name
- **THEN** the CLI performs the corresponding drill API workflow

#### Scenario: Invalid invocation
- **WHEN** the subcommand or instance name is missing or unsupported
- **THEN** the CLI prints usage and exits with a non-zero status without calling the API

### Requirement: Automatic API authentication

The CLI MUST authenticate before calling protected APIs, MUST use English for its source text, help, and diagnostics, and MUST NOT print credentials or access tokens.

#### Scenario: Default credential login
- **WHEN** neither `AUTH_TOKEN` nor `BEARER_TOKEN` is supplied
- **THEN** the CLI calls `POST /login` with the default `admin` and `123456` credentials, reads `.data.accessToken`, and uses it as a Bearer token for subsequent requests

#### Scenario: Credential override
- **WHEN** `AUTH_USERNAME` or `AUTH_PASSWORD` is supplied
- **THEN** the CLI uses the overridden value in the login request

#### Scenario: Explicit token
- **WHEN** `AUTH_TOKEN` or `BEARER_TOKEN` is supplied
- **THEN** the CLI skips login and uses the supplied Bearer token

#### Scenario: Login failure
- **WHEN** login returns a non-success response or no `.data.accessToken`
- **THEN** the CLI exits non-zero before calling a protected instance or Drill API

### Requirement: Create request derives from instance detail

Before creating a drill, the CLI MUST fetch the named DisasterInstance detail and MUST use the returned namespace, secondary cluster, protected namespaces, modifier/bulk configuration, and dataRestore hooks when building the request.

#### Scenario: Instance has restore customization and hooks
- **WHEN** instance detail contains modifier rules, bulk modifier actions, or `veleroHooks.dataRestore`
- **THEN** the create request carries the equivalent drill-level values and never sends `veleroHooks.dataBackup`

#### Scenario: Isolated namespace mapping
- **WHEN** no explicit `DRILL_NAMESPACE_MAPPING` override is supplied
- **THEN** every protected source namespace maps to a unique DNS-compatible target namespace for this drill invocation

#### Scenario: Instance detail is unavailable
- **WHEN** the instance API returns a non-success response or no data object
- **THEN** the CLI exits without creating a DisasterDrill

### Requirement: Resolve the relevant drill by instance

For non-create commands, the CLI MUST resolve a Drill associated with the supplied instance and MUST default to the most recently created matching Drill.

#### Scenario: Latest drill resolution
- **WHEN** `DRILL_NAME` is unset and matching drills exist
- **THEN** the CLI selects the first item from the server's creation-time-descending instance filter

#### Scenario: Explicit drill override
- **WHEN** `DRILL_NAME` is set
- **THEN** the CLI loads that Drill and rejects it if its `instanceName` differs from the supplied instance

#### Scenario: No matching drill
- **WHEN** no Drill is associated with the instance
- **THEN** the CLI exits non-zero with a diagnostic message

### Requirement: Enforce action state preconditions

The CLI MUST enforce the server's state requirements before mutating a Drill.

#### Scenario: Execute a ready drill
- **WHEN** the resolved Drill is `Ready`
- **THEN** the CLI calls `POST /drills/{name}/confirm`

#### Scenario: Cleanup a completed drill
- **WHEN** the resolved Drill is `Completed` and cleanup is not set
- **THEN** the CLI calls `POST /drills/{name}/cleanup`

#### Scenario: Cleanup is already progressing or complete
- **WHEN** the Drill is `CleaningUp` or `CleanedUp`, or its cleanup flag is already true
- **THEN** the cleanup command returns the current status without issuing a duplicate cleanup request

#### Scenario: Action is not allowed
- **WHEN** the Drill is in any other state for the requested action
- **THEN** the CLI exits non-zero without sending the action request

### Requirement: API and output handling

The CLI MUST validate HTTP status, business code, and JSON structure, and SHALL emit machine-readable JSON for successful commands.

#### Scenario: API error response
- **WHEN** an API returns non-2xx HTTP status or non-zero business code
- **THEN** the CLI prints the server message and response body to stderr and exits non-zero

#### Scenario: Successful status query
- **WHEN** a matching Drill exists
- **THEN** the status command prints its name, instance, namespace, state, reason, message, action flags, target cluster, current step, mapping, and steps as JSON

### Requirement: Operator usage manual

The Harness SHALL provide a Chinese usage manual for the disaster drill CLI.

#### Scenario: Safe end-to-end operation
- **WHEN** an operator prepares to create, execute, inspect, and clean up a drill
- **THEN** the manual documents prerequisites, commands, state gates, deterministic Drill selection, namespace isolation, DataSync backup checks, asynchronous polling, failure handling, and local tests
