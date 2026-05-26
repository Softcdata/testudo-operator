# Tasks

- [ ] Define `FsmStateFailingOver` constant in `disasterinstance_types.go` (if not exists). <!-- id: 1 -->
- [ ] Update `handleFailover` to set state to `FailingOver` at start (before steps). <!-- id: 2 -->
- [ ] Update `handleUndo` to allow `FailingOver` state. <!-- id: 3 -->
- [ ] Update `handleUndo` logic to handle partial failover (ensure idempotency). <!-- id: 4 -->
- [ ] Validate Retry logic (ensure creating Failover op is allowed when state is FailingOver but prev op failed). <!-- id: 5 -->
- [ ] Update E2E tests to verify state transitions and Undo flow. <!-- id: 6 -->
