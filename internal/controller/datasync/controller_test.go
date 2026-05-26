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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
				Client:    fakeClient,
				Scheme:    s,
				Log:       ctrl.Log.WithName("test"),
				Recorder:  recorder,
				Scheduler: syncScheduler,
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
