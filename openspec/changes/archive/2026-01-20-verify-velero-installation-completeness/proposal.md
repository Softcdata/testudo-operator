# Proposal: Verify Velero Installation Completeness

## Problem
Currently, the Operator considers Velero installed as long as the CRDs are present or the Deployment exists. However, a functional Velero installation also requires the `node-agent` DaemonSet to be running, especially for Restic/Kopia based file system backups. The current check is insufficient and may lead to false positives where the Operator thinks Velero is ready but file-level backups fail.

## Solution
Enhance the Velero installation verification logic to explicitly check for the existence and readiness of the `node-agent` DaemonSet in addition to the Velero Deployment.

1.  **Extended Verification**: In `cluster_controller.go`, extend `IsVeleroInstalled` (or similar logic) to check for `node-agent` DaemonSet.
2.  **Status Reporting**: Reflect the status of `node-agent` in the Cluster status or events if missing.

## Impact
- **Cluster Controller**: Modified to include `node-agent` check.
- **Cluster Status**: More accurate reflection of Velero readiness.
