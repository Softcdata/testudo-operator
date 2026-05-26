## ADDED Requirements
### Requirement: User Identity Propagation
Controllers MUST identify the initiator of an operation by reading the `testudo.softcdata.com/user` annotation from the resource.

#### Scenario: User identity present
- **WHEN** recording an event for a resource
- **AND** the resource has the `testudo.softcdata.com/user` annotation set to "admin"
- **THEN** the event message tag `[User: ...]` MUST be set to "admin"

#### Scenario: User identity missing
- **WHEN** recording an event for a resource
- **AND** the annotation is missing
- **THEN** the event message tag `[User: ...]` MUST default to "system"
