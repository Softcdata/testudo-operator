# Task: 实现 V2 链路追踪 (Trace ID Propagation V2)

- [ ] **Proposal Review** <!-- id: 0 -->
    - [x] Create comprehensive proposal `implement-trace-id-propagation-v2/proposal.md` <!-- id: 1 -->
    - [ ] Review and approve the proposal <!-- id: 2 -->

- [ ] **Server API Enhancement** <!-- id: 3 -->
    - [ ] Update `ExecuteAction` in `disaster-server` to extract `X-Trace-ID` (or similar) from request headers. <!-- id: 4 -->
    - [ ] Inject `testudo.softcdata.com/trace-id` annotation into `DisasterOperation` metadata during creation. <!-- id: 5 -->

- [ ] **Controller Logic: Group Operations** <!-- id: 6 -->
    - [ ] Verify `internal/controller/disasteroperation/controller_group.go`. <!-- id: 7 -->
    - [ ] In `handleGroupOperation`, when creating child `DisasterOperation`s (for instances), populate `Annotations["testudo.softcdata.com/trace-id"]` from the parent Group Operation. <!-- id: 8 -->

- [ ] **Controller Logic: Sync Operations** <!-- id: 9 -->
    - [ ] Verify `internal/controller/disasteroperation/controller.go`. <!-- id: 10 -->
    - [ ] In `handleSync` (SyncOnce, SyncData, SyncResource), read Trace ID from Operation. <!-- id: 11 -->
    - [ ] Update `DataSync` / `ResourceSync` annotations with `testudo.softcdata.com/last-trace-id` before triggering sync. <!-- id: 12 -->
    - [ ] In `executeFinalSync` (Failover), similarly propagate Trace ID to DataSync/ResourceSync. <!-- id: 13 -->
    - [ ] In `handleReprotect` -> `executeResumeSchedules` -> update Sync annotations. <!-- id: 14 -->

- [ ] **Controller Logic: ConfigMap metadata** <!-- id: 15 -->
    - [ ] Verify `internal/controller/disasteroperation/controller.go` -> `recordReplicasBeforeScaleDown`. <!-- id: 16 -->
    - [ ] Add `testudo.softcdata.com/trace-id` annotation to the created `replicas-xxx` ConfigMap. <!-- id: 17 -->

- [ ] **Validation** <!-- id: 18 -->
    - [ ] Create E2E test scenario verifying trace ID propagation through the chain: GroupOp -> InstanceOp -> DataSync -> ConfigMap. <!-- id: 19 -->
