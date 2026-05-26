# Sync History

## ADDED Requirements

### Requirement: ResourceSync and DataSync MUST maintain a history of recent sync executions
ResourceSync and DataSync CRs SHALL include a `.status.history` field that stores a list of recent synchronization records. Each record SHALL capture the execution details including timestamp, duration, and status.

#### Scenario: Viewing sync history
Given a ResourceSync object that has completed 5 sync cycles
When I inspect its `.status.history` field
Then I should see 5 records
And the records should contain start time, duration, and status
And the number of records should not exceed the configured limit (20)

### Requirement: Sync execution history MUST be synchronized to BackupRestoreStatistics
The Operator MUST regularly synchronize the aggregated success and failure counts from the `history` list to the associated `BackupRestoreStatistics` CR.

#### Scenario: Verifying statistics accuracy
Given a ResourceSync object with 3 successful and 1 failed sync history records
When I inspect the associated BackupRestoreStatistics object
Then the `completed` count should be 3
And the `failed` count should be 1
