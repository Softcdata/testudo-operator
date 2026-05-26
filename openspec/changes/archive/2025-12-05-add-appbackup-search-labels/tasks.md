# 任务列表

- [x] 修改 `internal/controller/appbackup_controller.go`:
    - [x] 定义新的标签常量:
        - `LabelAppBackupName = "testudo.softcdata.com/app-backup-name"`
        - `LabelAppBackupNamespace = "testudo.softcdata.com/app-backup-namespace"`
        - `LabelAppBackupCluster = "testudo.softcdata.com/app-backup-cluster"`
        - `LabelAppBackupStatus = "testudo.softcdata.com/app-backup-status"`
    - [x] 实现 `syncLabels` 方法:
        - 检查上述标签是否存在且值是否正确。
        - `LabelAppBackupNamespace` 取值逻辑：
            - 检查 `spec.template.includedNamespaces`。
            - 如果列表包含 `*` 或为空，值为 "all"。
            - 否则将列表中的所有值用逗号拼接（例如 "ns1,ns2"）。
        - 如果不一致，更新 `AppBackup.Labels`。
        - 注意：`status` 标签应反映 `status.Status` 字段的值。
    - [x] 在 `Reconcile` 流程的合适位置（如 Status 更新后，或流程末尾）调用 `syncLabels`。
        - **关键**: 必须确保仅在标签确实需要变更时才执行 Update 操作，以避免触发无限 Reconcile 循环。
