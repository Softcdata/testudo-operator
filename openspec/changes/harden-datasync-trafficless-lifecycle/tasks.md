## 1. Proposal and Boundary

- [x] 1.1 Inspect existing DataSync, AppRestore, Cluster runtime, cleanup label, and trafficless proposals.
- [x] 1.2 Define a DataSync-only lifecycle boundary and explicit non-goals.
- [x] 1.3 Define owner/run-scoped cleanup and failure-closed handling for ambiguous legacy Pods.
- [x] 1.4 Define status, timeout, and reason contracts without CRD/API expansion.
- [x] 1.5 Validate this proposal with openspec validate harden-datasync-trafficless-lifecycle --strict.

## 2. DataSync Lifecycle

- [x] 2.1 Add DataSync-only AppRestore lifecycle label and owner/run-scoped Trafficless Pod labels.
- [x] 2.2 Replace image-based cleanup detection with exact platform cleanup selectors.
- [x] 2.3 Add CleanupBeforeRestore polling, bounded timeout, and ambiguous legacy Pod failure handling.
- [x] 2.4 Add CleanupAfterRestore polling; do not transition DataSync to Ready before deletion confirmation.
- [x] 2.5 Propagate AppRestore reason/message to DataSync Conditions, events, and history.
- [x] 2.6 Align generated AppRestore timeout with DisasterInstance OperationTimeoutMinutes.
- [x] 2.7 Verify target PVC usable state before DataSync Ready.

## 3. Trafficless AppRestore Observation

- [x] 3.1 Restrict new observation behavior to AppRestore objects carrying the DataSync Trafficless lifecycle label.
- [x] 3.2 Detect bounded Unschedulable, image pull, mount/configuration, and runtime container failures on the exact Trafficless Pod set.
- [x] 3.3 Check target Velero Deployment/node-agent preconditions and scheduled-node node-agent coverage.
- [x] 3.4 Treat PVR Failed, long Pending, and long InProgress as stable failures with precise diagnostics.
- [x] 3.5 Confirm same-name Velero Restore deletion before retry/recreate; fail closed on termination timeout.
- [x] 3.6 Preserve existing generic AppRestore behavior for ordinary AppRestore, ResourceSync, and Drill paths.

## 4. Tests

- [x] 4.1 Unit test exact owner/run cleanup selection and non-deletion of normal busybox Pods.
- [x] 4.2 Unit test cleanup API success followed by delayed/not-completed deletion.
- [x] 4.3 Unit test stale unlabeled trafficless Pod failure-closed behavior.
- [x] 4.4 Unit test Node/Pod failures: Unschedulable, ImagePullBackOff, FailedMount, init/sidecar failure, and scheduled-node node-agent absence.
- [x] 4.5 Unit test PVR Failed, Pending, and InProgress timeout diagnostics.
- [x] 4.6 Unit test same-name Velero Restore termination wait and timeout.
- [x] 4.7 Unit test AppRestore timeout propagation from instance configuration.
- [x] 4.8 Regression test ResourceSync, Drill, shared builder, and ordinary AppRestore remain outside the new branch.

## 5. E2E and Documentation

- [ ] 5.1 Run isolated target-cluster E2E cases for each failure class with unique namespace/instance/run prefixes.
- [x] 5.2 Verify API/operator/Kubernetes/Velero/PVR/PVC evidence and retain failed evidence.
  - 已通过的隔离运行时用例：`DS-TL-E2E-001`（PVC FSB、PVR/PVC、cleanup confirmation、marker、MinIO）和 `DS-TL-E2E-006`（无归属 Pod 失败关闭、普通 busybox 不删除）。证据目录：`/home/chenxi/YS/disaster-test/test/e2e-browser-fullstack/datasync-trafficless-lifecycle-runtime-exec-20260715-135205/`。
  - 5.1 保持未完成：`DS-TL-E2E-002..005` 需停止 node-agent、干预 Velero/PVR 或等待长终止窗口，未在共享环境中安全注入；相应单测已通过，未被标记为运行时通过。
- [x] 5.3 Update operational troubleshooting with the stable reason-to-remediation mapping.
- [x] 5.4 Update Harness progress and decision logs for each implementation milestone.

## 6. Validation

- [x] 6.1 Run openspec validate harden-datasync-trafficless-lifecycle --strict.
- [ ] 6.2 Run openspec validate --strict.
  - Current CLI requires `--all`; `openspec validate --all --strict` reports 67 passed and 18 pre-existing failures outside this change.
- [x] 6.3 Run make harness-preflight, make harness-lint, and make harness-ci.
- [x] 6.4 Run go test for DataSync, AppRestore, Cluster, ResourceSync, and DisasterOperation packages.
- [x] 6.5 Run make test and make lint before implementation delivery.
  - `make test` passed. `make lint` was executed and reports 258 repository-wide existing issues; targeted scans report no issue in this change's lifecycle source or tests.
