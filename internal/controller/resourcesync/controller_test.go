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

package resourcesync

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrlpkg "github.com/softcdata/testudo-operator/internal/controller"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResourceSyncController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ResourceSync Controller Suite")
}

var _ = Describe("ResourceSync Controller", func() {
	var (
		ctx           context.Context
		r             *ResourceSyncReconciler
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

	createTestResourceSync := func(name, namespace string) *disasterv1.ResourceSync {
		return &disasterv1.ResourceSync{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				Finalizers: []string{"testudo.softcdata.com/resourcesync-finalizer"},
			},
			Spec: disasterv1.ResourceSyncSpec{
				Instance: "test-instance",
				Trigger: disasterv1.TriggerSpec{
					Schedule: "*/1 * * * *", // 每分钟
				},
			},
			Status: disasterv1.ResourceSyncStatus{
				State:        disasterv1.ResourceSyncStateReady,
				LastSyncTime: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
			},
		}
	}

	Describe("Cron 调度注册", func() {
		It("应该将 Cron 任务注册到调度器", func() {
			resourceSync := createTestResourceSync("test-rs", "default")

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(resourceSync).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			// 调谐
			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-rs",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// 验证调度器中是否有任务
			Expect(syncScheduler.HasJob("default", "test-rs")).To(BeTrue())
		})

		It("如果暂停，应该从调度器移除任务", func() {
			resourceSync := createTestResourceSync("test-rs", "default")
			resourceSync.Spec.Paused = true

			// 先手动添加一个任务模拟已存在
			err := syncScheduler.AddOrUpdate("default", "test-rs", "*/1 * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())
			Expect(syncScheduler.HasJob("default", "test-rs")).To(BeTrue())

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(resourceSync).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			// 调谐
			_, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-rs",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// 验证调度器中任务已被移除
			Expect(syncScheduler.HasJob("default", "test-rs")).To(BeFalse())
		})

		It("如果 schedule 被清空，应该从调度器移除残留任务", func() {
			resourceSync := createTestResourceSync("test-rs", "default")
			resourceSync.Spec.Trigger.Schedule = ""

			err := syncScheduler.AddOrUpdate("default", "test-rs", "*/1 * * * *", func() {})
			Expect(err).NotTo(HaveOccurred())
			Expect(syncScheduler.HasJob("default", "test-rs")).To(BeTrue())

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(resourceSync).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			_, err = r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-rs",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(syncScheduler.HasJob("default", "test-rs")).To(BeFalse())
		})

		It("残留 cron 回调在 schedule 清空后不应该写入手动触发器", func() {
			resourceSync := createTestResourceSync("test-rs", "default")
			resourceSync.Spec.Trigger.Schedule = ""
			resourceSync.Spec.Trigger.Manual = ""

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(resourceSync).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			r.triggerSync("default", "test-rs")

			latest := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-rs", Namespace: "default"}, latest)).To(Succeed())
			Expect(latest.Spec.Trigger.Manual).To(BeEmpty())
		})

		It("残留 cron 回调在暂停后不应该写入手动触发器", func() {
			resourceSync := createTestResourceSync("test-rs", "default")
			resourceSync.Spec.Paused = true
			resourceSync.Spec.Trigger.Manual = ""

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(resourceSync).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			r.triggerSync("default", "test-rs")

			latest := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-rs", Namespace: "default"}, latest)).To(Succeed())
			Expect(latest.Spec.Trigger.Manual).To(BeEmpty())
		})

		It("进行中时 cron 回调不应该覆盖手动触发器", func() {
			resourceSync := createTestResourceSync("test-rs", "default")
			resourceSync.Spec.Trigger.Manual = "2026-05-14T02:00:00Z"
			resourceSync.Status.State = disasterv1.ResourceSyncStateInProgress

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(resourceSync).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			r.triggerSync("default", "test-rs")

			latest := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-rs", Namespace: "default"}, latest)).To(Succeed())
			Expect(latest.Spec.Trigger.Manual).To(Equal("2026-05-14T02:00:00Z"))
		})
	})

	Describe("同步执行 (Real Logic)", func() {
		createFullEnvironment := func(name, namespace string) (*disasterv1.ResourceSync, *disasterv1.DisasterInstance, *disasterv1.DisasterConfig) {
			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: namespace},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster:      "cluster-A",
					TargetCluster:      "cluster-B",
					ResourceSyncPolicy: "policy-daily",
				},
			}
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test-instance", Namespace: namespace},
				Spec: disasterv1.DisasterInstanceSpec{
					Config:     "test-config",
					Namespaces: []string{"app-ns"},
				},
			}
			rs := &disasterv1.ResourceSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Finalizers: []string{"testudo.softcdata.com/resourcesync-finalizer"},
				},
				Spec: disasterv1.ResourceSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Schedule: "*/1 * * * *"},
				},
				Status: disasterv1.ResourceSyncStatus{
					State:        disasterv1.ResourceSyncStateReady,
					LastSyncTime: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
				},
			}
			return rs, instance, config
		}

		It("应该创建 AppBackup (Skeleton) 及其 OwnerReference", func() {
			resourceSync, instance, config := createFullEnvironment("test-rs", "default")
			// 触发手动同步
			resourceSync.Spec.Trigger.Manual = time.Now().Format(time.RFC3339)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(resourceSync, instance, config).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			// Reconcile 可能会因为依赖标签同步、状态初始化、Action 触发等原因多次 Requeue。
			// 这里循环执行直到创建出固定命名的 AppBackup（rs-<ResourceSync.name>）。
			appBackupName := "rs-" + resourceSync.Name
			var backup *disasterv1.AppBackup
			for i := 0; i < 10; i++ {
				res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-rs", Namespace: "default"}})
				Expect(err).NotTo(HaveOccurred())
				_ = res

				ab := &disasterv1.AppBackup{}
				if err := fakeClient.Get(ctx, types.NamespacedName{Name: appBackupName, Namespace: "default"}, ab); err == nil {
					backup = ab
					break
				}
			}
			Expect(backup).NotTo(BeNil())

			// 验证 SnapshotVolumes 为 false (Skeleton Sync)
			Expect(backup.Spec.Template.SnapshotVolumes).NotTo(BeNil())
			Expect(*backup.Spec.Template.SnapshotVolumes).To(BeFalse())

			Expect(backup.OwnerReferences).To(HaveLen(1))
			Expect(backup.OwnerReferences[0].Name).To(Equal(resourceSync.Name))
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
			resourceSync := &disasterv1.ResourceSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rs",
					Namespace:  "default",
					Finalizers: []string{"testudo.softcdata.com/resourcesync-finalizer"},
				},
				Spec: disasterv1.ResourceSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Manual: time.Now().Format(time.RFC3339)},
				},
				Status: disasterv1.ResourceSyncStatus{
					State: disasterv1.ResourceSyncStateInProgress,
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
				WithObjects(resourceSync, instance, config, storage).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-rs", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-rs", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.ResourceSyncStateFailed))
			Expect(updated.Status.Reason).To(Equal(resourceSyncReasonStorageUnavailable))
			Expect(updated.Status.Message).To(ContainSubstring("StorageRepository bad-sr 不可用"))
			Expect(updated.Status.LastSyncTime).NotTo(BeNil())

			events := &corev1.EventList{}
			Expect(fakeClient.List(ctx, events, client.InNamespace("default"))).To(Succeed())
			finishedFound := false
			for i := range events.Items {
				ev := events.Items[i]
				if ev.Reason == helper.EventReasonExecutionFinished &&
					strings.Contains(ev.Message, `"status":"Failed"`) &&
					strings.Contains(ev.Message, `执行资源同步 test-rs`) {
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

		It("当 Backup 被 AppBackup 标记为超时失败时应该转 Failed", func() {
			backupName := "bak-rs-test-rs-timeout"
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
			resourceSync := &disasterv1.ResourceSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rs",
					Namespace:  "default",
					Finalizers: []string{"testudo.softcdata.com/resourcesync-finalizer"},
				},
				Spec: disasterv1.ResourceSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Schedule: "*/1 * * * *"},
				},
				Status: disasterv1.ResourceSyncStatus{
					State:          disasterv1.ResourceSyncStateInProgress,
					LastBackupName: backupName,
				},
			}
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{Name: "rs-test-rs", Namespace: "default"},
				Spec:       (&ResourceSyncReconciler{}).buildAppBackupSpec(instance, config),
				Status: disasterv1.AppBackupStatus{
					LatestBackupStatus: disasterv1.LastBackupStatusFailed,
					Reason:             "TimeoutExceeded",
					Message:            "Backup did not report a Velero phase within 10m",
					History: []disasterv1.BackupRecord{{
						Name:          backupName,
						Phase:         ctrlpkg.BackupPhaseTimeoutFailed,
						ManagedStatus: disasterv1.LastBackupStatusFailed,
					}},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(resourceSync, instance, config, appBackup).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-rs", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-rs", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.ResourceSyncStateFailed))
			Expect(updated.Status.LastSyncTime).NotTo(BeNil())
			Expect(updated.Status.Reason).To(Equal(resourceSyncReasonBackupFailed))
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
			resourceSync := &disasterv1.ResourceSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rs",
					Namespace:  "default",
					Finalizers: []string{"testudo.softcdata.com/resourcesync-finalizer"},
				},
				Spec: disasterv1.ResourceSyncSpec{
					Instance: "test-instance",
					Trigger:  disasterv1.TriggerSpec{Schedule: "*/1 * * * *"},
				},
				Status: disasterv1.ResourceSyncStatus{
					State: disasterv1.ResourceSyncStateInProgress,
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
				WithObjects(resourceSync, instance, config, storage).
				WithStatusSubresource(resourceSync).
				Build()

			r = &ResourceSyncReconciler{
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
			}

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-rs", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())

			backup := &disasterv1.AppBackup{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "rs-test-rs", Namespace: "default"}, backup)).To(Succeed())
			Expect(backup.Spec.Timeout).NotTo(BeNil())
			Expect(backup.Spec.Timeout.Duration).To(Equal(180 * time.Minute))
		})
	})
})
