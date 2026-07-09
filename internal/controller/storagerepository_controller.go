/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	runtimecfg "github.com/softcdata/testudo-operator/internal/controller/runtimeconfig"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// S3API defines the interface for S3 operations used by the controller
type S3API interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// S3ClientFactory defines the interface for creating S3 clients
type S3ClientFactory interface {
	NewS3Client(ctx context.Context, sr *disasterv1.StorageRepository, settings StorageRuntimeSettings) (S3API, error)
}

// DefaultS3ClientFactory is the default implementation
type DefaultS3ClientFactory struct{}

func (f *DefaultS3ClientFactory) NewS3Client(ctx context.Context, sr *disasterv1.StorageRepository, settings StorageRuntimeSettings) (S3API, error) {
	loadOptions := []func(*config.LoadOptions) error{
		config.WithRegion(settings.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			sr.Spec.AccessKey,
			sr.Spec.SecretKey,
			"",
		)),
	}
	httpClient, err := buildStorageHTTPClient(settings.CACert)
	if err != nil {
		return nil, err
	}
	if httpClient != nil {
		loadOptions = append(loadOptions, config.WithHTTPClient(httpClient))
	}

	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		if settings.Endpoint != "" {
			o.BaseEndpoint = aws.String(settings.Endpoint)
		}
		o.UsePathStyle = settings.UsePathStyle
	}), nil
}

// StorageRepositoryReconciler reconciles a StorageRepository object
// StorageRepositoryReconciler reconciles a StorageRepository object
type StorageRepositoryReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
	S3Factory S3ClientFactory
}

func storageRepositoryRequeueInterval() time.Duration {
	return runtimecfg.SnapshotCurrent().StorageRuntime.RequeueInterval
}

// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=storagerepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=storagerepositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=testudo.softcdata.com,resources=storagerepositories/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the StorageRepository object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *StorageRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	sr := &disasterv1.StorageRepository{}
	err := r.Get(ctx, req.NamespacedName, sr)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch StorageRepository")
		return ctrl.Result{RequeueAfter: storageRepositoryRequeueInterval()}, nil
	}
	logger = logger.WithValues(TraceIDKey, sr.Annotations[AnnotationTraceID])
	ctx = context.WithValue(ctx, TraceIDKey, sr.Annotations[AnnotationTraceID])

	// Handle deletion
	if !sr.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, sr)
	}

	// Sync dependency labels
	if r.syncDependencyLabels(sr) {
		if err := r.Update(ctx, sr); err != nil {
			logger.Error(err, "failed to update dependency labels")
			return ctrl.Result{RequeueAfter: storageRepositoryRequeueInterval()}, nil
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Add finalizer if not present
	currentMetadataHash := r.calculateMetadataHash(sr)
	specChanged := false
	metadataChanged := false
	if !controllerutil.ContainsFinalizer(sr, LabelStorageFinalizer) {
		controllerutil.AddFinalizer(sr, LabelStorageFinalizer)
		if err := r.Update(ctx, sr); err != nil {
			logger.Error(err, "failed to add finalizer")
			return ctrl.Result{RequeueAfter: storageRepositoryRequeueInterval()}, nil
		}
		// Emit 创建存储 Started event
		traceID := sr.Annotations[AnnotationTraceID]
		user := sr.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, sr,
			fmt.Sprintf("创建存储 %s", sr.Name), "-", user, traceID, "开始创建存储")
	} else {
		// Check for Spec change OR Metadata change
		specChanged = sr.Generation > sr.Status.ObservedGeneration && sr.Status.ObservedGeneration > 0
		metadataChanged = currentMetadataHash != sr.Status.ObservedMetadataHash && sr.Status.ObservedMetadataHash != ""

		if specChanged || metadataChanged {
			// Emit 编辑存储 Started event
			traceID := sr.Annotations[AnnotationTraceID]
			user := sr.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, sr,
				fmt.Sprintf("编辑存储 %s", sr.Name), "-", user, traceID, "开始更新存储配置")
		}

		// Fast Path for Metadata-only updates (mirrors Cluster logic)
		if metadataChanged && !specChanged {
			traceID := sr.Annotations[AnnotationTraceID]
			user := sr.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			now := metav1.Now()
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, sr,
				fmt.Sprintf("编辑存储 %s", sr.Name), "-", helper.TaskStatusSuccess,
				&now, &now, user, traceID, "存储元数据更新完成")
			sr.Status.ObservedMetadataHash = currentMetadataHash
		}
	}

	// Status update is handled explicitly in success/failure paths to ensure consistency
	// Validation runs at a fixed interval to keep status fresh.

	// 记录初始状态，用于区分“首次创建/恢复”和“定期检查”
	wasAvailable := sr.Status.Status == disasterv1.StorageRepositoryStatusAvailable
	shouldReportProgress := specChanged || sr.Status.ObservedGeneration == 0
	previousStatus := sr.Status.Status
	validationTaskName := fmt.Sprintf("创建存储 %s", sr.Name)
	if specChanged {
		validationTaskName = fmt.Sprintf("编辑存储 %s", sr.Name)
	}

	if err := r.ValidateS3Configuration(ctx, sr, validationTaskName, shouldReportProgress); err != nil {
		sr.Status.Status = disasterv1.StorageRepositoryStatusUnavailable
		now := metav1.Now()
		sr.Status.LastCheckTime = &now
		helper.SetStatusError(&sr.Status, "ValidationFailed", err.Error())

		// Validation 失败后，消费本轮变更，避免后续重试重复触发“编辑存储”事件。
		sr.Status.ObservedGeneration = sr.Generation
		sr.Status.ObservedMetadataHash = currentMetadataHash

		// 失败事件仅在“状态切换到失败”或“本次确有新配置变更”时发射，避免刷屏。
		if previousStatus != disasterv1.StorageRepositoryStatusUnavailable || specChanged {
			traceID := sr.Annotations[AnnotationTraceID]
			user := sr.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			failedTaskName := fmt.Sprintf("创建存储 %s", sr.Name)
			failedMessage := sr.Status.Message
			if specChanged {
				failedTaskName = fmt.Sprintf("编辑存储 %s", sr.Name)
			}
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, sr,
				failedTaskName, "local", helper.TaskStatusFailed,
				&sr.CreationTimestamp, &now, user, traceID, failedMessage, sr.Status.Reason)
			sr.Status.LastEventPhase = string(disasterv1.StorageRepositoryStatusUnavailable)
		}

		if previousStatus != disasterv1.StorageRepositoryStatusUnavailable || specChanged {
			r.Recorder.Event(sr, "Warning", "ValidationFailed", err.Error())
		}
		if updateErr := r.Status().Update(ctx, sr); updateErr != nil {
			logger.Error(updateErr, "unable to update StorageRepository status after validation failure")
			return ctrl.Result{RequeueAfter: storageRepositoryRequeueInterval()}, nil
		}
		return ctrl.Result{RequeueAfter: storageRepositoryRequeueInterval()}, nil
	}
	sr.Status.Status = disasterv1.StorageRepositoryStatusAvailable
	now := metav1.Now()
	sr.Status.LastCheckTime = &now
	sr.Status.Reason = "Available"
	sr.Status.Message = "S3 configuration validated successfully"

	// Emit Edit Finished event only if we detected a change AND we reached here successfully
	if !wasAvailable && sr.Status.ReadyTimestamp == nil {
		// Existing logic: Created
		traceID := sr.Annotations[AnnotationTraceID]
		user := sr.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		sr.Status.ReadyTimestamp = &now
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, sr,
			fmt.Sprintf("创建存储 %s", sr.Name), "local", helper.TaskStatusSuccess,
			&sr.CreationTimestamp, &now, user, traceID, "存储创建完成")
		sr.Status.LastEventPhase = string(disasterv1.StorageRepositoryStatusAvailable)
	} else {
		// Edit Finished
		// Note: metadataChanged is not checked here because if it was the only change, we already handled it in Fast Path.
		// If specChanged is true, we emit Finished now.
		if specChanged {
			traceID := sr.Annotations[AnnotationTraceID]
			user := sr.Annotations[AnnotationUser]
			if user == "" {
				user = "system"
			}
			now := metav1.Now()
			helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, sr,
				fmt.Sprintf("编辑存储 %s", sr.Name), "-", helper.TaskStatusSuccess,
				&now, &now, user, traceID, "存储配置更新完成")
		}
	}

	// Update Observed fields
	sr.Status.ObservedGeneration = sr.Generation
	sr.Status.ObservedMetadataHash = currentMetadataHash
	sr.Status.LastEventPhase = string(disasterv1.StorageRepositoryStatusAvailable)

	if previousStatus != disasterv1.StorageRepositoryStatusAvailable || specChanged {
		r.Recorder.Event(sr, "Normal", "Available", "S3 configuration validated successfully")
	}

	// Calculate S3 usage statistics
	if err := r.calculateAndUpdateS3Usage(ctx, sr); err != nil {
		logger.Error(err, "failed to calculate S3 usage stats")
		// We still update status below, even if calculation failed this time.
	}

	if err := r.Status().Update(ctx, sr); err != nil {
		logger.Error(err, "unable to update StorageRepository status")
		return ctrl.Result{RequeueAfter: storageRepositoryRequeueInterval()}, nil
	}
	return ctrl.Result{RequeueAfter: storageRepositoryRequeueInterval()}, nil
}

// countBackupsInPrefix helper counts backups in a specific exact prefix
func (r *StorageRepositoryReconciler) countBackupsInPrefix(ctx context.Context, svc S3API, bucket, exactPrefix string) (int64, error) {
	var count int64
	var continuationToken *string
	for {
		resp, err := svc.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(exactPrefix),
			Delimiter:         aws.String("/"),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return 0, err
		}
		count += int64(len(resp.CommonPrefixes))
		if resp.IsTruncated != nil && *resp.IsTruncated {
			continuationToken = resp.NextContinuationToken
		} else {
			break
		}
	}
	return count, nil
}

// calculateAndUpdateS3Usage polls S3 to compute the total size and backup count.
func (r *StorageRepositoryReconciler) calculateAndUpdateS3Usage(ctx context.Context, sr *disasterv1.StorageRepository) error {
	settings, err := resolveStorageRuntimeSettings(ctx, r.Client, sr)
	if err != nil {
		return err
	}

	svc, err := r.S3Factory.NewS3Client(ctx, sr, settings)
	if err != nil {
		return err
	}

	// 1. Calculate Used Space
	var totalSize int64
	var continuationToken *string
	for {
		resp, err := svc.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(sr.Spec.Bucket),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return err
		}
		for _, obj := range resp.Contents {
			if obj.Size != nil {
				totalSize += *obj.Size
			}
		}
		if resp.IsTruncated != nil && *resp.IsTruncated {
			continuationToken = resp.NextContinuationToken
		} else {
			break
		}
	}

	// 2. Calculate Total Backup Count
	var backupCount int64

	// First, check if there are backups at the root of the bucket (no velero prefix)
	c, err := r.countBackupsInPrefix(ctx, svc, sr.Spec.Bucket, "backups/")
	if err == nil {
		backupCount += c
	}

	// Next, iterate through all root level directories (like c170/, c171/) and look for backups/
	continuationToken = nil
	for {
		resp, err := svc.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(sr.Spec.Bucket),
			Delimiter:         aws.String("/"),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return err
		}
		for _, p := range resp.CommonPrefixes {
			prefix := *p.Prefix
			if prefix != "backups/" && prefix != "restores/" {
				c, err := r.countBackupsInPrefix(ctx, svc, sr.Spec.Bucket, prefix+"backups/")
				if err == nil {
					backupCount += c
				}
			}
		}
		if resp.IsTruncated != nil && *resp.IsTruncated {
			continuationToken = resp.NextContinuationToken
		} else {
			break
		}
	}

	// 3. Update Status
	sr.Status.UsedSpaceBytes = totalSize
	sr.Status.TotalBackupCount = backupCount
	return nil
}

// calculateMetadataHash calculates a deterministic hash of the filtered metadata (labels & annotations)
func (r *StorageRepositoryReconciler) calculateMetadataHash(sr *disasterv1.StorageRepository) string {
	// 1. Filter Labels
	labels := make(map[string]string)
	for k, v := range sr.Labels {
		// Exclude system/controller managed labels
		if k == LabelStorageFinalizer {
			continue
		}
		labels[k] = v
	}

	// 2. Filter Annotations
	annotations := make(map[string]string)
	for k, v := range sr.Annotations {
		if k == AnnotationTraceID ||
			k == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		}
		annotations[k] = v
	}

	// 3. Serialize to map
	data := struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	}{
		Labels:      labels,
		Annotations: annotations,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return ""
	}

	// 4. Hash
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:])
}

func (r *StorageRepositoryReconciler) handleDelete(ctx context.Context, sr *disasterv1.StorageRepository) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Legacy finalizer deletion protection is temporarily disabled.
	//
	// The old behavior blocked finalizer removal when the StorageRepository was
	// still referenced by DisasterPolicy resources. We are temporarily bypassing
	// that logic so deletion can proceed, and will re-introduce the new
	// case-based deletion rules separately.
	/*
		policies := &disasterv1.DisasterPolicyList{}
		if err := r.List(ctx, policies, client.MatchingLabels{LabelStorageRepositoryName: sr.Name}); err != nil {
			return ctrl.Result{}, err
		}

		if len(policies.Items) > 0 {
			message := fmt.Sprintf("Cannot delete StorageRepository %s because it is used by %d DisasterPolicies", sr.Name, len(policies.Items))
			logger.Info(message)
			sr.Status.Reason = "DeletionBlocked"
			sr.Status.Message = message
			if err := r.Status().Update(ctx, sr); err != nil {
				return ctrl.Result{}, err
			}
			// Requeue to check again later
			return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
		}
	*/

	// 删除存储 Started 事件
	if sr.Status.LastEventPhase != "Deleting" {
		traceID := sr.Annotations[AnnotationTraceID]
		user := sr.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		helper.ReportTaskStartedWithClient(ctx, r.Client, r.Scheme, sr,
			fmt.Sprintf("删除存储 %s", sr.Name), "-", user, traceID, "开始删除存储")
		sr.Status.LastEventPhase = "Deleting"
		if err := r.Status().Update(ctx, sr); err != nil {
			logger.Error(err, "unable to update status for deleting event")
			// continue anyway
		}
	}

	// 发射删除完成事件（必须在移除 Finalizer 之前！）
	{
		traceID := sr.Annotations[AnnotationTraceID]
		user := sr.Annotations[AnnotationUser]
		if user == "" {
			user = "system"
		}
		now := metav1.Now()
		helper.ReportTaskFinishedWithClient(ctx, r.Client, r.Scheme, sr,
			fmt.Sprintf("删除存储 %s", sr.Name), "-", helper.TaskStatusSuccess,
			sr.DeletionTimestamp, &now, user, traceID, "存储删除完成")
	}

	// Remove finalizer
	if controllerutil.ContainsFinalizer(sr, LabelStorageFinalizer) {
		controllerutil.RemoveFinalizer(sr, LabelStorageFinalizer)
		if err := r.Update(ctx, sr); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.S3Factory == nil {
		r.S3Factory = &DefaultS3ClientFactory{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&disasterv1.StorageRepository{}).
		// For(&velerov1.BackupStorageLocation{}).
		WithEventFilter(IgnoreStatusUpdatesPredicate{}). // 应用事件过滤器
		Named("storagerepository").
		Complete(r)
}

// SetupWithManagerAndFactory sets up the controller with a custom S3 factory
func (r *StorageRepositoryReconciler) SetupWithManagerAndFactory(mgr ctrl.Manager, factory S3ClientFactory) error {
	r.S3Factory = factory
	return r.SetupWithManager(mgr)
}

// ValidateS3Configuration 验证 S3 配置是否有效
func (r *StorageRepositoryReconciler) ValidateS3Configuration(ctx context.Context, sr *disasterv1.StorageRepository, taskName string, shouldReportProgress bool) error {
	logger := logf.FromContext(ctx)

	traceID := sr.Annotations[AnnotationTraceID]
	user := sr.Annotations[AnnotationUser]
	if user == "" {
		user = "system"
	}

	if shouldReportProgress {
		helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, sr, taskName, "-", user, traceID, "正在连接对象存储服务...")
	}

	settings, err := resolveStorageRuntimeSettings(ctx, r.Client, sr)
	if err != nil {
		return err
	}

	svc, err := r.S3Factory.NewS3Client(ctx, sr, settings)
	if err != nil {
		return err
	}

	// 尝试列出存储桶中的对象来验证配置
	_, err = svc.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(sr.Spec.Bucket),
	})
	if err != nil {
		var errorType *types.NotFound
		if errors.As(err, &errorType) {
			logger.Info("Bucket does not exist, creating...",
				"bucket", sr.Spec.Bucket,
				"endpoint", sr.Spec.Endpoint,
				"region", sr.Spec.Region)
			// 存储桶不存在，尝试创建存储桶
			if shouldReportProgress {
				helper.ReportTaskProgressWithClient(ctx, r.Client, r.Scheme, sr, taskName, "-", user, traceID, fmt.Sprintf("存储桶 %s 不存在，正在创建...", sr.Spec.Bucket))
			}
			_, err = svc.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(sr.Spec.Bucket),
			})
			if err != nil {
				return err
			}
		} else {
			logger.Error(err, "S3 configuration validation failed",
				"bucket", sr.Spec.Bucket,
				"endpoint", sr.Spec.Endpoint,
				"region", sr.Spec.Region)
			return fmt.Errorf("failed to access bucket %s: %w", sr.Spec.Bucket, err)
		}

	}

	logger.Info("S3 configuration validated successfully",
		"bucket", sr.Spec.Bucket,
		"endpoint", sr.Spec.Endpoint,
		"region", sr.Spec.Region)
	// Validation success
	return nil
}

func (r *StorageRepositoryReconciler) syncDependencyLabels(sr *disasterv1.StorageRepository) bool {
	if sr.Labels == nil {
		sr.Labels = make(map[string]string)
	}
	_, _, tokenChanged := EnsureDependencyTokenLabel(sr.Labels, string(sr.UID))
	_, depChanged := RebuildDependencyToLabels(sr.Labels, nil)
	return tokenChanged || depChanged
}
