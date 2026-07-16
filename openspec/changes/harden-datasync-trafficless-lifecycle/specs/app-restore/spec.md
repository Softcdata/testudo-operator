## ADDED Requirements

### Requirement: DataSync-Owned Trafficless Restore Observation

AppRestore MUST apply Trafficless Pod and PVR observation only when the AppRestore carries the internal DataSync Trafficless lifecycle identifier. It MUST preserve the existing behavior for AppRestore objects without that identifier.

#### Scenario: DataSync Trafficless Pod has a startup failure

- **GIVEN** a DataSync-owned Trafficless AppRestore has restored a Pod identified by its platform owner/run labels
- **AND** the Pod is stably Unschedulable, has an image pull failure, mount/configuration failure, or a failing init/container runtime state
- **WHEN** the bounded observation window expires
- **THEN** AppRestore MUST enter an internal failure-convergence path with the corresponding stable reason and diagnostic message
- **AND** it MUST issue the existing safe Restore termination path
- **AND** it MUST NOT report a final failed terminal result until the target Restore deletion is confirmed or the termination timeout is reached

#### Scenario: Ordinary AppRestore is not a DataSync Trafficless restore

- **GIVEN** an AppRestore lacks the DataSync Trafficless lifecycle identifier
- **WHEN** AppRestore reconciles it
- **THEN** AppRestore MUST NOT enable the DataSync-specific remote Pod selector, node-agent coverage check, or Trafficless cleanup semantics

### Requirement: DataSync-Owned Restore Termination Confirmation

When a DataSync-owned Trafficless AppRestore retries or terminates a Velero Restore, it MUST confirm that the same-named target Restore is absent before creating a replacement. It MUST fail closed if deletion cannot be confirmed within the effective AppRestore timeout.

#### Scenario: Timed-out Restore is deleting normally

- **GIVEN** a DataSync-owned Trafficless Restore has exceeded its effective timeout
- **WHEN** AppRestore submits deletion for that Restore
- **THEN** AppRestore MUST remain in its internal termination-confirmation path until the target Restore is absent
- **AND** it MUST NOT create a same-named replacement while the old Restore remains present
- **AND** once the target Restore is absent, it MUST enter a failed terminal result with the preserved failure reason

#### Scenario: Timed-out Restore remains terminating

- **GIVEN** a DataSync-owned Trafficless Restore remains present past the effective timeout after deletion was requested
- **WHEN** AppRestore reconciles the termination-confirmation path
- **THEN** AppRestore MUST report VeleroRestoreTerminationTimeout
- **AND** it MUST preserve the target Restore name and observed termination details in its status message
