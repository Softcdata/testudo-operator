# Tasks

- [x] 定义 `BackupRestoreStatistics` CRD 结构体 (`pkg/apis/disaster/v1/backuprestorestatistics_types.go`)。 <!-- id: define-crd -->
- [x] 实现 `StatisticsHelper` 接口及方法 (`pkg/helper/statistics_helper.go`)。 <!-- id: implement-helper -->
- [x] 生成 CRD Manifests 和 DeepCopy 代码。 <!-- id: generate-code -->
- [x] 在 `AppBackup` 控制器中集成统计更新逻辑。 <!-- id: integrate-appbackup -->
- [x] 在 `AppRestore` 控制器中集成统计更新逻辑。 <!-- id: integrate-apprestore -->
