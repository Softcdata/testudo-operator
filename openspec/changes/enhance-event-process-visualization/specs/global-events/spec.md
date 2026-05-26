## ADDED Requirements

### Requirement: Task Timeline Visualization
The Server MUST aggregate all events sharing the same Task ID into a chronological timeline, rather than just retaining the latest state.

#### Scenario: Task Detail Response
- **WHEN** client requests a specific Task Event
- **THEN** the response includes a `timeline` array
- **AND** the array contains all associated events sorted by timestamp
- **AND** the root `message` reflects the latest state, but history is preserved in `timeline`

### Requirement: Intermediate Progress Reporting
Long-running operations MUST report intermediate `InProgress` events with descriptive messages to populate the timeline.

#### Scenario: Operation Progress
- **WHEN** an operation (e.g., Backup) completes a significant sub-step (e.g., resource created)
- **THEN** the controller emits an event with `Reason=ExecutionStarted` (or `InProgress`) and `Status=InProgress`
- **AND** the message describes the specific step (e.g., "Velero Backup created, waiting for completion")
- **AND** the `duration` remains unset (or calculated dynamically)
