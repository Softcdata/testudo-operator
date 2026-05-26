# Recovery Specification

# Recovery Specification

## MODIFIED Requirements

### Requirement: Failover Starts
Requirement: When a Failover operation transitions to Running, the DisasterInstance state MUST be updated to intermediate state.

#### Scenario: Failover Start
Given a Protected DisasterInstance
When a Failover Operation starts
Then the Instance FsmState becomes `FailingOver`

### Requirement: Failover Fails
Requirement: When a Failover operation fails (e.g. timeout), the DisasterInstance state MUST remain in intermediate state to allow recovery.

#### Scenario: Failover Timeout
Given a FailingOver DisasterInstance
When the Failover Operation times out
Then the Instance FsmState remains `FailingOver`
And the Operation State becomes `Failed`

### Requirement: Undo Allowed States
Requirement: The Undo operation MUST be allowed when the Instance is in Active or FailingOver state.

#### Scenario: Undo Failed Failover
Given a FailingOver DisasterInstance (due to failed failover)
When an Undo Operation is created
Then it is accepted and executed
And eventually the Instance becomes `Protected`

### Requirement: Retry Allowed States
Requirement: A new Failover operation MUST be allowed when the Instance is in FailingOver state if the previous operation is not running.

#### Scenario: Retry Failed Failover
Given a FailingOver DisasterInstance with a Failed operation
When a new Failover Operation is created
Then it is accepted and executed


