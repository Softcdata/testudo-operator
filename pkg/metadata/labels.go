package metadata

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	LabelVeleroStorageLocation = "velero.io/storage-location" // Velero BSL 名称

	// 通用依赖标签
	// LabelDependencyToken 标识资源自身 token（由 UID 派生）
	LabelDependencyToken = "testudo.softcdata.com/dependency-token"
	// LabelDependencyToPrefix 标识当前资源到下游资源的依赖边前缀
	// 完整 key 形如: testudo.softcdata.com/dependency-to-<target-token>
	LabelDependencyToPrefix = "testudo.softcdata.com/dependency-to-"
	// LabelCleanupOwnerToken 标识该资源会随某个 owner 删除而被清理
	LabelCleanupOwnerToken = "testudo.softcdata.com/cleanup-owner-token"
	// LabelCleanupRelation 标识清理关系来源
	LabelCleanupRelation = "testudo.softcdata.com/cleanup-relation"
	// LabelCleanupStrategy 标识清理执行策略
	LabelCleanupStrategy = "testudo.softcdata.com/cleanup-strategy"
	// LabelCleanupManagedBy 标识 cleanup 标签由哪个控制器体系维护
	LabelCleanupManagedBy = "testudo.softcdata.com/cleanup-managed-by"
	// LabelCleanupManagedByValueOperator 是当前 operator 写入 cleanup 标签的固定值
	LabelCleanupManagedByValueOperator = "disaster-operator"
	// LabelTrafficlessLifecycle marks the internal lifecycle owner of a trafficless restore.
	// It is intentionally an implementation label rather than a user-facing API field.
	LabelTrafficlessLifecycle = "testudo.softcdata.com/trafficless-lifecycle"
	// TrafficlessLifecycleDataSync scopes enhanced trafficless observation to DataSync.
	TrafficlessLifecycleDataSync = "datasync"
	// LabelTrafficlessRun distinguishes the temporary Pods produced by one restore run.
	LabelTrafficlessRun = "testudo.softcdata.com/trafficless-run"
	// CleanupRelationDataSyncTrafficlessPod is the cleanup protocol relation for DataSync FSB Pods.
	CleanupRelationDataSyncTrafficlessPod = "datasync.trafficlessPod"

	// AppBackup 标签
	LabelAppBackupName             = "testudo.softcdata.com/app-backup-name"              // 名称 用于列表检索 用于velero标识appbackup
	LabelAppBackupFinalizer        = "testudo.softcdata.com/finalizer"                    //  用于删除保护,删除appbackup,连级删除velero backup
	LabelAppBackupUID              = "testudo.softcdata.com/app-backup-uid"               // appbackup uid 用于velero标识appbackup
	LabelAppBackupIncludeNamespace = "testudo.softcdata.com/app-backup-include-namespace" //备份源命名空间 用于列表检索
	LabelAppBackupCluster          = "testudo.softcdata.com/app-backup-cluster"           //备份源集群 用于列表检索
	LabelAppBackupStatus           = "testudo.softcdata.com/app-backup-status"            //appbackup状态 用于列表检索
	LabelAppBackupType             = "testudo.softcdata.com/app-backup-type"              //appbackup类型  用于列表检索 Schedule/Manual

	// AppRestore 标签
	LabelAppRestoreFinalizer  = "testudo.softcdata.com/finalizer"               //  用于删除保护,删除apprestore,连级删除velero restore
	LabelAppRestoreUID        = "testudo.softcdata.com/app-restore-uid"         //  apprestore uid 用于velero标识apprestore
	LabelAppRestoreName       = "testudo.softcdata.com/app-restore-name"        // 名称 用于列表检索 用于velero标识apprestore，用于列表检索
	LabelAppRestoreNamespace  = "testudo.softcdata.com/app-restore-namespace"   // 目标命名空间 用于列表检索
	LabelAppRestoreCluster    = "testudo.softcdata.com/app-restore-cluster"     // 目标集群 用于列表检索
	LabelAppRestoreStatus     = "testudo.softcdata.com/app-restore-status"      //恢复状态 用于列表检索  Pending、Restoring、Succeeded、Failed、Cancelled、Deleting、Unknown
	LabelAppRestoreSource     = "testudo.softcdata.com/app-restore-source"      // appbackup 名称
	LabelAppRestoreSourceType = "testudo.softcdata.com/app-restore-source-type" // 恢复源类型 (Manual/Schedule), 同步自 AppBackup 的 LabelAppBackupType
	LabelAppRestoreUpdated    = "testudo.softcdata.com/app-restore-updated"     //恢复时间 用于列表检索

	// AppBackup / AppRestore 来源标签
	LabelAppResourceOrigin    = "testudo.softcdata.com/app-resource-origin"     // 资源来源: user / disaster-instance
	LabelAppResourceOwnerKind = "testudo.softcdata.com/app-resource-owner-kind" // 来源类型: user / datasync / resourcesync
	LabelAppResourceOwnerName = "testudo.softcdata.com/app-resource-owner-name" // 来源对象名: DataSync/ResourceSync 名称

	// 结构化任务事件来源标签
	LabelTaskOrigin     = "testudo.softcdata.com/task-origin"      // 任务来源: user / disaster-instance
	LabelTaskOriginKind = "testudo.softcdata.com/task-origin-kind" // 来源类型: user / datasync / resourcesync

	// DisasterPolicy 标签
	LabelDisasterPolicyUID   = "testudo.softcdata.com/disaster-policy-uid"   // DisasterPolicy uid 用于appbackup标识关联的DisasterPolicy
	LabelDisasterPolicyName  = "testudo.softcdata.com/disaster-policy-name"  // 名称 用于列表检索
	LabelDisasterPolicyType  = "testudo.softcdata.com/disaster-policy-type"  // 类型 用于列表检索  AutoBackup/DataSync/ResourceSync
	LabelDisasterPolicyState = "testudo.softcdata.com/disaster-policy-state" // 状态 用于列表检索  Enabled/Disabled
	LabelPolicyFinalizer     = "testudo.softcdata.com/policy-finalizer"      // 用于删除保护

	// StorageRepository 标签
	LabelStorageRepositoryName = "testudo.softcdata.com/storage-repository-name" // 名称 用于列表检索
	LabelStorageFinalizer      = "testudo.softcdata.com/storage-finalizer"       // 用于删除保护

	//Cluster 标签
	LabelClusterName                   = "testudo.softcdata.com/cluster-name"                     // 名称 用于列表检索
	LabelClusterNamespaceCount         = "testudo.softcdata.com/cluster-namespace-count"          // 统计口径内的命名空间数量（排除系统命名空间）用于列表检索
	LabelClusterResourceTotalCount     = "testudo.softcdata.com/cluster-resource-total-count"     // 统计口径内的 namespace 级备份资源总数 用于列表检索
	LabelClusterWorkloadNamespaceCount = "testudo.softcdata.com/cluster-workload-namespace-count" // 含 running workload 的命名空间数量 用于列表检索
	LabelClusterWorkloadTotalCount     = "testudo.softcdata.com/cluster-workload-total-count"     // 上述命名空间内的 namespace 级备份资源总数 用于列表检索
	LabelClusterFinalizer              = "testudo.softcdata.com/cluster-finalizer"                // 用于删除保护

	//Annotations
	AnnotationTraceID         = "testudo.softcdata.com/trace-id"         // trace id 用于请求追踪
	AnnotationLastTraceID     = "testudo.softcdata.com/last-trace-id"    // 最后一次触发操作的 trace id
	AnnotationUser            = "testudo.softcdata.com/user"             // user 用于操作审计
	AnnotationUninstallVelero = "testudo.softcdata.com/uninstall-velero" // 是否卸载velero
	// AnnotationAppBackupManualPaused records the user's pause/resume intent for an AppBackup.
	AnnotationAppBackupManualPaused = "testudo.softcdata.com/app-backup-manual-paused"
	// AnnotationEnsureStorage 是一次性触发信号，用于指示 Operator 立即检查并创建指定存储的 BSL。
	// Value: StorageRepository 的名称 (e.g. "s3-default")
	// Action: Operator 收到后会创建 Secret/BSL，然后移除此注解。
	AnnotationEnsureStorage = "testudo.softcdata.com/ensure-storage"
	// AnnotationEnsureStorageSourceCluster 用于指定 ensure-storage 生成 BSL 的源集群后缀。
	// Value: SourceCluster 的名称 (e.g. "cluster-a")
	AnnotationEnsureStorageSourceCluster = "testudo.softcdata.com/ensure-storage-source-cluster"
	// AnnotationRefreshClusterStats 是一次性 typed refresh 信号，用于指示 Operator 立即重算指定集群统计。
	// Value: namespaceStats | workloadNamespaceStats | all
	AnnotationRefreshClusterStats = "testudo.softcdata.com/refresh-cluster-stats"
	// AnnotationRestartTimestamp 用于指示重跑演练
	// Value: RFC3339 Timestamp
	AnnotationRestartTimestamp = "testudo.softcdata.com/restart-timestamp"
)

type ClusterStatsRefreshType string

const (
	ClusterStatsRefreshTypeNamespaceStats         ClusterStatsRefreshType = "namespaceStats"
	ClusterStatsRefreshTypeWorkloadNamespaceStats ClusterStatsRefreshType = "workloadNamespaceStats"
	ClusterStatsRefreshTypeAll                    ClusterStatsRefreshType = "all"
)

func IsValidClusterStatsRefreshType(value string) bool {
	switch ClusterStatsRefreshType(value) {
	case ClusterStatsRefreshTypeNamespaceStats, ClusterStatsRefreshTypeWorkloadNamespaceStats, ClusterStatsRefreshTypeAll:
		return true
	default:
		return false
	}
}

const (
	AppResourceOriginUser             = "user"
	AppResourceOriginDisasterInstance = "disaster-instance"

	AppResourceOwnerKindUser         = "user"
	AppResourceOwnerKindDataSync     = "datasync"
	AppResourceOwnerKindResourceSync = "resourcesync"
)

const (
	CleanupStrategyDelete           = "delete"
	CleanupStrategyDeleteRequest    = "delete_request"
	CleanupStrategyOwnerReference   = "owner_reference"
	CleanupStrategyBackgroundDelete = "background_delete"
)

// ResolveAppResourceOriginByOwnerRefs 基于 controller owner 计算资源来源标签三元组。
func ResolveAppResourceOriginByOwnerRefs(ownerRefs []metav1.OwnerReference) (origin, ownerKind, ownerName string) {
	origin = AppResourceOriginUser
	ownerKind = AppResourceOwnerKindUser
	ownerName = ""

	for _, ref := range ownerRefs {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}

		switch ref.Kind {
		case "DataSync":
			return AppResourceOriginDisasterInstance, AppResourceOwnerKindDataSync, ref.Name
		case "ResourceSync":
			return AppResourceOriginDisasterInstance, AppResourceOwnerKindResourceSync, ref.Name
		default:
			return AppResourceOriginUser, AppResourceOwnerKindUser, ""
		}
	}

	return origin, ownerKind, ownerName
}

// EnsureAppResourceOriginLabels 根据 ownerRefs 幂等维护来源标签，返回是否发生变更。
func EnsureAppResourceOriginLabels(labels map[string]string, ownerRefs []metav1.OwnerReference) bool {
	if labels == nil {
		return false
	}

	origin, ownerKind, ownerName := ResolveAppResourceOriginByOwnerRefs(ownerRefs)
	changed := false

	if labels[LabelAppResourceOrigin] != origin {
		labels[LabelAppResourceOrigin] = origin
		changed = true
	}

	if labels[LabelAppResourceOwnerKind] != ownerKind {
		labels[LabelAppResourceOwnerKind] = ownerKind
		changed = true
	}

	if ownerName == "" {
		if _, ok := labels[LabelAppResourceOwnerName]; ok {
			delete(labels, LabelAppResourceOwnerName)
			changed = true
		}
	} else if labels[LabelAppResourceOwnerName] != ownerName {
		labels[LabelAppResourceOwnerName] = ownerName
		changed = true
	}

	return changed
}
