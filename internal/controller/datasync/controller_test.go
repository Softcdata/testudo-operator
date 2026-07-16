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

package datasync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrlcommon "github.com/softcdata/testudo-operator/internal/controller"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDataSyncController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DataSync Controller Suite")
}

var _ = Describe("DataSync Controller", func() {
	var (
		ctx           context.Context
		r             *DataSyncReconciler
		fakeClient    client.Client
		s             *runtime.Scheme
		recorder      *record.FakeRecorder
		syncScheduler *scheduler.SyncScheduler
	)

	BeforeEach(func() {
		ctx = context.Background()

		s = runtime.NewScheme()
		Expect(scheme.AddToScheme(s)).To(Succeed())
		Expect(disasterv1.AddToScheme(s)).To(Succeed())

		recorder = record.NewFakeRecorder(100)

		var err error
		syncScheduler, err = scheduler.NewSyncScheduler()
		Expect(err).NotTo(HaveOccurred())
		syncScheduler.Start()
	})

	AfterEach(func() {
		_ = syncScheduler.Shutdown()
	})

	sourceClientWithObjects := func(objects ...client.Object) client.Client {
		return fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(objects...).
			Build()
	}

	sourceClientWithPVC := func(namespace, name string) client.Client {
		return sourceClientWithObjects(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		})
	}

	createTestDataSync := func(name, namespace string) *disasterv1.DataSync {
		return &disasterv1.DataSync{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				Finalizers: []string{"testudo.softcdata.com/datasync-finalizer"},
			},
			Spec: disasterv1.DataSyncSpec{
				Instance: "test-instance",
				Trigger: disasterv1.TriggerSpec{
					Schedule: "*/1 * * * *", // 每分钟
				},
			},
			Status: disasterv1.DataSyncStatus{
				State:        disasterv1.DataSyncStateReady,
				LastSyncTime: &metav1.Time{Time: time.Now()},
			},
		}
	}

	Describe("Cron 调度注册", func() {
		It("应该将 Cron 任务注册到调度器", func() {
			dataSync := createTestDataSync("test-ds", "default")
			// 确保 State 不为空
			dataSync.Status.State = disasterv1.DataSyncStateReady

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			// 调谐
			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-ds",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// 验证调度器中是否有任务
			Expect(syncScheduler.HasJob("default", "test-ds")).To(BeTrue())
		})

		It("如果暂停，应该从调度器移除任务", func() {
			dataSync := createTestDataSync("test-ds", "default")
			dataSync.Spec.Paused = true // 暂停
			dataSync.Status.State = disasterv1.DataSyncStateReady

			// 先手动添加一个任务模拟已存在
			err := syncScheduler.AddOrUpdate("default", "test-ds", "*/1 * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())
			Expect(syncScheduler.HasJob("default", "test-ds")).To(BeTrue())

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			// 调谐
			_, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-ds",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// 验证调度器中任务已被移除
			Expect(syncScheduler.HasJob("default", "test-ds")).To(BeFalse())
		})

		It("如果 schedule 被清空，应该从调度器移除残留任务", func() {
			dataSync := createTestDataSync("test-ds", "default")
			dataSync.Spec.Trigger.Schedule = ""
			dataSync.Status.State = disasterv1.DataSyncStateReady

			err := syncScheduler.AddOrUpdate("default", "test-ds", "*/1 * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())
			Expect(syncScheduler.HasJob("default", "test-ds")).To(BeTrue())

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			_, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-ds",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(syncScheduler.HasJob("default", "test-ds")).To(BeFalse())
		})

		It("残留 cron 回调在 schedule 清空后不应该写入手动触发器", func() {
			dataSync := createTestDataSync("test-ds", "default")
			dataSync.Spec.Trigger.Schedule = ""
			dataSync.Spec.Trigger.Manual = ""

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			r.triggerSync("default", "test-ds")

			latest := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-ds", Namespace: "default"}, latest)).To(Succeed())
			Expect(latest.Spec.Trigger.Manual).To(BeEmpty())
		})

		It("残留 cron 回调在暂停后不应该写入手动触发器", func() {
			dataSync := createTestDataSync("test-ds", "default")
			dataSync.Spec.Paused = true
			dataSync.Spec.Trigger.Manual = ""

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			r.triggerSync("default", "test-ds")

			latest := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-ds", Namespace: "default"}, latest)).To(Succeed())
			Expect(latest.Spec.Trigger.Manual).To(BeEmpty())
		})

		It("进行中时 cron 回调不应该覆盖手动触发器", func() {
			dataSync := createTestDataSync("test-ds", "default")
			dataSync.Spec.Trigger.Manual = "2026-05-14T02:00:00Z"
			dataSync.Status.State = disasterv1.DataSyncStateInProgress

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			r.triggerSync("default", "test-ds")

			latest := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-ds", Namespace: "default"}, latest)).To(Succeed())
			Expect(latest.Spec.Trigger.Manual).To(Equal("2026-05-14T02:00:00Z"))
		})
	})

	Describe("同步执行 (Real Logic)", func() {
		createFullEnvironment := func(name, namespace string) (*disasterv1.DataSync, *disasterv1.DisasterInstance, *disasterv1.DisasterConfig) {
			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: namespace},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster:  "cluster-A",
					TargetCluster:  "cluster-B",
					DataSyncPolicy: "policy-daily",
				},
			}
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test-instance", Namespace: namespace},
				Spec: disasterv1.DisasterInstanceSpec{
					Config:     "test-config",
					Namespaces: []string{"app-ns"},
				},
			}
			ds := &disasterv1.DataSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Finalizers: []string{"testudo.softcdata.com/datasync-finalizer"},
				},
				Spec: disasterv1.DataSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Schedule: "*/1 * * * *"},
				},
				Status: disasterv1.DataSyncStatus{
					State: disasterv1.DataSyncStateReady,
				},
			}
			return ds, instance, config
		}

		It("应该创建 AppBackup 及其 OwnerReference", func() {
			dataSync, instance, config := createFullEnvironment("test-ds", "default")
			dataSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			// 第一次调用：更新状态到 InProgress
			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeTrue())

			// 第二次调用：创建 AppBackup
			res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeTrue())

			// 验证 AppBackup 创建
			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateInProgress))

			backup := &disasterv1.AppBackup{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "ds-" + dataSync.Name, Namespace: "default"}, backup)).To(Succeed())
			Expect(backup.Spec.Cluster).To(Equal("cluster-A"))
			Expect(backup.OwnerReferences).To(HaveLen(1))
			Expect(backup.OwnerReferences[0].Name).To(Equal(dataSync.Name))
		})

		It("无 PVC 首次同步应该直接 skipped success 且不创建 AppBackup/AppRestore", func() {
			dataSync, instance, config := createFullEnvironment("no-pvc-ds", "default")
			dataSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config).
				WithStatusSubresource(dataSync, &disasterv1.BackupRestoreStatistics{}).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithObjects()},
			}

			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "no-pvc-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeFalse())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "no-pvc-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateReady))
			Expect(updated.Status.LastSyncTime).NotTo(BeNil())
			Expect(updated.Status.Reason).To(BeEmpty())
			Expect(updated.Status.Message).To(BeEmpty())
			Expect(updated.Status.History).To(HaveLen(1))
			Expect(updated.Status.History[0].Status).To(Equal(dataSyncHistoryStatusSkipped))
			Expect(updated.Status.History[0].BackupName).To(BeEmpty())
			Expect(updated.Status.History[0].RestoreName).To(BeEmpty())

			condition := apimeta.FindStatusCondition(updated.Status.Conditions, dataSyncConditionNoDataVolumes)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(dataSyncReasonNoPVCFound))

			backup := &disasterv1.AppBackup{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "ds-no-pvc-ds", Namespace: "default"}, backup)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			restoreList := &disasterv1.AppRestoreList{}
			Expect(fakeClient.List(ctx, restoreList, client.InNamespace("default"))).To(Succeed())
			Expect(restoreList.Items).To(BeEmpty())

			stats := &disasterv1.BackupRestoreStatistics{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "ds-no-pvc-ds-stats", Namespace: "default"}, stats)).To(Succeed())
			Expect(stats.Status.Statistics.Total).To(Equal(int32(1)))
			Expect(stats.Status.Statistics.Completed).To(Equal(int32(1)))
			Expect(stats.Status.Statistics.Failed).To(Equal(int32(0)))
		})

		It("无 PVC 手动触发应该 skipped success 且不触发旧 AppBackup action", func() {
			dataSync, instance, config := createFullEnvironment("no-pvc-manual-ds", "default")
			lastSync := metav1.NewTime(time.Now().Add(-1 * time.Hour))
			manualAt := time.Now()
			oldActionAt := metav1.NewTime(time.Now().Add(-2 * time.Hour).Truncate(time.Second))
			dataSync.Status.LastSyncTime = &lastSync
			dataSync.Spec.Trigger.Manual = manualAt.Format(time.RFC3339)
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "ds-no-pvc-manual-ds", Namespace: "default"},
				Spec: disasterv1.AppBackupSpec{
					Cluster: "cluster-A",
					Action: &disasterv1.BackupAction{
						Type:      "Backup",
						RequestAt: oldActionAt,
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config, appBackup).
				WithStatusSubresource(dataSync, &disasterv1.BackupRestoreStatistics{}).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithObjects()},
			}

			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "no-pvc-manual-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeFalse())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "no-pvc-manual-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateReady))
			Expect(updated.Status.History).To(HaveLen(1))
			Expect(updated.Status.History[0].Status).To(Equal(dataSyncHistoryStatusSkipped))

			updatedBackup := &disasterv1.AppBackup{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "ds-no-pvc-manual-ds", Namespace: "default"}, updatedBackup)).To(Succeed())
			Expect(updatedBackup.Spec.Action).NotTo(BeNil())
			Expect(updatedBackup.Spec.Action.RequestAt.Time).To(Equal(oldActionAt.Time))
		})

		It("labelSelector 不匹配 Pod/PVC 时应该 skipped success", func() {
			dataSync, instance, config := createFullEnvironment("selector-no-pvc-ds", "default")
			dataSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)
			instance.Spec.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "wanted"}}
			sourceClient := sourceClientWithObjects(
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "data",
						Namespace: "app-ns",
						Labels:    map[string]string{"app": "other"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-pod",
						Namespace: "app-ns",
						Labels:    map[string]string{"app": "other"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
						Volumes: []corev1.Volume{{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"},
							},
						}},
					},
				},
			)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config).
				WithStatusSubresource(dataSync, &disasterv1.BackupRestoreStatistics{}).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClient},
			}

			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "selector-no-pvc-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeFalse())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "selector-no-pvc-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateReady))
			Expect(updated.Status.History).To(HaveLen(1))
			Expect(updated.Status.History[0].Status).To(Equal(dataSyncHistoryStatusSkipped))
		})

		It("labelSelector 匹配 Pod 引用 PVC 时不应该跳过", func() {
			dataSync, instance, config := createFullEnvironment("pod-pvc-ds", "default")
			dataSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)
			instance.Spec.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}
			sourceClient := sourceClientWithObjects(
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "app-ns"},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-pod",
						Namespace: "app-ns",
						Labels:    map[string]string{"app": "demo"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
						Volumes: []corev1.Volume{{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"},
							},
						}},
					},
				},
			)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClient},
			}

			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "pod-pvc-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeTrue())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "pod-pvc-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateInProgress))
			Expect(updated.Status.History).To(BeEmpty())
		})

		It("源集群 PVC 发现失败时应该 Failed 而不是 skipped", func() {
			dataSync, instance, config := createFullEnvironment("discover-fail-ds", "default")
			dataSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockError: fmt.Errorf("source api unavailable")},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "discover-fail-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "discover-fail-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateFailed))
			Expect(updated.Status.Reason).To(Equal(dataSyncReasonDependencyFailed))
			Expect(updated.Status.History).To(BeEmpty())
			Expect(apimeta.FindStatusCondition(updated.Status.Conditions, dataSyncConditionNoDataVolumes)).To(BeNil())
		})

		It("源集群 PVC list 失败时应该 Failed 而不是 skipped", func() {
			dataSync, instance, config := createFullEnvironment("list-fail-ds", "default")
			dataSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)
			sourceClient := &ctrlcommon.MockClient{
				Client: sourceClientWithObjects(),
				MockList: func(context.Context, client.ObjectList, ...client.ListOption) error {
					return fmt.Errorf("list forbidden")
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClient},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "list-fail-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "list-fail-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateFailed))
			Expect(updated.Status.Reason).To(Equal(dataSyncReasonDependencyFailed))
			Expect(updated.Status.Message).To(ContainSubstring("发现源集群可恢复 PVC 失败"))
			Expect(updated.Status.History).To(BeEmpty())
			Expect(apimeta.FindStatusCondition(updated.Status.Conditions, dataSyncConditionNoDataVolumes)).To(BeNil())
		})

		It("无 PVC 且 StorageRepository 不可用时仍应该 skipped success", func() {
			dataSync, instance, config := createFullEnvironment("no-pvc-bad-storage-ds", "default")
			dataSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)
			config.Spec.StorageRepository = "bad-sr"
			storage := &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "bad-sr", Namespace: "disaster-system"},
				Status: disasterv1.StorageRepositoryStatus{
					Status:  disasterv1.StorageRepositoryStatusUnavailable,
					Reason:  "ValidationFailed",
					Message: "forbidden",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config, storage).
				WithStatusSubresource(dataSync, &disasterv1.BackupRestoreStatistics{}).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithObjects()},
			}

			res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "no-pvc-bad-storage-ds", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeFalse())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "no-pvc-bad-storage-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateReady))
			Expect(updated.Status.Reason).To(BeEmpty())
			Expect(updated.Status.Message).To(BeEmpty())
			Expect(updated.Status.History).To(HaveLen(1))
			Expect(updated.Status.History[0].Status).To(Equal(dataSyncHistoryStatusSkipped))
			Expect(apimeta.FindStatusCondition(updated.Status.Conditions, "SyncFailed")).To(BeNil())
		})

		It("当 Backup PartiallyFailed 时应该写入 LastSyncTime 且不重复追加失败条件", func() {
			backupName := "bak-ds-test-ds-failed"

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-A",
					TargetCluster: "cluster-B",
				},
			}
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test-instance", Namespace: "default"},
				Spec: disasterv1.DisasterInstanceSpec{
					Config:     "test-config",
					Namespaces: []string{"app-ns"},
				},
			}
			dataSync := &disasterv1.DataSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ds",
					Namespace:  "default",
					Finalizers: []string{"testudo.softcdata.com/datasync-finalizer"},
				},
				Spec: disasterv1.DataSyncSpec{
					Instance: "test-instance",
					Trigger: disasterv1.TriggerSpec{
						Schedule: "*/1 * * * *",
					},
				},
				Status: disasterv1.DataSyncStatus{
					State:          disasterv1.DataSyncStateInProgress,
					LastBackupName: backupName,
					Conditions: []metav1.Condition{
						{
							Type:               "BackupFailed",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
							Reason:             "BackupFailed",
							Message:            "old message",
						},
					},
				},
			}
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ds-test-ds",
					Namespace: "default",
				},
				Spec: disasterv1.AppBackupSpec{
					Cluster: "cluster-A",
				},
				Status: disasterv1.AppBackupStatus{
					History: []disasterv1.BackupRecord{
						{
							Name:  backupName,
							Phase: string(velerov1.BackupPhasePartiallyFailed),
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config, appBackup).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-ds",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateFailed))
			Expect(updated.Status.LastSyncTime).NotTo(BeNil())
			Expect(updated.Status.Conditions).To(HaveLen(1))
			Expect(updated.Status.Conditions[0].Type).To(Equal("BackupFailed"))
			Expect(updated.Status.Conditions[0].Message).To(ContainSubstring(backupName))
		})

		It("当 Backup 被 AppBackup 标记为超时失败时应该转 Failed", func() {
			backupName := "bak-ds-test-ds-timeout"
			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-A",
					TargetCluster: "cluster-B",
				},
			}
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test-instance", Namespace: "default"},
				Spec: disasterv1.DisasterInstanceSpec{
					Config:     "test-config",
					Namespaces: []string{"app-ns"},
				},
			}
			dataSync := &disasterv1.DataSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ds",
					Namespace:  "default",
					Finalizers: []string{"testudo.softcdata.com/datasync-finalizer"},
				},
				Spec: disasterv1.DataSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Schedule: "*/1 * * * *"},
				},
				Status: disasterv1.DataSyncStatus{
					State:          disasterv1.DataSyncStateInProgress,
					LastBackupName: backupName,
				},
			}
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "ds-test-ds", Namespace: "default"},
				Spec:       (&DataSyncReconciler{}).buildAppBackupSpec(instance, config),
				Status: disasterv1.AppBackupStatus{
					LatestBackupStatus: disasterv1.LastBackupStatusFailed,
					Reason:             "TimeoutExceeded",
					Message:            "Backup did not report a Velero phase within 10m",
					History: []disasterv1.BackupRecord{{
						Name:          backupName,
						Phase:         ctrlcommon.BackupPhaseTimeoutFailed,
						ManagedStatus: disasterv1.LastBackupStatusFailed,
					}},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config, appBackup).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-ds", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateFailed))
			Expect(updated.Status.LastSyncTime).NotTo(BeNil())
			Expect(updated.Status.Reason).To(Equal(dataSyncReasonBackupFailed))
			Expect(updated.Status.Message).To(ContainSubstring("did not report a Velero phase"))
		})

		It("应该将实例级操作超时投递到 AppBackup", func() {
			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster:     "cluster-A",
					TargetCluster:     "cluster-B",
					StorageRepository: "good-sr",
				},
			}
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test-instance", Namespace: "default"},
				Spec: disasterv1.DisasterInstanceSpec{
					Config:                  "test-config",
					Namespaces:              []string{"app-ns"},
					OperationTimeoutMinutes: 180,
				},
			}
			dataSync := &disasterv1.DataSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ds",
					Namespace:  "default",
					Finalizers: []string{"testudo.softcdata.com/datasync-finalizer"},
				},
				Spec: disasterv1.DataSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Schedule: "*/1 * * * *"},
				},
				Status: disasterv1.DataSyncStatus{
					State: disasterv1.DataSyncStateInProgress,
				},
			}
			storage := &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "good-sr", Namespace: "disaster-system"},
				Status: disasterv1.StorageRepositoryStatus{
					Status: disasterv1.StorageRepositoryStatusAvailable,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config, storage).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-ds", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			backup := &disasterv1.AppBackup{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "ds-test-ds", Namespace: "default"}, backup)).To(Succeed())
			Expect(backup.Spec.Timeout).NotTo(BeNil())
			Expect(backup.Spec.Timeout.Duration).To(Equal(180 * time.Minute))
		})

		It("当备份已在进行中时也应该同步新的实例级操作超时到 AppBackup", func() {
			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster:     "cluster-A",
					TargetCluster:     "cluster-B",
					StorageRepository: "good-sr",
				},
			}
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test-instance", Namespace: "default"},
				Spec: disasterv1.DisasterInstanceSpec{
					Config:                  "test-config",
					Namespaces:              []string{"app-ns"},
					OperationTimeoutMinutes: 180,
				},
			}
			dataSync := &disasterv1.DataSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ds",
					Namespace:  "default",
					Finalizers: []string{"testudo.softcdata.com/datasync-finalizer"},
				},
				Spec: disasterv1.DataSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Schedule: "*/1 * * * *"},
				},
				Status: disasterv1.DataSyncStatus{
					State:          disasterv1.DataSyncStateInProgress,
					LastBackupName: "bak-ds-test-ds-old",
				},
			}
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "ds-test-ds", Namespace: "default"},
				Spec: disasterv1.AppBackupSpec{
					Cluster: "cluster-A",
					Template: velerov1.BackupSpec{
						IncludedNamespaces: []string{"app-ns"},
					},
					Timeout: &metav1.Duration{Duration: time.Hour},
				},
				Status: disasterv1.AppBackupStatus{
					History: []disasterv1.BackupRecord{
						{
							Name:  "bak-ds-test-ds-old",
							Phase: string(velerov1.BackupPhaseInProgress),
						},
					},
				},
			}
			storage := &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "good-sr", Namespace: "disaster-system"},
				Status: disasterv1.StorageRepositoryStatus{
					Status: disasterv1.StorageRepositoryStatusAvailable,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config, appBackup, storage).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-ds", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			updatedBackup := &disasterv1.AppBackup{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "ds-test-ds", Namespace: "default"}, updatedBackup)).To(Succeed())
			Expect(updatedBackup.Spec.Timeout).NotTo(BeNil())
			Expect(updatedBackup.Spec.Timeout.Duration).To(Equal(180 * time.Minute))
		})

		It("当 StorageRepository 不可用时应该直接失败并发射 Finished 事件", func() {
			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster:      "cluster-A",
					TargetCluster:      "cluster-B",
					StorageRepository:  "bad-sr",
					ResourceSyncPolicy: "policy-daily",
				},
			}
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test-instance", Namespace: "default"},
				Spec: disasterv1.DisasterInstanceSpec{
					Config:     "test-config",
					Namespaces: []string{"app-ns"},
				},
			}
			dataSync := &disasterv1.DataSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ds",
					Namespace:  "default",
					Finalizers: []string{"testudo.softcdata.com/datasync-finalizer"},
				},
				Spec: disasterv1.DataSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Manual: time.Now().Format(time.RFC3339)},
				},
				Status: disasterv1.DataSyncStatus{
					State: disasterv1.DataSyncStateInProgress,
				},
			}
			storage := &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{Name: "bad-sr", Namespace: "disaster-system"},
				Status: disasterv1.StorageRepositoryStatus{
					Status:  disasterv1.StorageRepositoryStatusUnavailable,
					Reason:  "ValidationFailed",
					Message: "forbidden",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(dataSync, instance, config, storage).
				WithStatusSubresource(dataSync).
				Build()

			r = &DataSyncReconciler{
				Client:              fakeClient,
				Scheme:              s,
				Log:                 ctrl.Log.WithName("test"),
				Recorder:            recorder,
				Scheduler:           syncScheduler,
				SourceClientFactory: &ctrlcommon.MockClientFactory{MockClient: sourceClientWithPVC("app-ns", "data")},
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-ds", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-ds", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DataSyncStateFailed))
			Expect(updated.Status.Reason).To(Equal(dataSyncReasonStorageUnavailable))
			Expect(updated.Status.Message).To(ContainSubstring("StorageRepository bad-sr 不可用"))
			Expect(updated.Status.LastSyncTime).NotTo(BeNil())

			events := &corev1.EventList{}
			Expect(fakeClient.List(ctx, events, client.InNamespace("default"))).To(Succeed())
			finishedFound := false
			for i := range events.Items {
				ev := events.Items[i]
				if ev.Reason == helper.EventReasonExecutionFinished &&
					strings.Contains(ev.Message, `"status":"Failed"`) &&
					strings.Contains(ev.Message, `执行数据同步 test-ds`) {
					payload := helper.DisasterEventPayload{}
					Expect(json.Unmarshal([]byte(ev.Message), &payload)).To(Succeed())
					Expect(payload.ErrorCode).To(Equal(updated.Status.Reason))
					Expect(payload.Message).To(Equal(updated.Status.Message))
					finishedFound = true
					break
				}
			}
			Expect(finishedFound).To(BeTrue())
		})
	})

})
