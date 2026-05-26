# Tasks: Implement Disaster Logic V2

## Context
Ref: `openspec/changes/implement-disaster-logic-v2/proposal.md`
We are implementing the actual business logic for Disaster Recovery orchestration, specifically the "Failover" operation and multi-cluster client management.

## Plan

### 1. DisasterOperation Controller Implementation
- [x] **Refine `handleFailover` Flow** <!-- id: 1 -->
  - Review and update the main state machine loop in `handleFailover`.
- [x] **Implement `PauseSchedules`** <!-- id: 2 -->
  - Ensure correct patching of DataSync/ResourceSync.
- [x] **Implement `ScaleDownSource`** <!-- id: 3 -->
  - Use `Cluster` CR to build Source Client on demand.
  - Iterate namespaces, annotating and scaling down Deployments/StatefulSets.
- [x] **Implement `FinalSync`** <!-- id: 4 -->
  - Trigger manual sync.
  - Wait for completion (check `LastSyncTime` and `Status`).
- [x] **Implement `ScaleUpTarget`** <!-- id: 5 -->
  - Use `Cluster` CR to build Target Client on demand.
  - Remove Trafficless selectors from Services.
  - Restore replicas from annotations.
- [x] **Implement `Failback` Logic** <!-- id: 6 -->
  - Implement the reverse logic for Failback.

### 2. Testing
- [x] **Unit Tests** <!-- id: 7 -->
  - Verify state transitions.
  - Test multi-cluster logic using fake clients.
