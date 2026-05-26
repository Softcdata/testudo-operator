package dependencybackfill

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metadata "github.com/softcdata/testudo-operator/pkg/metadata"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultManagementNamespace = "disaster-system"

type kindResult struct {
	Kind    string
	Scanned int
	Updated int
	Failed  int
	Err     error
}

// Runner executes one-time dependency label backfill for existing resources.
// The process is idempotent: running it repeatedly yields the same labels.
type Runner struct {
	reader              client.Reader
	writer              client.Client
	log                 logr.Logger
	managementNamespace string
}

// StartupRunnable triggers one-time backfill when manager starts.
// It only runs on leader when leader election is enabled.
type StartupRunnable struct {
	runner *Runner
	log    logr.Logger
}

func NewRunner(reader client.Reader, writer client.Client, log logr.Logger) *Runner {
	return &Runner{
		reader:              reader,
		writer:              writer,
		log:                 log,
		managementNamespace: defaultManagementNamespace,
	}
}

func (r *Runner) SetManagementNamespace(namespace string) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = defaultManagementNamespace
	}
	r.managementNamespace = namespace
}

func (r *Runner) effectiveManagementNamespace() string {
	if namespace := strings.TrimSpace(r.managementNamespace); namespace != "" {
		return namespace
	}
	return defaultManagementNamespace
}

func NewStartupRunnable(runner *Runner, log logr.Logger) *StartupRunnable {
	return &StartupRunnable{
		runner: runner,
		log:    log,
	}
}

func (r *StartupRunnable) Start(ctx context.Context) error {
	if r.runner == nil {
		return nil
	}

	if err := r.runner.RunOnce(context.WithoutCancel(ctx)); err != nil {
		// Backfill is best-effort; do not stop manager on partial failure.
		r.log.Error(err, "dependency label backfill finished with errors")
	}
	return nil
}

func (r *StartupRunnable) NeedLeaderElection() bool {
	return true
}

func (r *Runner) RunOnce(ctx context.Context) error {
	if r.reader == nil || r.writer == nil {
		return fmt.Errorf("dependency backfill requires non-nil reader and writer")
	}

	results := []kindResult{
		r.backfillClusters(ctx),
		r.backfillStorageRepositories(ctx),
		r.backfillDisasterPolicies(ctx),
		r.backfillDisasterConfigs(ctx),
		r.backfillDisasterInstances(ctx),
		r.backfillDisasterGroups(ctx),
		r.backfillAppBackups(ctx),
		r.backfillAppRestores(ctx),
		r.backfillDisasterDrills(ctx),
		r.backfillDisasterBackups(ctx),
		r.backfillDisasterOperations(ctx),
		r.backfillDataSyncs(ctx),
		r.backfillResourceSyncs(ctx),
		r.backfillDisasterJobs(ctx),
	}

	var totalScanned, totalUpdated, totalFailed int
	var errs []error
	for _, res := range results {
		totalScanned += res.Scanned
		totalUpdated += res.Updated
		totalFailed += res.Failed

		r.log.Info(
			"dependency label backfill finished for kind",
			"kind", res.Kind,
			"scanned", res.Scanned,
			"updated", res.Updated,
			"failed", res.Failed,
		)
		if res.Err != nil {
			errs = append(errs, res.Err)
		}
	}

	r.log.Info(
		"dependency label backfill summary",
		"kinds", len(results),
		"scanned", totalScanned,
		"updated", totalUpdated,
		"failed", totalFailed,
	)

	return errors.Join(errs...)
}

func (r *Runner) backfillClusters(ctx context.Context) kindResult {
	res := kindResult{Kind: "Cluster"}
	list := &disasterv1.ClusterList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list Cluster: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncCluster(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync Cluster %s: %w", obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update Cluster %s: %w", obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillStorageRepositories(ctx context.Context) kindResult {
	res := kindResult{Kind: "StorageRepository"}
	list := &disasterv1.StorageRepositoryList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list StorageRepository: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncStorageRepository(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync StorageRepository %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update StorageRepository %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDisasterPolicies(ctx context.Context) kindResult {
	res := kindResult{Kind: "DisasterPolicy"}
	list := &disasterv1.DisasterPolicyList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DisasterPolicy: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDisasterPolicy(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DisasterPolicy %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DisasterPolicy %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDisasterConfigs(ctx context.Context) kindResult {
	res := kindResult{Kind: "DisasterConfig"}
	list := &disasterv1.DisasterConfigList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DisasterConfig: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDisasterConfig(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DisasterConfig %s: %w", obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DisasterConfig %s: %w", obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDisasterInstances(ctx context.Context) kindResult {
	res := kindResult{Kind: "DisasterInstance"}
	list := &disasterv1.DisasterInstanceList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DisasterInstance: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDisasterInstance(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DisasterInstance %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DisasterInstance %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDisasterGroups(ctx context.Context) kindResult {
	res := kindResult{Kind: "DisasterGroup"}
	list := &disasterv1.DisasterGroupList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DisasterGroup: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDisasterGroup(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DisasterGroup %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DisasterGroup %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillAppBackups(ctx context.Context) kindResult {
	res := kindResult{Kind: "AppBackup"}
	list := &disasterv1.AppBackupList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list AppBackup: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncAppBackup(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync AppBackup %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update AppBackup %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillAppRestores(ctx context.Context) kindResult {
	res := kindResult{Kind: "AppRestore"}
	list := &disasterv1.AppRestoreList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list AppRestore: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncAppRestore(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync AppRestore %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update AppRestore %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDisasterDrills(ctx context.Context) kindResult {
	res := kindResult{Kind: "DisasterDrill"}
	list := &disasterv1.DisasterDrillList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DisasterDrill: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDisasterDrill(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DisasterDrill %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DisasterDrill %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDisasterBackups(ctx context.Context) kindResult {
	res := kindResult{Kind: "DisasterBackup"}
	list := &disasterv1.DisasterBackupList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DisasterBackup: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDisasterBackup(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DisasterBackup %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DisasterBackup %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDisasterOperations(ctx context.Context) kindResult {
	res := kindResult{Kind: "DisasterOperation"}
	list := &disasterv1.DisasterOperationList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DisasterOperation: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDisasterOperation(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DisasterOperation %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DisasterOperation %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDataSyncs(ctx context.Context) kindResult {
	res := kindResult{Kind: "DataSync"}
	list := &disasterv1.DataSyncList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DataSync: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDataSync(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DataSync %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DataSync %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillResourceSyncs(ctx context.Context) kindResult {
	res := kindResult{Kind: "ResourceSync"}
	list := &disasterv1.ResourceSyncList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list ResourceSync: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncResourceSync(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync ResourceSync %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update ResourceSync %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) backfillDisasterJobs(ctx context.Context) kindResult {
	res := kindResult{Kind: "DisasterJob"}
	list := &disasterv1.DisasterJobList{}
	if err := r.reader.List(ctx, list); err != nil {
		res.Err = fmt.Errorf("list DisasterJob: %w", err)
		return res
	}
	res.Scanned = len(list.Items)
	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		changed, err := r.syncDisasterJob(ctx, obj)
		if err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("sync DisasterJob %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		if !changed {
			continue
		}
		if err := r.writer.Update(ctx, obj); err != nil {
			res.Failed++
			errs = append(errs, fmt.Errorf("update DisasterJob %s/%s: %w", obj.Namespace, obj.Name, err))
			continue
		}
		res.Updated++
	}
	res.Err = errors.Join(errs...)
	return res
}

func (r *Runner) syncCluster(_ context.Context, cluster *disasterv1.Cluster) (bool, error) {
	if cluster.Labels == nil {
		cluster.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(cluster.Labels, string(cluster.UID))
	_, depChanged := metadata.RebuildDependencyToLabels(cluster.Labels, nil)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncStorageRepository(_ context.Context, sr *disasterv1.StorageRepository) (bool, error) {
	if sr.Labels == nil {
		sr.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(sr.Labels, string(sr.UID))
	_, depChanged := metadata.RebuildDependencyToLabels(sr.Labels, nil)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDisasterPolicy(ctx context.Context, policy *disasterv1.DisasterPolicy) (bool, error) {
	if policy.Labels == nil {
		policy.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(policy.Labels, string(policy.UID))
	edges := make([]metadata.DependencyEdge, 0, 1)

	if storageName := policy.Labels[metadata.LabelStorageRepositoryName]; storageName != "" {
		if token, ok := r.resolveStorageToken(ctx, policy.Namespace, storageName); ok {
			edges = append(edges, metadata.DependencyEdge{
				TargetToken:  token,
				RelationCode: "label.storageRepositoryName",
			})
		}
	}

	_, depChanged := metadata.RebuildDependencyToLabels(policy.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDisasterConfig(ctx context.Context, dc *disasterv1.DisasterConfig) (bool, error) {
	if dc.Labels == nil {
		dc.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(dc.Labels, string(dc.UID))
	edges := make([]metadata.DependencyEdge, 0, 5)

	if token, ok := r.resolveClusterToken(ctx, dc.Spec.SourceCluster); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.sourceCluster",
		})
	}
	if token, ok := r.resolveClusterToken(ctx, dc.Spec.TargetCluster); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.targetCluster",
		})
	}
	if token, ok := r.resolveStorageToken(ctx, r.effectiveManagementNamespace(), dc.Spec.StorageRepository); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.storageRepository",
		})
	}

	policyNamespace := dc.Namespace
	if policyNamespace == "" {
		policyNamespace = r.effectiveManagementNamespace()
	}
	if token, ok := r.resolvePolicyToken(ctx, policyNamespace, dc.Spec.DataSyncPolicy); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.dataSyncPolicy",
		})
	}
	if token, ok := r.resolvePolicyToken(ctx, policyNamespace, dc.Spec.ResourceSyncPolicy); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.resourceSyncPolicy",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(dc.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDisasterInstance(ctx context.Context, instance *disasterv1.DisasterInstance) (bool, error) {
	if instance.Labels == nil {
		instance.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(instance.Labels, string(instance.UID))
	edges := make([]metadata.DependencyEdge, 0, 1)

	if token, ok := r.resolveDisasterConfigToken(ctx, instance.Spec.Config); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.config",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(instance.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDisasterGroup(ctx context.Context, group *disasterv1.DisasterGroup) (bool, error) {
	if group.Labels == nil {
		group.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(group.Labels, string(group.UID))
	edges := make([]metadata.DependencyEdge, 0)

	for _, level := range group.Spec.Levels {
		for _, instanceName := range level {
			if token, ok := r.resolveDisasterInstanceToken(ctx, group.Namespace, instanceName); ok {
				edges = append(edges, metadata.DependencyEdge{
					TargetToken:  token,
					RelationCode: "spec.levels",
				})
			}
		}
	}

	_, depChanged := metadata.RebuildDependencyToLabels(group.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncAppBackup(ctx context.Context, ab *disasterv1.AppBackup) (bool, error) {
	if ab.Labels == nil {
		ab.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(ab.Labels, string(ab.UID))
	edges := make([]metadata.DependencyEdge, 0, 3)

	if token, ok := r.resolveClusterToken(ctx, ab.Spec.Cluster); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.cluster",
		})
	}
	if token, ok := r.resolvePolicyToken(ctx, ab.Namespace, ab.Spec.DisasterPolicy); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.disasterPolicy",
		})
	}
	if token, ok := r.resolveStorageToken(ctx, ab.Namespace, ab.Spec.Template.StorageLocation); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.template.storageLocation",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(ab.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncAppRestore(ctx context.Context, ar *disasterv1.AppRestore) (bool, error) {
	if ar.Labels == nil {
		ar.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(ar.Labels, string(ar.UID))
	edges := make([]metadata.DependencyEdge, 0, 4)

	if token, ok := r.resolveClusterToken(ctx, ar.Spec.Cluster); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.cluster",
		})
	}
	if token, ok := r.resolveAppBackupToken(ctx, ar.Namespace, ar.Spec.BackupSource); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.backupSource",
		})
	}
	if token, ok := r.resolveClusterToken(ctx, ar.Spec.SourceCluster); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.sourceCluster",
		})
	}
	if token, ok := r.resolveStorageToken(ctx, ar.Namespace, ar.Spec.StorageRepository); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.storageRepository",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(ar.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDisasterDrill(ctx context.Context, drill *disasterv1.DisasterDrill) (bool, error) {
	if drill.Labels == nil {
		drill.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(drill.Labels, string(drill.UID))
	edges := make([]metadata.DependencyEdge, 0, 2)

	if token, ok := r.resolveDisasterInstanceToken(ctx, drill.Namespace, drill.Spec.InstanceName); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.instanceName",
		})
	}
	if token, ok := r.resolveDisasterGroupToken(ctx, drill.Namespace, drill.Spec.GroupName); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.groupName",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(drill.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDisasterBackup(ctx context.Context, db *disasterv1.DisasterBackup) (bool, error) {
	if db.Labels == nil {
		db.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(db.Labels, string(db.UID))
	edges := make([]metadata.DependencyEdge, 0, 1)

	if token, ok := r.resolveDisasterConfigToken(ctx, db.Spec.DisasterConfig); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.disasterConfig",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(db.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDisasterOperation(ctx context.Context, op *disasterv1.DisasterOperation) (bool, error) {
	if op.Labels == nil {
		op.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(op.Labels, string(op.UID))
	edges := make([]metadata.DependencyEdge, 0, 2)

	if token, ok := r.resolveDisasterInstanceToken(ctx, op.Namespace, op.Spec.InstanceName); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.instanceName",
		})
	}
	if token, ok := r.resolveDisasterGroupToken(ctx, op.Namespace, op.Spec.GroupName); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.groupName",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(op.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDataSync(ctx context.Context, ds *disasterv1.DataSync) (bool, error) {
	if ds.Labels == nil {
		ds.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(ds.Labels, string(ds.UID))
	edges := make([]metadata.DependencyEdge, 0, 1)

	if token, ok := r.resolveDisasterInstanceToken(ctx, ds.Namespace, ds.Spec.Instance); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.instance",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(ds.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncResourceSync(ctx context.Context, rs *disasterv1.ResourceSync) (bool, error) {
	if rs.Labels == nil {
		rs.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(rs.Labels, string(rs.UID))
	edges := make([]metadata.DependencyEdge, 0, 1)

	if token, ok := r.resolveDisasterInstanceToken(ctx, rs.Namespace, rs.Spec.Instance); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.instance",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(rs.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) syncDisasterJob(ctx context.Context, dj *disasterv1.DisasterJob) (bool, error) {
	if dj.Labels == nil {
		dj.Labels = make(map[string]string)
	}
	_, _, tokenChanged := metadata.EnsureDependencyTokenLabel(dj.Labels, string(dj.UID))
	edges := make([]metadata.DependencyEdge, 0, 2)

	if token, ok := r.resolveDisasterBackupToken(ctx, dj.Namespace, dj.Spec.DisasterBackup); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "spec.disasterBackup",
		})
	}
	if token, ok := r.resolvePolicyToken(ctx, dj.Namespace, dj.Labels[metadata.LabelDisasterPolicyName]); ok {
		edges = append(edges, metadata.DependencyEdge{
			TargetToken:  token,
			RelationCode: "label.disasterPolicyName",
		})
	}

	_, depChanged := metadata.RebuildDependencyToLabels(dj.Labels, edges)
	return tokenChanged || depChanged, nil
}

func (r *Runner) resolveClusterToken(ctx context.Context, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	obj := &disasterv1.Cluster{}
	if err := r.reader.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		return "", false
	}
	return metadata.BuildDependencyToken(string(obj.UID)), true
}

func (r *Runner) resolveStorageToken(ctx context.Context, namespace, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	obj := &disasterv1.StorageRepository{}
	err := r.reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)
	managementNamespace := r.effectiveManagementNamespace()
	if apierrors.IsNotFound(err) && namespace != managementNamespace {
		err = r.reader.Get(ctx, types.NamespacedName{Namespace: managementNamespace, Name: name}, obj)
	}
	if err != nil {
		return "", false
	}
	return metadata.BuildDependencyToken(string(obj.UID)), true
}

func (r *Runner) resolvePolicyToken(ctx context.Context, namespace, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	obj := &disasterv1.DisasterPolicy{}
	if err := r.reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		return "", false
	}
	return metadata.BuildDependencyToken(string(obj.UID)), true
}

func (r *Runner) resolveDisasterConfigToken(ctx context.Context, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	obj := &disasterv1.DisasterConfig{}
	if err := r.reader.Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
		return "", false
	}
	return metadata.BuildDependencyToken(string(obj.UID)), true
}

func (r *Runner) resolveDisasterInstanceToken(ctx context.Context, namespace, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	obj := &disasterv1.DisasterInstance{}
	if err := r.reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		return "", false
	}
	return metadata.BuildDependencyToken(string(obj.UID)), true
}

func (r *Runner) resolveDisasterGroupToken(ctx context.Context, namespace, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	obj := &disasterv1.DisasterGroup{}
	if err := r.reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		return "", false
	}
	return metadata.BuildDependencyToken(string(obj.UID)), true
}

func (r *Runner) resolveAppBackupToken(ctx context.Context, namespace, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	obj := &disasterv1.AppBackup{}
	if err := r.reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		return "", false
	}
	return metadata.BuildDependencyToken(string(obj.UID)), true
}

func (r *Runner) resolveDisasterBackupToken(ctx context.Context, namespace, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	obj := &disasterv1.DisasterBackup{}
	if err := r.reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		return "", false
	}
	return metadata.BuildDependencyToken(string(obj.UID)), true
}
