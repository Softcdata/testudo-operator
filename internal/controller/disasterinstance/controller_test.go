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

package disasterinstance

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

func TestDisasterInstanceController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DisasterInstance Controller Suite")
}

var _ = Describe("DisasterInstance Controller", func() {
	var (
		ctx        context.Context
		r          *DisasterInstanceReconciler
		fakeClient client.Client
		s          *runtime.Scheme
		recorder   *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()

		// 设置 scheme
		s = runtime.NewScheme()
		Expect(scheme.AddToScheme(s)).To(Succeed())
		Expect(disasterv1.AddToScheme(s)).To(Succeed())

		recorder = record.NewFakeRecorder(100)
	})

	// 创建测试用的 DisasterConfig
	createTestConfig := func() *disasterv1.DisasterConfig {
		return &disasterv1.DisasterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-config",
			},
			Spec: disasterv1.DisasterConfigSpec{
				SourceCluster:     "cluster-1",
				TargetCluster:     "cluster-2",
				StorageRepository: "test-repo",
				DataSyncPolicy:    "data-sync-policy",
			},
		}
	}

	// 创建测试用的 DisasterInstance
	createTestInstance := func(name, namespace string) *disasterv1.DisasterInstance {
		return &disasterv1.DisasterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: disasterv1.DisasterInstanceSpec{
				Config: "test-config",
			},
		}
	}

	Describe("状态机测试", func() {
		Context("当 DisasterInstance 刚创建时", func() {
			It("应该将 FsmState 初始化为 Pending", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 第一次调谐：添加 Finalizer
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeTrue())

				// 第二次调谐：初始化 FsmState
				result, err = r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证状态
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStatePending))
			})
		})

		Context("当处于 Pending 状态时", func() {
			It("应该创建 DataSync 和 ResourceSync 并转换到 Initializing", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStatePending
				instance.Finalizers = []string{finalizerName}

				// 补充对应的 DisasterPolicy CR
				dataPolicy := &disasterv1.DisasterPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "data-sync-policy", Namespace: "default"},
					Spec: disasterv1.DisasterPolicySpec{
						Type:     disasterv1.PolicyTypeDataSync,
						Schedule: "*/15 * * * *",
						State:    disasterv1.PolicyStateEnabled,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataPolicy).
					WithStatusSubresource(instance, &disasterv1.DataSync{}, &disasterv1.ResourceSync{}).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Requeue).To(BeTrue())

				// 验证 DataSync 被创建
				dataSync := &disasterv1.DataSync{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      "dr-ds-test-instance",
					Namespace: "default",
				}, dataSync)
				Expect(err).NotTo(HaveOccurred())
				Expect(dataSync.Spec.Instance).To(Equal("test-instance"))

				// 验证 ResourceSync 被创建
				resourceSync := &disasterv1.ResourceSync{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      "dr-rs-test-instance",
					Namespace: "default",
				}, resourceSync)
				Expect(err).NotTo(HaveOccurred())
				Expect(resourceSync.Spec.Instance).To(Equal("test-instance"))

				// 验证状态转换到 Initializing
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateInitializing))
				Expect(updated.Status.PrimaryCluster).To(Equal("cluster-1"))
				Expect(updated.Status.SecondaryCluster).To(Equal("cluster-2"))
			})
		})

		Context("当处于 Initializing 状态时", func() {
			It("如果子资源都 Ready，应该转换到 Protected", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateInitializing
				instance.Status.DataSyncName = "dr-ds-test-instance"
				instance.Status.ResourceSyncName = "dr-rs-test-instance"
				instance.Finalizers = []string{finalizerName}

				now := metav1.Now()
				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.DataSyncStatus{
						State:        disasterv1.DataSyncStateReady,
						LastSyncTime: &now,
					},
				}
				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.ResourceSyncStatus{
						State:        disasterv1.ResourceSyncStateReady,
						LastSyncTime: &now,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataSync, resourceSync).
					WithStatusSubresource(instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				// First pass may sync dependency labels and requeue.
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())
				// Second pass should create DataSync/ResourceSync.
				_, err = r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证状态转换到 Protected
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
				Expect(updated.Status.AvailableOperations).To(ContainElements("failover", "pause", "synconce"))
			})

			It("如果子资源未 Ready，应该保持 Initializing 并重新入队", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateInitializing
				instance.Status.DataSyncName = "dr-ds-test-instance"
				instance.Status.ResourceSyncName = "dr-rs-test-instance"
				instance.Finalizers = []string{finalizerName}

				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.DataSyncStatus{
						State: disasterv1.DataSyncStateInProgress, // 未就绪
					},
				}
				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.ResourceSyncStatus{
						State: disasterv1.ResourceSyncStateReady,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataSync, resourceSync).
					WithStatusSubresource(instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(10 * time.Second))

				// 验证仍然是 Initializing
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateInitializing))
			})

			It("如果任一子资源 Failed，应该转换到 Failed", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateInitializing
				instance.Status.DataSyncName = "dr-ds-test-instance"
				instance.Status.ResourceSyncName = "dr-rs-test-instance"
				instance.Finalizers = []string{finalizerName}

				now := metav1.Now()
				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.DataSyncStatus{
						State:        disasterv1.DataSyncStateFailed,
						LastSyncTime: &now,
					},
				}
				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.ResourceSyncStatus{
						State:        disasterv1.ResourceSyncStateReady,
						LastSyncTime: &now,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataSync, resourceSync).
					WithStatusSubresource(instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
				Expect(updated.Status.Reason).To(Equal(instanceReasonDataSyncFailed))
				Expect(updated.Status.Message).To(Equal("initialization failed: data sync failed"))
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reset", "syncdata"}))
				Expect(updated.Status.LastDataSyncTime).NotTo(BeNil())
			})
		})

		Context("当处于 Protected 状态时", func() {
			It("应该更新 AvailableOperations", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateProtected
				instance.Status.Reason = "StaleError"
				instance.Status.Message = "stale message"
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(60 * time.Second))

				// 验证 AvailableOperations
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.AvailableOperations).To(ContainElements("failover", "pause", "synconce"))
				Expect(updated.Status.Reason).To(BeEmpty())
				Expect(updated.Status.Message).To(BeEmpty())
			})

			It("当 DataSync 失败时应该进入 Failed", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateProtected
				instance.Status.DataSyncName = "dr-ds-test-instance"
				instance.Status.ResourceSyncName = "dr-rs-test-instance"
				instance.Finalizers = []string{finalizerName}

				lastSync := metav1.Now()
				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.DataSyncStatus{
						State:        disasterv1.DataSyncStateFailed,
						Reason:       "BuildRestoreSpecFailed",
						Message:      "storageclass not found",
						LastSyncTime: &lastSync,
					},
				}
				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.ResourceSyncStatus{
						State: disasterv1.ResourceSyncStateReady,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataSync, resourceSync).
					WithStatusSubresource(instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
				Expect(updated.Status.Reason).To(Equal("BuildRestoreSpecFailed"))
				Expect(updated.Status.Message).To(Equal("storageclass not found"))
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reset", "syncdata"}))
				Expect(updated.Status.LastDataSyncTime).NotTo(BeNil())
			})
		})

		Context("当处于 Paused 状态时", func() {
			It("应该只允许 resume 操作", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStatePaused
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证 AvailableOperations
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"resume"}))
			})
		})

		Context("当处于 Active 状态时", func() {
			It("应该只允许 reprotect 和 undo 操作", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateActive
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证 AvailableOperations
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reprotect", "undo"}))
			})

			It("当 ResourceSync 失败且无错误详情时应回退到默认错误码", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateActive
				instance.Status.DataSyncName = "dr-ds-test-instance"
				instance.Status.ResourceSyncName = "dr-rs-test-instance"
				instance.Finalizers = []string{finalizerName}

				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.DataSyncStatus{
						State: disasterv1.DataSyncStateReady,
					},
				}
				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.ResourceSyncStatus{
						State: disasterv1.ResourceSyncStateFailed,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataSync, resourceSync).
					WithStatusSubresource(instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
				Expect(updated.Status.Reason).To(Equal(instanceReasonResourceSyncFailed))
				Expect(updated.Status.Message).To(Equal("resource sync failed"))
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reset", "syncresource"}))
			})
		})

		Context("当处于 Failed 状态时", func() {
			It("当同步子资源恢复成功后应自动清错并收敛到 Protected", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailed
				instance.Status.Reason = "BuildRestoreSpecFailed"
				instance.Status.Message = "stale build restore spec failed"
				instance.Status.PrimaryCluster = "cluster-1"
				instance.Status.SecondaryCluster = "cluster-2"
				instance.Status.DataSyncName = "dr-ds-test-instance"
				instance.Status.ResourceSyncName = "dr-rs-test-instance"
				oldDataSyncTime := metav1.NewTime(time.Now().Add(-30 * time.Minute))
				oldResourceSyncTime := metav1.NewTime(time.Now().Add(-30 * time.Minute))
				instance.Status.LastDataSyncTime = &oldDataSyncTime
				instance.Status.LastResourceSyncTime = &oldResourceSyncTime
				instance.Finalizers = []string{finalizerName}

				nowData := metav1.Now()
				nowResource := metav1.NewTime(time.Now().Add(1 * time.Minute))
				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.DataSyncStatus{
						State:        disasterv1.DataSyncStateReady,
						LastSyncTime: &nowData,
					},
				}
				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.ResourceSyncStatus{
						State:        disasterv1.ResourceSyncStateReady,
						LastSyncTime: &nowResource,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataSync, resourceSync).
					WithStatusSubresource(config, instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				}
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				_, err = r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
				Expect(updated.Status.Reason).To(BeEmpty())
				Expect(updated.Status.Message).To(BeEmpty())
				Expect(updated.Status.AvailableOperations).To(ContainElements("failover", "pause", "synconce", "syncdata", "syncresource"))
				Expect(updated.Status.LastDataSyncTime).NotTo(BeNil())
				Expect(updated.Status.LastResourceSyncTime).NotTo(BeNil())
				Expect(updated.Status.LastDataSyncTime.Time.After(oldDataSyncTime.Time)).To(BeTrue())
				Expect(updated.Status.LastResourceSyncTime.Time.After(oldResourceSyncTime.Time)).To(BeTrue())
			})

			It("同步类失败仍未恢复时，应保留 Failed 并开放对应同步重试", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailed
				instance.Status.Reason = instanceReasonDataSyncFailed
				instance.Status.Message = "stale data sync failed"
				instance.Status.DataSyncName = "dr-ds-test-instance"
				instance.Status.ResourceSyncName = "dr-rs-test-instance"
				instance.Status.AvailableOperations = []string{"reset"}
				instance.Finalizers = []string{finalizerName}

				now := metav1.Now()
				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.DataSyncStatus{
						State:        disasterv1.DataSyncStateFailed,
						LastSyncTime: &now,
					},
				}
				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.ResourceSyncStatus{
						State:        disasterv1.ResourceSyncStateReady,
						LastSyncTime: &now,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataSync, resourceSync).
					WithStatusSubresource(config, instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
				Expect(updated.Status.Reason).To(Equal(instanceReasonDataSyncFailed))
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reset", "syncdata"}))
			})

			It("非同步类失败原因不应自动恢复", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailed
				instance.Status.Reason = "StepFailed"
				instance.Status.Message = "failover step failed"
				instance.Status.DataSyncName = "dr-ds-test-instance"
				instance.Status.ResourceSyncName = "dr-rs-test-instance"
				instance.Finalizers = []string{finalizerName}

				nowData := metav1.Now()
				nowResource := metav1.NewTime(time.Now().Add(1 * time.Minute))
				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.DataSyncStatus{
						State:        disasterv1.DataSyncStateReady,
						LastSyncTime: &nowData,
					},
				}
				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Status: disasterv1.ResourceSyncStatus{
						State:        disasterv1.ResourceSyncStateReady,
						LastSyncTime: &nowResource,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataSync, resourceSync).
					WithStatusSubresource(config, instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				}
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				_, err = r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
				Expect(updated.Status.Reason).To(Equal("StepFailed"))
				Expect(updated.Status.Message).To(Equal("failover step failed"))
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reset"}))
			})
		})
	})

	Describe("联动测试", func() {
		Context("创建 DisasterInstance", func() {
			It("应该自动创建带有 OwnerReference 的 DataSync 和 ResourceSync", func() {
				config := createTestConfig()
				instance := createTestInstance("test-instance", "default")
				instance.UID = "test-uid-12345"
				instance.Status.FsmState = disasterv1.FsmStatePending
				instance.Finalizers = []string{finalizerName}

				// 补充对应的 DisasterPolicy CR
				dataPolicy := &disasterv1.DisasterPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "data-sync-policy", Namespace: "default"},
					Spec: disasterv1.DisasterPolicySpec{
						Type:     disasterv1.PolicyTypeDataSync,
						Schedule: "*/15 * * * *",
						State:    disasterv1.PolicyStateEnabled,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataPolicy).
					WithStatusSubresource(instance, &disasterv1.DataSync{}, &disasterv1.ResourceSync{}).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())
				// First pass may sync dependency labels and requeue.
				_, err = r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证 DataSync 的 OwnerReference
				dataSync := &disasterv1.DataSync{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      "dr-ds-test-instance",
					Namespace: "default",
				}, dataSync)
				Expect(err).NotTo(HaveOccurred())
				Expect(dataSync.OwnerReferences).To(HaveLen(1))
				Expect(dataSync.OwnerReferences[0].Kind).To(Equal("DisasterInstance"))
				Expect(dataSync.OwnerReferences[0].Name).To(Equal("test-instance"))

				// 验证 ResourceSync 的 OwnerReference
				resourceSync := &disasterv1.ResourceSync{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      "dr-rs-test-instance",
					Namespace: "default",
				}, resourceSync)
				Expect(err).NotTo(HaveOccurred())
				Expect(resourceSync.OwnerReferences).To(HaveLen(1))
				Expect(resourceSync.OwnerReferences[0].Kind).To(Equal("DisasterInstance"))
				Expect(resourceSync.OwnerReferences[0].Name).To(Equal("test-instance"))
			})
		})

		Context("DisasterConfig 不存在时", func() {
			It("应该进入 ConfigError 并重新入队", func() {
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStatePending
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance). // 没有 config!
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(5 * time.Second))

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateConfigError))
				Expect(updated.Status.LastStableFsmState).To(Equal(disasterv1.FsmStatePending))
				Expect(updated.Status.Reason).To(Equal(instanceReasonConfigNotFound))
			})
		})
	})

	Describe("删除保护测试", func() {
		Context("当实例处于 Protected 状态尝试删除时", func() {
			It("应该阻止删除并记录事件", func() {
				Skip("legacy finalizer deletion protection temporarily disabled")

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateProtected
				instance.Finalizers = []string{finalizerName}
				now := metav1.Now()
				instance.DeletionTimestamp = &now

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证被阻止（Finalizer 未移除）
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Finalizers).To(ContainElement(finalizerName))

				// 验证 Result
				Expect(result.RequeueAfter).To(Equal(30 * time.Second))

				// 验证 Event
				// Expect(recorder.Events).To(HaveLen(1)) // 可能会有其他事件，只要包含即可
				var foundEvent bool
				for len(recorder.Events) > 0 {
					event := <-recorder.Events
					if event != "" { // 简单检查
						foundEvent = true
						break
					}
				}
				// 注意：fake recorder 的 behavior 有时取决于 buffer size。只要 RequeueAfter 正确就说明走到了那个分支
				Expect(foundEvent).To(BeTrue())
			})
		})

		Context("当实例被 DisasterGroup 引用尝试删除时", func() {
			It("应该阻止删除并记录事件", func() {
				Skip("legacy finalizer deletion protection temporarily disabled")

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStatePending // 即使状态不属于保护状态，被组引用也应阻止
				instance.Finalizers = []string{finalizerName}
				now := metav1.Now()
				instance.DeletionTimestamp = &now

				group := &disasterv1.DisasterGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-group",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterGroupSpec{
						Levels: [][]string{{"test-instance"}},
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance, group).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证被阻止（Finalizer 未移除）
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Finalizers).To(ContainElement(finalizerName))

				// 验证 Result
				Expect(result.RequeueAfter).To(Equal(30 * time.Second))

				// 验证 Event
				foundEvent := false
				for len(recorder.Events) > 0 {
					event := <-recorder.Events
					if event != "" {
						foundEvent = true
						break
					}
				}
				Expect(foundEvent).To(BeTrue())
			})
		})

		Context("当带有 force-delete 注解尝试删除时", func() {
			It("应该允许删除（移除 Finalizer）", func() {
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateProtected
				instance.Finalizers = []string{finalizerName}
				instance.Annotations = map[string]string{
					"testudo.softcdata.com/force-delete": "true",
				}
				now := metav1.Now()
				instance.DeletionTimestamp = &now

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证 Finalizer 已移除 (或对象已删除)
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				if err == nil {
					Expect(updated.Finalizers).NotTo(ContainElement(finalizerName))
				} else {
					Expect(errors.IsNotFound(err)).To(BeTrue())
				}
			})
		})

		Context("当处于 Paused 状态尝试删除时", func() {
			It("应该允许删除", func() {
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStatePaused // Paused 不在保护列表
				instance.Finalizers = []string{finalizerName}
				now := metav1.Now()
				instance.DeletionTimestamp = &now

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// 调谐
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-instance",
						Namespace: "default",
					},
				})
				Expect(err).NotTo(HaveOccurred())

				// 验证 Finalizer 已移除 (或对象已删除)
				updated := &disasterv1.DisasterInstance{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)
				if err == nil {
					Expect(updated.Finalizers).NotTo(ContainElement(finalizerName))
				} else {
					Expect(errors.IsNotFound(err)).To(BeTrue())
				}
			})
		})
	})

	Describe("DisasterPolicy 调度传播测试", func() {
		// 辅助函数：创建带有 DataSyncPolicy + ResourceSyncPolicy 的 DisasterConfig
		createConfigWithPolicies := func(dataPolicyName, resPolicyName string) *disasterv1.DisasterConfig {
			return &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster:      "cluster-1",
					TargetCluster:      "cluster-2",
					StorageRepository:  "test-repo",
					DataSyncPolicy:     dataPolicyName,
					ResourceSyncPolicy: resPolicyName,
				},
			}
		}

		// 辅助函数：创建处于 Pending+Finalizer 状态的实例
		createReadyInstance := func(name, namespace string) *disasterv1.DisasterInstance {
			inst := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  namespace,
					Finalizers: []string{finalizerName},
					UID:        "test-uid-999",
				},
				Spec: disasterv1.DisasterInstanceSpec{
					Config: "test-config",
				},
			}
			inst.Status.FsmState = disasterv1.FsmStatePending
			return inst
		}

		Context("当 DisasterConfig 引用了 Enabled 的 DataSyncPolicy 时", func() {
			It("DataSync 的 Schedule 应等于策略的 Cron 表达式", func() {
				policy := &disasterv1.DisasterPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-data-policy",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterPolicySpec{
						Type:     disasterv1.PolicyTypeDataSync,
						Schedule: "0 */6 * * *", // 每6小时
						State:    disasterv1.PolicyStateEnabled,
					},
				}
				config := createConfigWithPolicies("my-data-policy", "")
				instance := createReadyInstance("test-instance", "default")

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, policy).
					WithStatusSubresource(instance, &disasterv1.DataSync{}, &disasterv1.ResourceSync{}).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// First pass may sync dependency labels and requeue.
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())
				// Second pass should perform the actual propagation logic.
				_, err = r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())

				dataSync := &disasterv1.DataSync{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name: "dr-ds-test-instance", Namespace: "default",
				}, dataSync)).To(Succeed())
				// Schedule 应该来自 DisasterPolicy，而非硬编码
				Expect(dataSync.Spec.Trigger.Schedule).To(Equal("0 */6 * * *"))
			})
		})

		Context("当 DataSyncPolicy 的 State=Disabled 时", func() {
			It("DataSync 的 Schedule 应为空，不触发调度", func() {
				policy := &disasterv1.DisasterPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "disabled-policy",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterPolicySpec{
						Type:     disasterv1.PolicyTypeDataSync,
						Schedule: "*/5 * * * *",
						State:    disasterv1.PolicyStateDisabled, // 禁用
					},
				}
				config := createConfigWithPolicies("disabled-policy", "")
				instance := createReadyInstance("test-instance", "default")

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, policy).
					WithStatusSubresource(instance, &disasterv1.DataSync{}, &disasterv1.ResourceSync{}).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// First pass may sync dependency labels and requeue.
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())
				// Second pass should perform the actual propagation logic.
				_, err = r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())

				dataSync := &disasterv1.DataSync{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name: "dr-ds-test-instance", Namespace: "default",
				}, dataSync)).To(Succeed())
				// Disabled 策略 → Schedule 应为空
				Expect(dataSync.Spec.Trigger.Schedule).To(BeEmpty())
			})
		})

		Context("当引用的 DataSyncPolicy 不存在时", func() {
			It("应该记录 Warning 事件并重新入队，不使用硬编码默认值", func() {
				config := createConfigWithPolicies("nonexistent-policy", "")
				instance := createReadyInstance("test-instance", "default")

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance). // 没有 policy CR
					WithStatusSubresource(instance, &disasterv1.DataSync{}, &disasterv1.ResourceSync{}).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// First pass may sync dependency labels and requeue.
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())

				result, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())
				// 应该重新入队等待策略就绪
				Expect(result.RequeueAfter).To(Equal(30 * time.Second))

				// 验证发出了 Warning 事件
				var foundWarning bool
				for len(recorder.Events) > 0 {
					event := <-recorder.Events
					if event != "" {
						foundWarning = true
						break
					}
				}
				Expect(foundWarning).To(BeTrue())
			})
		})

		Context("当 DisasterConfig 同时引用了 DataSyncPolicy 和 ResourceSyncPolicy 时", func() {
			It("DataSync 和 ResourceSync 的 Schedule 应分别遵守各自策略", func() {
				dataPolicy := &disasterv1.DisasterPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "data-policy",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterPolicySpec{
						Type:     disasterv1.PolicyTypeDataSync,
						Schedule: "*/30 * * * *",
						State:    disasterv1.PolicyStateEnabled,
					},
				}
				resPolicy := &disasterv1.DisasterPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "res-policy",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterPolicySpec{
						Type:     disasterv1.PolicyTypeResourceSync,
						Schedule: "0 3 * * *",
						State:    disasterv1.PolicyStateEnabled,
					},
				}
				config := createConfigWithPolicies("data-policy", "res-policy")
				instance := createReadyInstance("test-instance", "default")

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataPolicy, resPolicy).
					WithStatusSubresource(instance, &disasterv1.DataSync{}, &disasterv1.ResourceSync{}).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// First pass may sync dependency labels and requeue.
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())
				// Second pass should perform the actual propagation logic.
				_, err = r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())

				dataSync := &disasterv1.DataSync{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name: "dr-ds-test-instance", Namespace: "default",
				}, dataSync)).To(Succeed())
				Expect(dataSync.Spec.Trigger.Schedule).To(Equal("*/30 * * * *"))

				resourceSync := &disasterv1.ResourceSync{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name: "dr-rs-test-instance", Namespace: "default",
				}, resourceSync)).To(Succeed())
				Expect(resourceSync.Spec.Trigger.Schedule).To(Equal("0 3 * * *"))
			})
		})

		Context("当实例已处于 Protected 且策略发生变更时", func() {
			It("应将最新策略调度持续传播到既有 DataSync/ResourceSync", func() {
				dataPolicy := &disasterv1.DisasterPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "data-policy-protected",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterPolicySpec{
						Type:     disasterv1.PolicyTypeDataSync,
						Schedule: "*/1 * * * *",
						State:    disasterv1.PolicyStateEnabled,
					},
				}
				resPolicy := &disasterv1.DisasterPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "res-policy-protected",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterPolicySpec{
						Type:     disasterv1.PolicyTypeResourceSync,
						Schedule: "*/2 * * * *",
						State:    disasterv1.PolicyStateEnabled,
					},
				}
				config := createConfigWithPolicies("data-policy-protected", "res-policy-protected")

				instance := &disasterv1.DisasterInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-instance",
						Namespace:  "default",
						UID:        "test-uid-protected",
						Finalizers: []string{finalizerName},
					},
					Spec: disasterv1.DisasterInstanceSpec{
						Config: "test-config",
					},
					Status: disasterv1.DisasterInstanceStatus{
						FsmState:         disasterv1.FsmStateProtected,
						DataSyncName:     "dr-ds-test-instance",
						ResourceSyncName: "dr-rs-test-instance",
					},
				}

				dataSync := &disasterv1.DataSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-ds-test-instance",
						Namespace: "default",
					},
					Spec: disasterv1.DataSyncSpec{
						Instance: "test-instance",
						Trigger: disasterv1.TriggerSpec{
							Schedule: "",
						},
					},
				}

				resourceSync := &disasterv1.ResourceSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dr-rs-test-instance",
						Namespace: "default",
					},
					Spec: disasterv1.ResourceSyncSpec{
						Instance: "test-instance",
						Trigger: disasterv1.TriggerSpec{
							Schedule: "",
						},
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, dataPolicy, resPolicy, dataSync, resourceSync).
					WithStatusSubresource(instance, dataSync, resourceSync).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				// First pass may sync dependency labels and requeue.
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())
				// Second pass should execute handleProtected and policy schedule propagation.
				_, err = r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "test-instance", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())

				updatedDS := &disasterv1.DataSync{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "dr-ds-test-instance", Namespace: "default"}, updatedDS)).To(Succeed())
				Expect(updatedDS.Spec.Trigger.Schedule).To(Equal("*/1 * * * *"))

				updatedRS := &disasterv1.ResourceSync{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "dr-rs-test-instance", Namespace: "default"}, updatedRS)).To(Succeed())
				Expect(updatedRS.Spec.Trigger.Schedule).To(Equal("*/2 * * * *"))
			})
		})

		Context("当配置健康守卫生效时", func() {
			reconcileTwice := func(name, namespace string) {
				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      name,
						Namespace: namespace,
					},
				}
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				_, err = r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
			}

			It("配置不存在时应进入 ConfigError 并记录原状态", func() {
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateProtected
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateConfigError))
				Expect(updated.Status.LastStableFsmState).To(Equal(disasterv1.FsmStateProtected))
				Expect(updated.Status.Reason).To(Equal(instanceReasonConfigNotFound))
			})

			It("配置 NotReady 时应进入 ConfigError 并透传错误信息", func() {
				config := createTestConfig()
				config.Status.Status = disasterv1.DisasterConfigStatusNotReady
				config.Status.Reason = "SourceClusterNotReady"
				config.Status.Message = "source cluster still warming up"

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStatePaused
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance).
					WithStatusSubresource(instance, config).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateConfigError))
				Expect(updated.Status.LastStableFsmState).To(Equal(disasterv1.FsmStatePaused))
				Expect(updated.Status.Reason).To(Equal("SourceClusterNotReady"))
				Expect(updated.Status.Message).To(Equal("source cluster still warming up"))
			})

			It("保持 ConfigError 时不应覆盖 LastStableFsmState", func() {
				config := createTestConfig()
				config.Status.Status = disasterv1.DisasterConfigStatusNotReady
				config.Status.Reason = "ConfigDegraded"
				config.Status.Message = "still not ready"

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateConfigError
				instance.Status.LastStableFsmState = disasterv1.FsmStateActive
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance).
					WithStatusSubresource(instance, config).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateConfigError))
				Expect(updated.Status.LastStableFsmState).To(Equal(disasterv1.FsmStateActive))
			})

			It("配置恢复后应恢复到进入前的原状态", func() {
				config := createTestConfig()
				config.Status.Status = disasterv1.DisasterConfigStatusReady

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateConfigError
				instance.Status.LastStableFsmState = disasterv1.FsmStatePaused
				instance.Status.Reason = "ConfigNotReady"
				instance.Status.Message = "previous config issue"
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance).
					WithStatusSubresource(instance, config).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStatePaused))
				Expect(updated.Status.LastStableFsmState).To(BeEmpty())
				Expect(updated.Status.Reason).To(BeEmpty())
				Expect(updated.Status.Message).To(BeEmpty())
			})

			It("LastStableFsmState 缺失时应保持 ConfigError", func() {
				config := createTestConfig()
				config.Status.Status = disasterv1.DisasterConfigStatusReady

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateConfigError
				instance.Status.LastStableFsmState = ""
				instance.Finalizers = []string{finalizerName}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance).
					WithStatusSubresource(instance, config).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateConfigError))
				Expect(updated.Status.Reason).To(Equal(instanceReasonStableStateMissing))
			})

			It("FailingOver 状态不应被配置守卫覆盖", func() {
				config := createTestConfig()
				config.Status.Status = disasterv1.DisasterConfigStatusError
				config.Status.Reason = "SourceClusterDown"
				config.Status.Message = "source cluster unreachable"

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailingOver
				instance.Finalizers = []string{finalizerName}
				failoverOp := &disasterv1.DisasterOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-instance-failover-running",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterOperationSpec{
						InstanceName:  "test-instance",
						OperationType: disasterv1.OperationTypeFailover,
					},
					Status: disasterv1.DisasterOperationStatus{
						State: disasterv1.OperationStateRunning,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, failoverOp).
					WithStatusSubresource(instance, config, failoverOp).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailingOver))
				Expect(updated.Status.LastStableFsmState).To(BeEmpty())
			})
		})

		Context("当实例处于 FailingOver 且 failover 已无运行中操作时", func() {
			reconcileTwice := func(name, namespace string) {
				req := ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}}
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				_, err = r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
			}

			It("最近 failover 在 FinalSync 失败时，应回滚到 Protected", func() {
				config := createTestConfig()
				config.Status.Status = disasterv1.DisasterConfigStatusReady

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailingOver
				instance.Finalizers = []string{finalizerName}

				now := metav1.Now()
				failoverOp := &disasterv1.DisasterOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "failover-op-finalsync-failed",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterOperationSpec{
						InstanceName:  "test-instance",
						OperationType: disasterv1.OperationTypeFailover,
					},
					Status: disasterv1.DisasterOperationStatus{
						State:          disasterv1.OperationStateFailed,
						CurrentStep:    string(disasterv1.FailoverStepFinalSync),
						CompletionTime: &now,
						Message:        "StorageClassTargetNotFound: sc-e2e-target-missing",
						Steps: []disasterv1.StepStatus{
							{
								Name:           string(disasterv1.FailoverStepFinalSync),
								State:          "Failed",
								CompletionTime: &now,
							},
						},
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, failoverOp).
					WithStatusSubresource(config, instance, failoverOp).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
				Expect(updated.Status.Reason).To(BeEmpty())
				Expect(updated.Status.Message).To(BeEmpty())
				Expect(updated.Status.AvailableOperations).To(ContainElement("failover"))
			})

			It("最近 failover 已 Failed 但带 autoCancel 成功时，应保持 Protected 而不是误判为 Active", func() {
				config := createTestConfig()
				config.Status.Status = disasterv1.DisasterConfigStatusReady

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailingOver
				instance.Finalizers = []string{finalizerName}

				now := metav1.Now()
				failoverOp := &disasterv1.DisasterOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "failover-op-finalsync-autocancel-completed",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterOperationSpec{
						InstanceName:  "test-instance",
						OperationType: disasterv1.OperationTypeFailover,
					},
					Status: disasterv1.DisasterOperationStatus{
						State:                    disasterv1.OperationStateFailed,
						CompletionTime:           &now,
						Message:                  "故障切换在步骤 FinalSync 失败后已自动补偿，实例已恢复为 Protected",
						AutoCancelTriggered:      true,
						AutoCancelStatus:         disasterv1.OperationAutoCancelStatusSucceeded,
						AutoCancelMode:           disasterv1.OperationAutoCancelModeCancelPath,
						AutoCancelTriggerStep:    string(disasterv1.FailoverStepFinalSync),
						AutoCancelCompletionTime: &now,
						RoleStatus: &disasterv1.RoleStatus{
							PrimaryCluster:   "cluster-1",
							SecondaryCluster: "cluster-2",
						},
						Steps: []disasterv1.StepStatus{
							{
								Name:           string(disasterv1.FailoverStepFinalSync),
								State:          "Failed",
								CompletionTime: &now,
							},
						},
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, failoverOp).
					WithStatusSubresource(config, instance, failoverOp).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
				Expect(updated.Status.PrimaryCluster).To(Equal("cluster-1"))
				Expect(updated.Status.SecondaryCluster).To(Equal("cluster-2"))
				Expect(updated.Status.Reason).To(BeEmpty())
				Expect(updated.Status.Message).To(BeEmpty())
				Expect(updated.Status.AvailableOperations).To(ContainElement("failover"))
				Expect(updated.Status.AvailableOperations).NotTo(ContainElement("undo"))
			})

			It("最近 failover 在非回滚步骤失败时，应收敛到 Failed 并保留错误信息", func() {
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailingOver
				instance.Finalizers = []string{finalizerName}

				now := metav1.Now()
				failoverOp := &disasterv1.DisasterOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "failover-op-scaledown-failed",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterOperationSpec{
						InstanceName:  "test-instance",
						OperationType: disasterv1.OperationTypeFailover,
					},
					Status: disasterv1.DisasterOperationStatus{
						State:          disasterv1.OperationStateFailed,
						Reason:         "StepFailed",
						CurrentStep:    string(disasterv1.FailoverStepScaleDownSource),
						CompletionTime: &now,
						Message:        "步骤 ScaleDownSource 执行失败: target cluster update failed",
						Steps: []disasterv1.StepStatus{
							{
								Name:           string(disasterv1.FailoverStepScaleDownSource),
								State:          "Failed",
								CompletionTime: &now,
							},
						},
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance, failoverOp).
					WithStatusSubresource(instance, failoverOp).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
				Expect(updated.Status.Reason).To(Equal("StepFailed"))
				Expect(updated.Status.Message).To(ContainSubstring("ScaleDownSource"))
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reset", "cancel"}))
			})
		})

		Context("当实例处于 FailingBack 且 reprotect 已无运行中操作时", func() {
			reconcileTwice := func(name, namespace string) {
				req := ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}}
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				_, err = r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
			}

			It("最近 reprotect 完成时，应收敛到 Protected", func() {
				config := createTestConfig()
				config.Status.Status = disasterv1.DisasterConfigStatusReady

				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailingBack
				instance.Finalizers = []string{finalizerName}

				now := metav1.Now()
				reprotectOp := &disasterv1.DisasterOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "reprotect-op-completed",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterOperationSpec{
						InstanceName:  "test-instance",
						OperationType: disasterv1.OperationTypeReprotect,
					},
					Status: disasterv1.DisasterOperationStatus{
						State:          disasterv1.OperationStateCompleted,
						CompletionTime: &now,
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(config, instance, reprotectOp).
					WithStatusSubresource(config, instance, reprotectOp).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
				Expect(updated.Status.Reason).To(BeEmpty())
				Expect(updated.Status.Message).To(BeEmpty())
				Expect(updated.Status.AvailableOperations).To(ContainElement("failover"))
			})

			It("最近 reprotect 失败时，应收敛到 Failed 并保留错误信息", func() {
				instance := createTestInstance("test-instance", "default")
				instance.Status.FsmState = disasterv1.FsmStateFailingBack
				instance.Finalizers = []string{finalizerName}

				now := metav1.Now()
				reprotectOp := &disasterv1.DisasterOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "reprotect-op-failed",
						Namespace: "default",
					},
					Spec: disasterv1.DisasterOperationSpec{
						InstanceName:  "test-instance",
						OperationType: disasterv1.OperationTypeReprotect,
					},
					Status: disasterv1.DisasterOperationStatus{
						State:          disasterv1.OperationStateFailed,
						Reason:         "StepFailed",
						CompletionTime: &now,
						Message:        "步骤 PauseSchedules 执行失败: test timeout",
					},
				}

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance, reprotectOp).
					WithStatusSubresource(instance, reprotectOp).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
				Expect(updated.Status.Reason).To(Equal("StepFailed"))
				Expect(updated.Status.Message).To(ContainSubstring("PauseSchedules"))
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reset"}))
			})

			It("超时且无 Running reprotect 时，应从 FailingBack 收敛到 Failed", func() {
				instance := createTestInstance("test-instance", "default")
				instance.Finalizers = []string{finalizerName}
				instance.CreationTimestamp = metav1.NewTime(time.Now().Add(-10 * time.Minute))
				instance.Spec.OperationTimeoutMinutes = 1
				instance.Status.FsmState = disasterv1.FsmStateFailingBack

				fakeClient = fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(instance).
					WithStatusSubresource(instance).
					Build()

				r = &DisasterInstanceReconciler{
					Client:   fakeClient,
					Scheme:   s,
					Log:      ctrl.Log.WithName("test"),
					Recorder: recorder,
				}

				reconcileTwice("test-instance", "default")

				updated := &disasterv1.DisasterInstance{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updated)).To(Succeed())
				Expect(updated.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
				Expect(updated.Status.Reason).To(Equal(instanceReasonFailbackMissing))
				Expect(updated.Status.Message).To(ContainSubstring("FailingBack timeout exceeded"))
				Expect(updated.Status.AvailableOperations).To(Equal([]string{"reset"}))
			})
		})
	})
})
