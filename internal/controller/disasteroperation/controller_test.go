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

package disasteroperation

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	metadata "github.com/softcdata/testudo-operator/pkg/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestDisasterOperationController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DisasterOperation Controller Suite")
}

var _ = Describe("DisasterOperation Controller", func() {
	var (
		ctx        context.Context
		r          *DisasterOperationReconciler
		fakeClient client.Client
		s          *runtime.Scheme
		recorder   *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()

		s = runtime.NewScheme()
		Expect(scheme.AddToScheme(s)).To(Succeed())
		Expect(disasterv1.AddToScheme(s)).To(Succeed())

		recorder = record.NewFakeRecorder(100)
	})

	createTestInstance := func(name, namespace string) *disasterv1.DisasterInstance {
		return &disasterv1.DisasterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: disasterv1.DisasterInstanceStatus{
				FsmState:         disasterv1.FsmStateProtected,
				PrimaryCluster:   "cluster-1",
				SecondaryCluster: "cluster-2",
				DataSyncName:     "dr-ds-" + name,
				ResourceSyncName: "dr-rs-" + name,
			},
		}
	}

	createTestDataSync := func(name, namespace string) *disasterv1.DataSync {
		return &disasterv1.DataSync{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: disasterv1.DataSyncSpec{
				Instance: "test-instance",
				Paused:   false,
			},
		}
	}

	createTestResourceSync := func(name, namespace string) *disasterv1.ResourceSync {
		return &disasterv1.ResourceSync{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: disasterv1.ResourceSyncSpec{
				Instance: "test-instance",
				Paused:   false,
			},
		}
	}

	createTestOperation := func(opType disasterv1.OperationType) *disasterv1.DisasterOperation {
		return &disasterv1.DisasterOperation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-op",
				Namespace: "default",
			},
			Spec: disasterv1.DisasterOperationSpec{
				InstanceName:  "test-instance",
				OperationType: opType,
			},
		}
	}

	boolPtr := func(v bool) *bool {
		return &v
	}

	Describe("Pause 操作", func() {
		It("应该暂停 DataSync 和 ResourceSync 并更新 Instance 状态", func() {
			instance := createTestInstance("test-instance", "default")
			dataSync := createTestDataSync("dr-ds-test-instance", "default")
			resourceSync := createTestResourceSync("dr-rs-test-instance", "default")
			op := createTestOperation(disasterv1.OperationTypePause)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, dataSync, resourceSync, op).
				WithStatusSubresource(instance, dataSync, resourceSync, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			// 第一次调谐：初始化
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			// 第二次调谐：执行操作
			_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			// 验证 DataSync 暂停
			updatedDS := &disasterv1.DataSync{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "dr-ds-test-instance", Namespace: "default"}, updatedDS)
			Expect(updatedDS.Spec.Paused).To(BeTrue())

			// 验证 ResourceSync 暂停
			updatedRS := &disasterv1.ResourceSync{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "dr-rs-test-instance", Namespace: "default"}, updatedRS)
			Expect(updatedRS.Spec.Paused).To(BeTrue())

			// 验证 Instance 状态
			updatedInst := &disasterv1.DisasterInstance{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updatedInst)
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStatePaused))

			// 验证操作完成
			updatedOp := &disasterv1.DisasterOperation{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "test-op", Namespace: "default"}, updatedOp)
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateCompleted))
		})
	})

	Describe("Failover 操作 (Real)", func() {
		It("应该逐步执行故障切换并最终更新状态", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"

			op := createTestOperation(disasterv1.OperationTypeFailover)

			// Config
			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			// Fake KubeConfig
			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)

			c1 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}
			c2 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			// Add DataSync/ResourceSync
			ds := createTestDataSync("dr-ds-test-instance", "default")
			ds.Spec.Instance = "test-instance"
			// Initial state empty or Ready, LastSyncTime nil

			rs := createTestResourceSync("dr-rs-test-instance", "default")
			rs.Spec.Instance = "test-instance"

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, c1, c2, ds, rs).
				WithStatusSubresource(instance, op, config, c1, c2, ds, rs).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				// Inject Mock ClientFactory
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			// 第一次调谐：初始化
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op", Namespace: "default"}})

			// 第二次调谐：初始化步骤
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op", Namespace: "default"}})

			// 循环执行步骤直到完成
			for i := 0; i < 50; i++ {
				r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op", Namespace: "default"}})

				// Simulate DataSync Controller
				updatedDS := &disasterv1.DataSync{}
				fakeClient.Get(ctx, types.NamespacedName{Name: "dr-ds-test-instance", Namespace: "default"}, updatedDS)
				if updatedDS.Spec.Trigger.Manual != "" {
					updatedDS.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
					updatedDS.Status.State = disasterv1.DataSyncStateReady
					fakeClient.Status().Update(ctx, updatedDS)
				}

				// Simulate ResourceSync Controller
				updatedRS := &disasterv1.ResourceSync{}
				fakeClient.Get(ctx, types.NamespacedName{Name: "dr-rs-test-instance", Namespace: "default"}, updatedRS)
				if updatedRS.Spec.Trigger.Manual != "" {
					updatedRS.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
					updatedRS.Status.State = disasterv1.ResourceSyncStateReady
					fakeClient.Status().Update(ctx, updatedRS)
				}

				// Check completion
				updatedOp := &disasterv1.DisasterOperation{}
				fakeClient.Get(ctx, types.NamespacedName{Name: "test-op", Namespace: "default"}, updatedOp)
				if updatedOp.Status.State == disasterv1.OperationStateCompleted {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}

			// 验证 DisasterInstance 状态
			updatedInst := &disasterv1.DisasterInstance{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: "default"}, updatedInst)
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateActive))
			Expect(updatedInst.Status.PrimaryCluster).To(Equal("cluster-2")) // 切换了

			// 验证操作完成
			updatedOp := &disasterv1.DisasterOperation{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "test-op", Namespace: "default"}, updatedOp)
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateCompleted))
			Expect(updatedOp.Status.Steps).To(HaveLen(7))
			for _, step := range updatedOp.Status.Steps {
				Expect(step.State).To(Equal("Completed"))
			}
		})

		It("FinalSync 触发 DataSync 时遇到冲突应自动重试", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailingOver
			instance.Status.DataSyncName = "dr-ds-test-instance"
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			ds := createTestDataSync("dr-ds-test-instance", "default")
			ds.Spec.Instance = "test-instance"
			ds.Status.State = disasterv1.DataSyncStateReady

			rs := createTestResourceSync("dr-rs-test-instance", "default")
			rs.Spec.Instance = "test-instance"
			rs.Status.State = disasterv1.ResourceSyncStateReady

			now := metav1.Now()
			step := &disasterv1.StepStatus{
				Name:      string(disasterv1.FailoverStepFinalSync),
				State:     "Running",
				StartTime: &now,
			}
			op := createTestOperation(disasterv1.OperationTypeFailover)
			op.Annotations = map[string]string{
				metadata.AnnotationTraceID: "trace-finalsync-conflict",
			}

			conflictInjected := false
			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, ds, rs, op).
				WithStatusSubresource(instance, ds, rs, op).
				WithInterceptorFuncs(interceptor.Funcs{
					Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						if _, ok := obj.(*disasterv1.DataSync); ok && !conflictInjected {
							conflictInjected = true
							return errors.NewConflict(
								schema.GroupResource{Group: "testudo.softcdata.com", Resource: "datasyncs"},
								obj.GetName(),
								fmt.Errorf("injected conflict"),
							)
						}
						return c.Update(ctx, obj, opts...)
					},
				}).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			done, err := r.executeFinalSync(ctx, instance, step, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeFalse())
			Expect(conflictInjected).To(BeTrue())

			updatedDS := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, updatedDS)).To(Succeed())
			Expect(updatedDS.Spec.Trigger.Manual).NotTo(BeEmpty())
			Expect(updatedDS.Annotations[metadata.AnnotationLastTraceID]).To(Equal("trace-finalsync-conflict"))
		})

		It("ResumeSchedules 恢复同步任务时遇到冲突应自动重试", func() {
			instance := createTestInstance("resume-conflict-instance", "default")

			ds := createTestDataSync(instance.Status.DataSyncName, instance.Namespace)
			ds.Spec.Paused = true
			rs := createTestResourceSync(instance.Status.ResourceSyncName, instance.Namespace)
			rs.Spec.Paused = true

			dataSyncConflictInjected := false
			resourceSyncConflictInjected := false
			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, ds, rs).
				WithStatusSubresource(instance, ds, rs).
				WithInterceptorFuncs(interceptor.Funcs{
					Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						switch obj.(type) {
						case *disasterv1.DataSync:
							if !dataSyncConflictInjected {
								dataSyncConflictInjected = true
								return errors.NewConflict(
									schema.GroupResource{Group: "testudo.softcdata.com", Resource: "datasyncs"},
									obj.GetName(),
									fmt.Errorf("injected datasync conflict"),
								)
							}
						case *disasterv1.ResourceSync:
							if !resourceSyncConflictInjected {
								resourceSyncConflictInjected = true
								return errors.NewConflict(
									schema.GroupResource{Group: "testudo.softcdata.com", Resource: "resourcesyncs"},
									obj.GetName(),
									fmt.Errorf("injected resourcesync conflict"),
								)
							}
						}
						return c.Update(ctx, obj, opts...)
					},
				}).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			done, err := r.executeResumeSchedules(ctx, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeTrue())
			Expect(dataSyncConflictInjected).To(BeTrue())
			Expect(resourceSyncConflictInjected).To(BeTrue())

			updatedDS := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, updatedDS)).To(Succeed())
			Expect(updatedDS.Spec.Paused).To(BeFalse())
			updatedRS := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, updatedRS)).To(Succeed())
			Expect(updatedRS.Spec.Paused).To(BeFalse())
		})

		It("ResumeSchedules 应按 spec.instance 恢复 status 名称缺失的同步任务", func() {
			instance := createTestInstance("resume-by-instance", "default")
			instance.Status.DataSyncName = ""
			instance.Status.ResourceSyncName = ""

			ds := createTestDataSync("custom-ds", instance.Namespace)
			ds.Spec.Instance = instance.Name
			ds.Spec.Paused = true
			rs := createTestResourceSync("custom-rs", instance.Namespace)
			rs.Spec.Instance = instance.Name
			rs.Spec.Paused = true
			unrelatedDS := createTestDataSync("unrelated-ds", instance.Namespace)
			unrelatedDS.Spec.Instance = "other-instance"
			unrelatedDS.Spec.Paused = true

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, ds, rs, unrelatedDS).
				WithStatusSubresource(instance, ds, rs, unrelatedDS).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			done, err := r.executeResumeSchedules(ctx, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeTrue())

			updatedDS := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, updatedDS)).To(Succeed())
			Expect(updatedDS.Spec.Paused).To(BeFalse())
			updatedRS := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, updatedRS)).To(Succeed())
			Expect(updatedRS.Spec.Paused).To(BeFalse())
			updatedUnrelatedDS := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: unrelatedDS.Name, Namespace: unrelatedDS.Namespace}, updatedUnrelatedDS)).To(Succeed())
			Expect(updatedUnrelatedDS.Spec.Paused).To(BeTrue())
		})

		It("FinalSync 遇到 ResourceSync 失败应立即返回错误", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailingOver
			instance.Status.DataSyncName = "dr-ds-test-instance"
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			ds := createTestDataSync("dr-ds-test-instance", "default")
			ds.Spec.Instance = "test-instance"
			ds.Status.State = disasterv1.DataSyncStateReady
			ds.Status.LastSyncTime = &metav1.Time{Time: time.Now()}

			rs := createTestResourceSync("dr-rs-test-instance", "default")
			rs.Spec.Instance = "test-instance"
			rs.Status.State = disasterv1.ResourceSyncStateFailed
			rs.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
			rs.Status.Reason = "BuildRestoreSpecFailed"
			rs.Status.Message = "image rewrite unmatched"

			start := metav1.NewTime(time.Now().Add(-2 * time.Minute))
			step := &disasterv1.StepStatus{
				Name:      string(disasterv1.FailoverStepFinalSync),
				State:     "Running",
				StartTime: &start,
			}
			op := createTestOperation(disasterv1.OperationTypeFailover)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, ds, rs, op).
				WithStatusSubresource(instance, ds, rs, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			done, err := r.executeFinalSync(ctx, instance, step, op)
			Expect(err).To(HaveOccurred())
			Expect(done).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("ResourceSync dr-rs-test-instance is Failed"))
			Expect(err.Error()).To(ContainSubstring("image rewrite unmatched"))
		})

		It("FinalSync 遇到 DataSync 失败应立即返回错误", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailingOver
			instance.Status.DataSyncName = "dr-ds-test-instance"
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			ds := createTestDataSync("dr-ds-test-instance", "default")
			ds.Spec.Instance = "test-instance"
			ds.Status.State = disasterv1.DataSyncStateFailed
			ds.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
			ds.Status.Reason = "RestoreFailed"
			ds.Status.Message = "target restore failed"

			rs := createTestResourceSync("dr-rs-test-instance", "default")
			rs.Spec.Instance = "test-instance"
			rs.Status.State = disasterv1.ResourceSyncStateReady
			rs.Status.LastSyncTime = &metav1.Time{Time: time.Now()}

			start := metav1.NewTime(time.Now().Add(-2 * time.Minute))
			step := &disasterv1.StepStatus{
				Name:      string(disasterv1.FailoverStepFinalSync),
				State:     "Running",
				StartTime: &start,
			}
			op := createTestOperation(disasterv1.OperationTypeFailover)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, ds, rs, op).
				WithStatusSubresource(instance, ds, rs, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			done, err := r.executeFinalSync(ctx, instance, step, op)
			Expect(err).To(HaveOccurred())
			Expect(done).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("DataSync dr-ds-test-instance is Failed"))
			Expect(err.Error()).To(ContainSubstring("target restore failed"))
		})

		It("首次同步触发失败时不应写入 sync-triggered-at 注解", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateActive
			instance.Status.DataSyncName = "dr-ds-test-instance"
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			ds := createTestDataSync("dr-ds-test-instance", "default")
			ds.Spec.Instance = "test-instance"
			ds.Status.State = disasterv1.DataSyncStateReady

			op := createTestOperation(disasterv1.OperationTypeSyncData)
			op.Status.State = disasterv1.OperationStateRunning
			op.Annotations = map[string]string{
				metadata.AnnotationTraceID: "trace-sync-trigger-failure",
			}

			triggerErr := fmt.Errorf("injected datasync update failure")
			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, ds, op).
				WithStatusSubresource(instance, ds, op).
				WithInterceptorFuncs(interceptor.Funcs{
					Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
						if _, ok := obj.(*disasterv1.DataSync); ok {
							return triggerErr
						}
						return c.Update(ctx, obj, opts...)
					},
				}).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			latestOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, latestOp)).To(Succeed())

			res, err := r.handleSync(ctx, ctrl.Log.WithName("test"), latestOp, true, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(3 * time.Second))

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, updatedOp)).To(Succeed())
			Expect(updatedOp.Annotations["testudo.softcdata.com/sync-triggered-at"]).To(BeEmpty())
			Expect(updatedOp.Status.Message).To(ContainSubstring("同步触发失败"))

			updatedDS := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, updatedDS)).To(Succeed())
			Expect(updatedDS.Spec.Trigger.Manual).To(BeEmpty())
		})

		It("syncresource 失败重试时应设置 NextRetryTime 并清除触发标记", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateActive
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			rs := createTestResourceSync("dr-rs-test-instance", "default")
			rs.Spec.Instance = "test-instance"
			rs.Status.State = disasterv1.ResourceSyncStateFailed
			rs.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
			rs.Status.Reason = "InjectedFailure"
			rs.Status.Message = "injected resourcesync failure"

			triggeredAt := time.Now().Add(-2 * time.Second).Format(time.RFC3339)
			op := createTestOperation(disasterv1.OperationTypeSyncResource)
			op.Status.State = disasterv1.OperationStateRunning
			op.Spec.RetryPolicy = &disasterv1.RetryPolicy{MaxRetries: 5, RetryIntervalSeconds: 45}
			op.Annotations = map[string]string{
				metadata.AnnotationTraceID:                "trace-sync-retry-next-retry-time",
				"testudo.softcdata.com/sync-triggered-at": triggeredAt,
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, rs, op).
				WithStatusSubresource(instance, rs, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			latestOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, latestOp)).To(Succeed())

			res, err := r.handleSync(ctx, ctrl.Log.WithName("test"), latestOp, false, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(45 * time.Second))

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.RetryCount).To(Equal(int32(1)))
			Expect(updatedOp.Status.NextRetryTime).NotTo(BeNil())
			Expect(updatedOp.Annotations["testudo.softcdata.com/sync-triggered-at"]).To(BeEmpty())
		})

		It("NextRetryTime 未到时不应继续消耗 retryCount", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateActive
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			rs := createTestResourceSync("dr-rs-test-instance", "default")
			rs.Spec.Instance = "test-instance"
			rs.Status.State = disasterv1.ResourceSyncStateFailed
			rs.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
			rs.Status.Reason = "InjectedFailure"
			rs.Status.Message = "injected resourcesync failure"

			nextRetryTime := metav1.NewTime(time.Now().Add(45 * time.Second))
			op := createTestOperation(disasterv1.OperationTypeSyncResource)
			op.Status.State = disasterv1.OperationStateRunning
			op.Status.RetryCount = 1
			op.Status.NextRetryTime = &nextRetryTime
			op.Spec.RetryPolicy = &disasterv1.RetryPolicy{MaxRetries: 5, RetryIntervalSeconds: 45}
			op.Annotations = map[string]string{
				metadata.AnnotationTraceID: "trace-sync-retry-wait-gate",
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, rs, op).
				WithStatusSubresource(instance, rs, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			latestOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, latestOp)).To(Succeed())

			res, err := r.handleSync(ctx, ctrl.Log.WithName("test"), latestOp, false, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">=", 40*time.Second))

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.RetryCount).To(Equal(int32(1)))
			Expect(updatedOp.Status.NextRetryTime).NotTo(BeNil())
			Expect(updatedOp.Annotations["testudo.softcdata.com/sync-triggered-at"]).To(BeEmpty())

			updatedRS := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, updatedRS)).To(Succeed())
			Expect(updatedRS.Spec.Trigger.Manual).To(BeEmpty())
		})

		It("NextRetryTime 到点后应重新触发同步并清空重试门禁", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateActive
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			rs := createTestResourceSync("dr-rs-test-instance", "default")
			rs.Spec.Instance = "test-instance"
			rs.Status.State = disasterv1.ResourceSyncStateReady
			rs.Status.LastSyncTime = &metav1.Time{Time: time.Now().Add(-time.Minute)}

			nextRetryTime := metav1.NewTime(time.Now().Add(-time.Second))
			op := createTestOperation(disasterv1.OperationTypeSyncResource)
			op.Status.State = disasterv1.OperationStateRunning
			op.Status.RetryCount = 1
			op.Status.NextRetryTime = &nextRetryTime
			op.Spec.RetryPolicy = &disasterv1.RetryPolicy{MaxRetries: 5, RetryIntervalSeconds: 45}
			op.Annotations = map[string]string{
				metadata.AnnotationTraceID: "trace-sync-retry-expired-gate",
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, rs, op).
				WithStatusSubresource(instance, rs, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			latestOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, latestOp)).To(Succeed())

			res, err := r.handleSync(ctx, ctrl.Log.WithName("test"), latestOp, false, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(3 * time.Second))

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.NextRetryTime).To(BeNil())
			Expect(updatedOp.Annotations["testudo.softcdata.com/sync-triggered-at"]).NotTo(BeEmpty())

			updatedRS := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, updatedRS)).To(Succeed())
			Expect(updatedRS.Spec.Trigger.Manual).NotTo(BeEmpty())
			Expect(updatedRS.Annotations[metadata.AnnotationLastTraceID]).To(Equal("trace-sync-retry-expired-gate"))
		})
	})

	Describe("ScaleDownSource 跳过逻辑", func() {
		It("skipScaleDownSource=true 时应跳过缩零并返回成功", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "missing-config"

			op := createTestOperation(disasterv1.OperationTypeFailover)
			op.Spec.SkipScaleDownSource = true

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			done, err := r.executeScaleDownSource(ctx, instance, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeTrue())

			var evt string
			Eventually(recorder.Events, time.Second).Should(Receive(&evt))
			Expect(evt).To(ContainSubstring("ScaleDownSourceSkipped"))
		})

		It("skipScaleDownSource=false 时应保持原有缩零入口行为", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "missing-config"

			op := createTestOperation(disasterv1.OperationTypeFailover)
			op.Spec.SkipScaleDownSource = false

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			done, err := r.executeScaleDownSource(ctx, instance, op)
			Expect(err).To(HaveOccurred())
			Expect(done).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("failed to get config"))
		})

		It("annotation 兼容模式开启时应跳过缩零", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "missing-config"

			op := createTestOperation(disasterv1.OperationTypeFailover)
			op.Annotations = map[string]string{
				"testudo.softcdata.com/skip-scale-down-source": "true",
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			done, err := r.executeScaleDownSource(ctx, instance, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeTrue())
		})
	})

	Describe("Group 子操作参数透传", func() {
		It("应将 skipScaleDownSource 从父操作透传到子操作", func() {
			instance := createTestInstance("test-instance", "default")
			instance.UID = types.UID("instance-uid")

			group := &disasterv1.DisasterGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-group",
					Namespace: "default",
					UID:       types.UID("group-uid"),
				},
				Spec: disasterv1.DisasterGroupSpec{
					Levels: [][]string{{"test-instance"}},
					Policy: disasterv1.DisasterGroupPolicy{
						FailPolicy: "Stop",
					},
				},
			}

			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "group-op",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					GroupName:           "test-group",
					OperationType:       disasterv1.OperationTypeFailover,
					SkipScaleDownSource: true,
					SkipFinalSync:       true,
					SkipPodReadyCheck:   boolPtr(true),
					WaitUntilReady:      true,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, group, op).
				WithStatusSubresource(instance, group, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			for i := 0; i < 8; i++ {
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "group-op", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			childOp := &disasterv1.DisasterOperation{}
			err := fakeClient.Get(ctx, types.NamespacedName{Name: "group-op-test-instance", Namespace: "default"}, childOp)
			Expect(err).NotTo(HaveOccurred())
			Expect(childOp.Spec.SkipScaleDownSource).To(BeTrue())
			Expect(childOp.Spec.SkipFinalSync).To(BeTrue())
			Expect(childOp.Spec.SkipPodReadyCheck).NotTo(BeNil())
			Expect(*childOp.Spec.SkipPodReadyCheck).To(BeTrue())
			Expect(childOp.Spec.WaitUntilReady).To(BeTrue())
		})

		It("应将 skip-scale-down-source annotation 从父操作透传到子操作", func() {
			instance := createTestInstance("test-instance", "default")
			instance.UID = types.UID("instance-uid")

			group := &disasterv1.DisasterGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-group",
					Namespace: "default",
					UID:       types.UID("group-uid"),
				},
				Spec: disasterv1.DisasterGroupSpec{
					Levels: [][]string{{"test-instance"}},
					Policy: disasterv1.DisasterGroupPolicy{
						FailPolicy: "Stop",
					},
				},
			}

			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "group-op",
					Namespace: "default",
					Annotations: map[string]string{
						"testudo.softcdata.com/skip-scale-down-source": "true",
					},
				},
				Spec: disasterv1.DisasterOperationSpec{
					GroupName:     "test-group",
					OperationType: disasterv1.OperationTypeFailover,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, group, op).
				WithStatusSubresource(instance, group, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			for i := 0; i < 8; i++ {
				_, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{Name: "group-op", Namespace: "default"},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			childOp := &disasterv1.DisasterOperation{}
			err := fakeClient.Get(ctx, types.NamespacedName{Name: "group-op-test-instance", Namespace: "default"}, childOp)
			Expect(err).NotTo(HaveOccurred())
			Expect(childOp.Annotations).To(HaveKeyWithValue("testudo.softcdata.com/skip-scale-down-source", "true"))
		})
	})

	Describe("步骤失败终态收敛", func() {
		It("Failover 失败收敛写入实例状态时遇到冲突应自动重试", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "missing-config"
			instance.Status.FsmState = disasterv1.FsmStateFailingOver

			now := metav1.Now()
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-settle-conflict",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeFailover,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepPreCheck),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepPreCheck),
							State:     "Running",
							StartTime: &now,
						},
					},
				},
			}

			conflictInjected := false
			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				WithInterceptorFuncs(interceptor.Funcs{
					SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
						if subResourceName == "status" {
							if _, ok := obj.(*disasterv1.DisasterInstance); ok && !conflictInjected {
								conflictInjected = true
								return errors.NewConflict(
									schema.GroupResource{Group: "testudo.softcdata.com", Resource: "disasterinstances"},
									obj.GetName(),
									fmt.Errorf("injected settle conflict"),
								)
							}
						}
						return c.SubResource(subResourceName).Update(ctx, obj, opts...)
					},
				}).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(conflictInjected).To(BeTrue())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusSucceeded))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
		})

		It("Failover 的 PreCheck 失败后应将实例状态回置为 Protected", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "missing-config"
			instance.Status.FsmState = disasterv1.FsmStateProtected

			now := metav1.Now()
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeFailover,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepPreCheck),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepPreCheck),
							State:     "Running",
							StartTime: &now,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			for i := 0; i < 8; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterOperation{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
				if updated.Status.State == disasterv1.OperationStateCompleted || updated.Status.State == disasterv1.OperationStateFailed {
					break
				}
			}

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Steps).To(HaveLen(1))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Failed"))
			Expect(updatedOp.Status.Message).To(ContainSubstring("PreCheck"))
			Expect(updatedOp.Status.AutoCancelTriggered).To(BeTrue())
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusSucceeded))
			Expect(updatedOp.Status.AutoCancelMode).To(Equal(disasterv1.OperationAutoCancelModeDirectRollback))
			Expect(updatedOp.Status.AutoCancelTriggerStep).To(Equal(string(disasterv1.FailoverStepPreCheck)))
			Expect(updatedOp.Status.ManualInterventionRequired).To(BeFalse())

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
		})

		It("Failover 的 RestorePolicy dry-run 失败必须在 ScaleDownSource 前终止", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.RestorePolicy = &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(false),
				ModifierRules: []disasterv1.RestoreModifierRule{{
					ID:   "dsl-disabled",
					Mode: disasterv1.RestoreModifierModeVeleroNative,
					Conditions: disasterv1.Conditions{
						GroupResource: "deployments.apps",
					},
					VeleroRule: &disasterv1.RestoreModifierVeleroRule{
						Patches: []disasterv1.JSONPatch{{
							Operation: "add",
							Path:      "/metadata/annotations/patched-by",
							Value:     "platform",
						}},
					},
				}},
			}
			instance.Status.FsmState = disasterv1.FsmStateProtected

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)
			c1 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}
			c2 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			now := metav1.Now()
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-precheck-dryrun-fail",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeFailover,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepPreCheck),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepPreCheck),
							State:     "Running",
							StartTime: &now,
						},
						{
							Name:  string(disasterv1.FailoverStepScaleDownSource),
							State: "Pending",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, c1, c2).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Message).To(ContainSubstring("restore policy validation failed"))
			Expect(updatedOp.Status.Message).To(ContainSubstring("ModifierFeatureDisabled"))
			Expect(updatedOp.Status.Steps).To(HaveLen(2))
			Expect(updatedOp.Status.Steps[0].Name).To(Equal(string(disasterv1.FailoverStepPreCheck)))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Failed"))
			Expect(updatedOp.Status.Steps[1].Name).To(Equal(string(disasterv1.FailoverStepScaleDownSource)))
			Expect(updatedOp.Status.Steps[1].State).To(Equal("Pending"))
			Expect(updatedOp.Status.AutoCancelTriggered).To(BeTrue())
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusSucceeded))
			Expect(updatedOp.Status.AutoCancelMode).To(Equal(disasterv1.OperationAutoCancelModeDirectRollback))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
		})

		It("Failover 的 CancelPath 失败后应标记自动补偿失败并要求人工介入", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "missing-config"
			instance.Status.FsmState = disasterv1.FsmStateFailingOver
			instance.Status.PrimaryCluster = "src"
			instance.Status.SecondaryCluster = "dst"

			now := metav1.Now()
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-scaledown-fail",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeFailover,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepScaleDownSource),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepScaleDownSource),
							State:     "Running",
							StartTime: &now,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			for i := 0; i < 12; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterOperation{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
				if updated.Status.State == disasterv1.OperationStateFailed {
					break
				}
			}

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Steps).To(HaveLen(1))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Failed"))
			Expect(updatedOp.Status.Message).To(ContainSubstring("ScaleDownSource"))
			Expect(updatedOp.Status.AutoCancelTriggered).To(BeTrue())
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusFailed))
			Expect(updatedOp.Status.AutoCancelMode).To(Equal(disasterv1.OperationAutoCancelModeCancelPath))
			Expect(updatedOp.Status.AutoCancelTriggerStep).To(Equal(string(disasterv1.FailoverStepScaleDownSource)))
			Expect(updatedOp.Status.ManualInterventionRequired).To(BeTrue())
			Expect(updatedOp.Status.AutoCancelSteps).To(HaveLen(3))
			Expect(updatedOp.Status.AutoCancelSteps[0].State).To(Equal("Failed"))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
			Expect(updatedInst.Status.Reason).To(Equal(operationReasonStepFailed))
			Expect(updatedInst.Status.Message).To(ContainSubstring("cancel step"))
		})

		It("Failover 的 CancelPath 超时后应自动补偿回 Protected", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Status.FsmState = disasterv1.FsmStateFailingOver

			dataSync := createTestDataSync("dr-ds-test-instance", "default")
			dataSync.Spec.Paused = true
			resourceSync := createTestResourceSync("dr-rs-test-instance", "default")
			resourceSync.Spec.Paused = true

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}
			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)
			c1 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}
			c2 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			start := metav1.NewTime(time.Now().Add(-3 * time.Minute))
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-timeout",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:   "test-instance",
					OperationType:  disasterv1.OperationTypeFailover,
					TimeoutMinutes: 1,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepScaleUpTarget),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepScaleUpTarget),
							State:     "Running",
							StartTime: &start,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, c1, c2, dataSync, resourceSync).
				WithStatusSubresource(instance, op, dataSync, resourceSync).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			for i := 0; i < 12; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterOperation{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
				if updated.Status.State == disasterv1.OperationStateCompleted || updated.Status.State == disasterv1.OperationStateFailed {
					break
				}
			}

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Steps).To(HaveLen(1))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Failed"))
			Expect(updatedOp.Status.AutoCancelReason).To(ContainSubstring("超时"))
			Expect(updatedOp.Status.AutoCancelTriggered).To(BeTrue())
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusSucceeded))
			Expect(updatedOp.Status.AutoCancelMode).To(Equal(disasterv1.OperationAutoCancelModeCancelPath))
			Expect(updatedOp.Status.AutoCancelSteps).To(HaveLen(3))
			Expect(updatedOp.Status.AutoCancelSteps[0].State).To(Equal("Completed"))
			Expect(updatedOp.Status.AutoCancelSteps[1].State).To(Equal("Completed"))
			Expect(updatedOp.Status.AutoCancelSteps[2].State).To(Equal("Completed"))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))

			updatedDS := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: dataSync.Name, Namespace: dataSync.Namespace}, updatedDS)).To(Succeed())
			Expect(updatedDS.Spec.Paused).To(BeFalse())

			updatedRS := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: resourceSync.Name, Namespace: resourceSync.Namespace}, updatedRS)).To(Succeed())
			Expect(updatedRS.Spec.Paused).To(BeFalse())
		})

		It("CheckReplicas 超时时应携带阻塞资源详情", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailingOver

			start := metav1.NewTime(time.Now().Add(-3 * time.Minute))
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-timeout-checkreplicas",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:   "test-instance",
					OperationType:  disasterv1.OperationTypeFailover,
					TimeoutMinutes: 1,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepCheckReplicas),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepCheckReplicas),
							State:     "Running",
							StartTime: &start,
							Message:   "Pod app-ns/imap-0 Pending (reason=ImagePullBackOff)",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateRunning))
			Expect(updatedOp.Status.Message).To(ContainSubstring("已触发自动补偿"))
			Expect(updatedOp.Status.Steps[0].Message).To(ContainSubstring("阻塞详情"))
			Expect(updatedOp.Status.Steps[0].Message).To(ContainSubstring("imap-0"))
			Expect(updatedOp.Status.AutoCancelTriggered).To(BeTrue())
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusRunning))
			Expect(updatedOp.Status.AutoCancelMode).To(Equal(disasterv1.OperationAutoCancelModeCancelPath))
		})

		It("Failover 在 SwitchRoles 超时后不得进入自动补偿", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailingOver

			start := metav1.NewTime(time.Now().Add(-3 * time.Minute))
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-timeout-switchroles",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:   "test-instance",
					OperationType:  disasterv1.OperationTypeFailover,
					TimeoutMinutes: 1,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepSwitchRoles),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepSwitchRoles),
							State:     "Running",
							StartTime: &start,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.AutoCancelTriggered).To(BeFalse())
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusNotTriggered))
			Expect(updatedOp.Status.AutoCancelMode).To(Equal(disasterv1.OperationAutoCancelModeNoAutoCancel))
			Expect(updatedOp.Status.ManualInterventionRequired).To(BeTrue())

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
		})

		It("同一实例存在其他进行中的 failover 时应拒绝并发执行", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailingOver

			runningOp := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-running",
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

			newOp := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-new",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeFailover,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, runningOp, newOp).
				WithStatusSubresource(instance, runningOp, newOp).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: newOp.Name, Namespace: newOp.Namespace}}
			for i := 0; i < 5; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterOperation{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
				if updated.Status.State == disasterv1.OperationStateFailed {
					break
				}
			}

			updatedNewOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedNewOp)).To(Succeed())
			Expect(updatedNewOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedNewOp.Status.Reason).To(Equal(operationReasonInvalidState))
			Expect(updatedNewOp.Status.Message).To(ContainSubstring("存在进行中的 failover 操作"))
			Expect(updatedNewOp.Status.Message).To(ContainSubstring("failover-op-running"))
		})

		It("Reprotect 的 RestorePolicy dry-run 失败必须在 PauseSchedules 前终止", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.RestorePolicy = &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(false),
				ModifierRules: []disasterv1.RestoreModifierRule{{
					ID:   "dsl-disabled",
					Mode: disasterv1.RestoreModifierModeVeleroNative,
					Conditions: disasterv1.Conditions{
						GroupResource: "deployments.apps",
					},
					VeleroRule: &disasterv1.RestoreModifierVeleroRule{
						Patches: []disasterv1.JSONPatch{{
							Operation: "add",
							Path:      "/metadata/annotations/patched-by",
							Value:     "platform",
						}},
					},
				}},
			}
			instance.Status.FsmState = disasterv1.FsmStateActive

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)
			c1 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}
			c2 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			now := metav1.Now()
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reprotect-op-precheck-dryrun-fail",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeReprotect,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepPreCheck),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepPreCheck),
							State:     "Running",
							StartTime: &now,
						},
						{
							Name:  string(disasterv1.FailoverStepPauseSchedules),
							State: "Pending",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, c1, c2).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Message).To(ContainSubstring("restore policy validation failed"))
			Expect(updatedOp.Status.Message).To(ContainSubstring("ModifierFeatureDisabled"))
			Expect(updatedOp.Status.Steps).To(HaveLen(2))
			Expect(updatedOp.Status.Steps[0].Name).To(Equal(string(disasterv1.FailoverStepPreCheck)))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Failed"))
			Expect(updatedOp.Status.Steps[1].Name).To(Equal(string(disasterv1.FailoverStepPauseSchedules)))
			Expect(updatedOp.Status.Steps[1].State).To(Equal("Pending"))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
		})

		It("Reprotect 步骤超时后应将实例状态收敛为 Failed", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailingBack

			start := metav1.NewTime(time.Now().Add(-3 * time.Minute))
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reprotect-op-timeout",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:   "test-instance",
					OperationType:  disasterv1.OperationTypeReprotect,
					TimeoutMinutes: 1,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepPauseSchedules),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepPauseSchedules),
							State:     "Running",
							StartTime: &start,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Message).To(ContainSubstring("超时"))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
			Expect(updatedInst.Status.Reason).To(Equal(operationReasonStepFailed))
			Expect(updatedInst.Status.Message).To(ContainSubstring("reprotect step"))
		})

		It("实例处于 Failed 时应允许执行 Cancel", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailed

			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cancel-op-from-failed",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeCancel,
				},
				Status: disasterv1.DisasterOperationStatus{
					State: disasterv1.OperationStateRunning,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateRunning))
			Expect(updatedOp.Status.Message).NotTo(ContainSubstring("必须在 FailingOver"))
			Expect(updatedOp.Status.Steps).To(HaveLen(3))
			Expect(updatedOp.Status.CurrentStep).To(Equal(string(disasterv1.CancelStepScaleDownTarget)))
			Expect(updatedOp.Status.Steps[0].Name).To(Equal(string(disasterv1.CancelStepScaleDownTarget)))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Pending"))
		})

		It("实例已回到 Protected 时重复 Cancel 应幂等完成并恢复同步调度", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateProtected
			instance.Status.DataSyncName = ""
			instance.Status.ResourceSyncName = ""
			ds := createTestDataSync("custom-ds", instance.Namespace)
			ds.Spec.Instance = instance.Name
			ds.Spec.Paused = true
			rs := createTestResourceSync("custom-rs", instance.Namespace)
			rs.Spec.Instance = instance.Name
			rs.Spec.Paused = true

			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cancel-op-protected",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  instance.Name,
					OperationType: disasterv1.OperationTypeCancel,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.CancelStepResumeSchedules),
					Steps: []disasterv1.StepStatus{
						{
							Name:  string(disasterv1.CancelStepScaleDownTarget),
							State: "Completed",
						},
						{
							Name:  string(disasterv1.CancelStepScaleUpSource),
							State: "Completed",
						},
						{
							Name:  string(disasterv1.CancelStepResumeSchedules),
							State: "Pending",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, ds, rs, op).
				WithStatusSubresource(instance, ds, rs, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateCompleted))
			Expect(updatedOp.Status.Message).To(ContainSubstring("幂等操作完成"))
			Expect(updatedOp.Status.CurrentStep).To(BeEmpty())
			Expect(updatedOp.Status.Steps).To(HaveLen(3))
			Expect(updatedOp.Status.Steps[2].Name).To(Equal(string(disasterv1.CancelStepResumeSchedules)))
			Expect(updatedOp.Status.Steps[2].State).To(Equal("Completed"))
			Expect(updatedOp.Status.Steps[2].CompletionTime).NotTo(BeNil())

			updatedDS := &disasterv1.DataSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, updatedDS)).To(Succeed())
			Expect(updatedDS.Spec.Paused).To(BeFalse())
			updatedRS := &disasterv1.ResourceSync{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: rs.Name, Namespace: rs.Namespace}, updatedRS)).To(Succeed())
			Expect(updatedRS.Spec.Paused).To(BeFalse())
		})

		It("Cancel 步骤执行报错后应将实例状态收敛为 Failed", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "missing-config"
			instance.Status.FsmState = disasterv1.FsmStateFailingOver

			now := metav1.Now()
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cancel-op-fail",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeCancel,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.CancelStepScaleDownTarget),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.CancelStepScaleDownTarget),
							State:     "Running",
							StartTime: &now,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			for i := 0; i < 4; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterOperation{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
				if updated.Status.State == disasterv1.OperationStateFailed {
					break
				}
			}

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
			Expect(updatedInst.Status.Reason).To(Equal(operationReasonStepFailed))
			Expect(updatedInst.Status.Message).To(ContainSubstring("cancel step"))
		})

		It("Cancel 未设置超时时应使用默认超时避免步骤长期卡住", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateFailingOver
			instance.Spec.OperationTimeoutMinutes = 0

			start := metav1.NewTime(time.Now().Add(-time.Duration(defaultOperationTimeoutMinutes+1) * time.Minute))
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cancel-op-default-timeout",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeCancel,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.CancelStepScaleDownTarget),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.CancelStepScaleDownTarget),
							State:     "Running",
							StartTime: &start,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Message).To(ContainSubstring("超时"))
			Expect(updatedOp.Status.Steps[0].Message).To(ContainSubstring("超时设置 60 分钟"))
		})

		It("Undo 步骤执行报错时应立即写入 Failed 终态", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "missing-config"
			instance.Status.FsmState = disasterv1.FsmStateActive

			now := metav1.Now()
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undo-op",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeUndo,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.UndoStepScaleDownTarget),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.UndoStepScaleDownTarget),
							State:     "Running",
							StartTime: &now,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			for i := 0; i < 6; i++ {
				_, err := r.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				updated := &disasterv1.DisasterOperation{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, updated)).To(Succeed())
				if updated.Status.State == disasterv1.OperationStateFailed {
					break
				}
			}

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.CompletionTime).NotTo(BeNil())
			Expect(updatedOp.Status.Message).To(ContainSubstring("步骤 ScaleDownTarget 执行失败"))

			Expect(updatedOp.Status.Steps).To(HaveLen(1))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Failed"))
			Expect(updatedOp.Status.Steps[0].CompletionTime).NotTo(BeNil())
			Expect(updatedOp.Status.Steps[0].Message).To(ContainSubstring("failed to get config"))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
			Expect(updatedInst.Status.Reason).To(Equal(operationReasonStepFailed))
			Expect(updatedInst.Status.Message).To(ContainSubstring("undo step"))
		})

		It("Undo 步骤超时后应将实例状态收敛为 Failed", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Status.FsmState = disasterv1.FsmStateActive

			start := metav1.NewTime(time.Now().Add(-3 * time.Minute))
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "undo-op-timeout",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:   "test-instance",
					OperationType:  disasterv1.OperationTypeUndo,
					TimeoutMinutes: 1,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.UndoStepScaleDownTarget),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.UndoStepScaleDownTarget),
							State:     "Running",
							StartTime: &start,
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Message).To(ContainSubstring("超时"))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateFailed))
			Expect(updatedInst.Status.Reason).To(Equal(operationReasonStepFailed))
			Expect(updatedInst.Status.Message).To(ContainSubstring("undo step"))
		})
	})

	Describe("Drill Cleanup 操作", func() {
		It("无 NamespaceMapping 场景下，应执行 ScaleDown", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Namespaces = []string{"app-ns"}

			op := createTestOperation(disasterv1.OperationTypeDrillCleanup)

			// 设置 Deployment 以验证缩容
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deploy",
					Namespace: "app-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: func(i int32) *int32 { return &i }(2),
				},
			}

			// Fake KubeConfig and Cluster
			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
`)
			c2 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, c2, deploy).
				WithStatusSubresource(instance, op, c2).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			// 初始化执行
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
			// 执行清理逻辑
			_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			// 验证缩容
			updatedDeploy := &appsv1.Deployment{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "test-deploy", Namespace: "app-ns"}, updatedDeploy)
			Expect(*updatedDeploy.Spec.Replicas).To(Equal(int32(0)))

			updatedOp := &disasterv1.DisasterOperation{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "test-op", Namespace: "default"}, updatedOp)
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateCompleted))
		})

		It("有 NamespaceMapping 场景下，应直接删除目标拉起的 Namespace", func() {
			instance := createTestInstance("test-instance", "default")

			op := createTestOperation(disasterv1.OperationTypeDrillCleanup)
			op.Spec.DrillConfig = &disasterv1.DrillConfig{
				NamespaceMapping: map[string]string{
					"app-ns": "drill-app-ns",
				},
			}

			// 设置目标 Namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "drill-app-ns",
				},
			}

			// getClusterClient 会解析 KubeConfig 构建 rest.Config，因此这里必须提供可解析的 kubeconfig。
			// 注意：测试里 ClientFactory 会忽略该 rest.Config 并直接返回 fakeClient，
			// 因此只要 kubeconfig 能被解析即可，不要求实际可连通。
			fakeKubeConfig := []byte(`
apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://127.0.0.1:6443
    insecure-skip-tls-verify: true
users:
- name: u
  user:
    token: dummy
contexts:
- name: x
  context:
    cluster: c
    user: u
current-context: x
`)
			c2 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, c2, ns).
				WithStatusSubresource(instance, op, c2).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			// Reconcile 可能会因为 OwnerReference/依赖标签同步与状态机初始化多次 Requeue。
			// 这里循环执行直到进入终态，避免测试对 reconcile 次数的隐式假设。
			var err error
			for i := 0; i < 10; i++ {
				_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op", Namespace: "default"}})
				Expect(err).NotTo(HaveOccurred())

				updatedOp := &disasterv1.DisasterOperation{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-op", Namespace: "default"}, updatedOp)).To(Succeed())
				if updatedOp.Status.State == disasterv1.OperationStateCompleted || updatedOp.Status.State == disasterv1.OperationStateFailed {
					break
				}
			}

			updatedNs := &corev1.Namespace{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "drill-app-ns"}, updatedNs)
			// fakeClient delete without finalizers makes the object immediately absent. Note: wait for requeue logic is not needed for fakeClient unless we added finalizers
			Expect(errors.IsNotFound(err)).To(BeTrue())

			updatedOp := &disasterv1.DisasterOperation{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "test-op", Namespace: "default"}, updatedOp)
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateCompleted))
		})
	})

	Describe("结构化任务事件发射", func() {
		It("应在操作进入 Running 后发射 ExecutionStarted 事件", func() {
			instance := createTestInstance("test-instance", "default")
			dataSync := createTestDataSync("dr-ds-test-instance", "default")
			resourceSync := createTestResourceSync("dr-rs-test-instance", "default")
			op := createTestOperation(disasterv1.OperationTypePause)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, dataSync, resourceSync, op).
				WithStatusSubresource(instance, dataSync, resourceSync, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			// 第一次调谐: Pending -> Running
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Name:      op.Name,
				Namespace: op.Namespace,
			}})
			Expect(err).NotTo(HaveOccurred())

			// 第二次调谐: 触发结构化 Started 事件
			_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Name:      op.Name,
				Namespace: op.Namespace,
			}})
			Expect(err).NotTo(HaveOccurred())

			var events corev1.EventList
			Expect(fakeClient.List(ctx, &events, client.InNamespace("default"))).To(Succeed())

			found := false
			for _, e := range events.Items {
				if e.InvolvedObject.Kind == "DisasterOperation" &&
					e.InvolvedObject.Name == op.Name &&
					e.Reason == "ExecutionStarted" &&
					e.Labels["testudo.softcdata.com/task-event"] == "true" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "应该存在 DisasterOperation 的结构化 ExecutionStarted 事件")
		})

		It("应在操作失败后发射 ExecutionFinished(Warning) 事件", func() {
			// 不创建 Instance，触发 handlePause 失败路径
			op := createTestOperation(disasterv1.OperationTypePause)

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(op).
				WithStatusSubresource(op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}

			// 1) Pending -> Running
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// 2) Running -> Failed
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			// 3) 终态分支发射 Finished 事件
			_, err = r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: op.Name, Namespace: op.Namespace}, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Reason).NotTo(BeEmpty())

			var events corev1.EventList
			Expect(fakeClient.List(ctx, &events, client.InNamespace("default"))).To(Succeed())

			found := false
			for _, e := range events.Items {
				if e.InvolvedObject.Kind == "DisasterOperation" &&
					e.InvolvedObject.Name == op.Name &&
					e.Reason == "ExecutionFinished" &&
					e.Type == corev1.EventTypeWarning &&
					e.Labels["testudo.softcdata.com/task-event"] == "true" {
					payload := helper.DisasterEventPayload{}
					Expect(json.Unmarshal([]byte(e.Message), &payload)).To(Succeed())
					Expect(payload.ErrorCode).To(Equal(updatedOp.Status.Reason))
					Expect(payload.Message).To(Equal(updatedOp.Status.Message))
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "应该存在 DisasterOperation 的结构化 ExecutionFinished Warning 事件")
		})
	})

	Describe("CheckReplicas 判定", func() {
		It("默认配置（无 skip 覆盖）应等待 readyReplicas", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.Namespaces = []string{"app-ns"}
			instance.Status.ResourceSyncName = "dr-rs-test-instance"
			// SkipPodReadyCheck 未配置，按安全默认应等待 ready

			op := createTestOperation(disasterv1.OperationTypeFailover)

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)

			targetCluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			one := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "imap",
					Namespace: "app-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &one,
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas: 0, // 未就绪应阻断
				},
			}

			replicasRaw, err := json.Marshal(map[string]int32{
				"app-ns/deployments/imap": 1,
			})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replicas-dr-rs-test-instance",
					Namespace: "default",
				},
				Data: map[string]string{
					"replicas": string(replicasRaw),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, targetCluster, cm, deploy).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			done, err := r.executeCheckReplicas(ctx, instance, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeFalse())
		})

		It("实例默认 skipPodReadyCheck=false 且无操作覆盖时应等待 readyReplicas", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.Namespaces = []string{"app-ns"}
			instance.Status.ResourceSyncName = "dr-rs-test-instance"
			instance.Spec.SkipPodReadyCheck = boolPtr(false)

			op := createTestOperation(disasterv1.OperationTypeFailover)

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)

			targetCluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			one := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "imap",
					Namespace: "app-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &one,
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas: 0, // Spec 达标但 Ready 未达标
				},
			}

			replicasRaw, err := json.Marshal(map[string]int32{
				"app-ns/deployments/imap": 1,
			})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replicas-dr-rs-test-instance",
					Namespace: "default",
				},
				Data: map[string]string{
					"replicas": string(replicasRaw),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, targetCluster, cm, deploy).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			done, err := r.executeCheckReplicas(ctx, instance, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeFalse())
		})

		It("PVC Pending 且 StorageClass 不存在时应失败阻断", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.Namespaces = []string{"app-ns"}
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			op := createTestOperation(disasterv1.OperationTypeFailover)

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)

			targetCluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			// 仅创建无关 StorageClass，触发 sc-e2e-source-b not found 场景
			sc := &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{Name: "another-sc"},
			}
			scName := "sc-e2e-source-b"
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "data-imap-0",
					Namespace: "app-ns",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: &scName,
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimPending,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, targetCluster, sc, pvc).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			done, err := r.executeCheckReplicas(ctx, instance, op)
			Expect(done).To(BeFalse())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("StorageClass"))
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("存在 Pending Pod 时应阻断并禁止进入 SwitchRoles", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.Namespaces = []string{"app-ns"}
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			op := createTestOperation(disasterv1.OperationTypeFailover)
			op.Status.CurrentStep = string(disasterv1.FailoverStepCheckReplicas)
			op.Status.Steps = []disasterv1.StepStatus{
				{
					Name:  string(disasterv1.FailoverStepCheckReplicas),
					State: "Running",
				},
			}

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)

			targetCluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			one := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "imap",
					Namespace: "app-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &one,
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas: 1,
				},
			}
			replicasRaw, err := json.Marshal(map[string]int32{
				"app-ns/deployments/imap": 1,
			})
			Expect(err).NotTo(HaveOccurred())
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replicas-dr-rs-test-instance",
					Namespace: "default",
				},
				Data: map[string]string{
					"replicas": string(replicasRaw),
				},
			}

			pendingPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "imap-pending-0",
					Namespace: "app-ns",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "imap",
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "ImagePullBackOff",
								},
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, targetCluster, cm, deploy, pendingPod).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			done, err := r.executeCheckReplicas(ctx, instance, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeFalse())
			Expect(op.Status.Steps).To(HaveLen(1))
			Expect(op.Status.Steps[0].Message).To(ContainSubstring("Pod app-ns/imap-pending-0"))
			Expect(op.Status.Steps[0].Message).To(ContainSubstring("ImagePullBackOff"))
		})

		It("操作级 skipPodReadyCheck=true 应覆盖实例默认并跳过 readyReplicas 校验", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.Namespaces = []string{"app-ns"}
			instance.Status.ResourceSyncName = "dr-rs-test-instance"
			instance.Spec.SkipPodReadyCheck = boolPtr(false)

			op := createTestOperation(disasterv1.OperationTypeFailover)
			op.Spec.SkipPodReadyCheck = boolPtr(true)

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)

			targetCluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			one := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "imap",
					Namespace: "app-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &one,
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas: 0, // Skip ready 校验时不应阻塞
				},
			}

			replicasRaw, err := json.Marshal(map[string]int32{
				"app-ns/deployments/imap": 1,
			})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replicas-dr-rs-test-instance",
					Namespace: "default",
				},
				Data: map[string]string{
					"replicas": string(replicasRaw),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, targetCluster, cm, deploy).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			done, err := r.executeCheckReplicas(ctx, instance, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeTrue())
		})

		It("当工作负载缺失期望副本元数据且当前副本为 0 时不应判定完成", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.Namespaces = []string{"app-ns"}
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			op := createTestOperation(disasterv1.OperationTypeFailover)

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)

			targetCluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			zero := int32(0)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "imap",
					Namespace: "app-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &zero,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, targetCluster, deploy).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			done, err := r.executeCheckReplicas(ctx, instance, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeFalse())
		})

		It("当工作负载达到期望副本时应判定完成", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.Namespaces = []string{"app-ns"}
			instance.Status.ResourceSyncName = "dr-rs-test-instance"

			op := createTestOperation(disasterv1.OperationTypeFailover)

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)

			targetCluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			one := int32(1)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "imap",
					Namespace: "app-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &one,
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas: 1,
				},
			}

			replicasRaw, err := json.Marshal(map[string]int32{
				"app-ns/deployments/imap": 1,
			})
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replicas-dr-rs-test-instance",
					Namespace: "default",
				},
				Data: map[string]string{
					"replicas": string(replicasRaw),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, targetCluster, cm, deploy).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			done, err := r.executeCheckReplicas(ctx, instance, op)
			Expect(err).NotTo(HaveOccurred())
			Expect(done).To(BeTrue())
		})
		It("Failover 的提交期等价校验失败必须在 PreCheck 前置终止", func() {
			instance := createTestInstance("test-instance", "default")
			instance.Spec.Config = "test-config"
			instance.Spec.Namespaces = []string{"default"}
			instance.Spec.RestorePolicy = &disasterv1.RestorePolicy{
				UseUnifiedDirectionResolver: boolPtr(true),
				ModifierRules: []disasterv1.RestoreModifierRule{{
					ID:   "precheck-scope",
					Mode: disasterv1.RestoreModifierModeReversible,
					Conditions: disasterv1.Conditions{
						GroupResource: "deployments.apps",
						Namespaces:    []string{"default"},
					},
					Pair: &disasterv1.RestoreModifierPair{
						Path:        "/metadata/annotations/patched-by",
						SourceValue: "rev",
						TargetValue: "fwd",
					},
				}},
			}
			instance.Status.FsmState = disasterv1.FsmStateProtected

			config := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config"},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: "cluster-1",
					TargetCluster: "cluster-2",
				},
			}

			fakeKubeConfig := []byte(`
apiVersion: v1
clusters:
- cluster:
    server: https://1.2.3.4
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: x
current-context: x
kind: Config
preferences: {}
users: []
`)
			c1 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}
			c2 := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
				Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
			}

			now := metav1.Now()
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "failover-op-precheck-submission-validation-fail",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance",
					OperationType: disasterv1.OperationTypeFailover,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:       disasterv1.OperationStateRunning,
					CurrentStep: string(disasterv1.FailoverStepPreCheck),
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepPreCheck),
							State:     "Running",
							StartTime: &now,
						},
						{
							Name:  string(disasterv1.FailoverStepScaleDownSource),
							State: "Pending",
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, op, config, c1, c2).
				WithStatusSubresource(instance, op).
				Build()

			r = &DisasterOperationReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
				ModifierSubmissionValidator: func(ctx context.Context, instance *disasterv1.DisasterInstance, baselineSource, baselineTarget, sourceClusterName string) error {
					return fmt.Errorf("ModifierRuleRejected: simulated precheck validation failure")
				},
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: op.Name, Namespace: op.Namespace}}
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			updatedOp := &disasterv1.DisasterOperation{}
			Expect(fakeClient.Get(ctx, req.NamespacedName, updatedOp)).To(Succeed())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Message).To(ContainSubstring("submission validation failed"))
			Expect(updatedOp.Status.Steps).To(HaveLen(2))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Failed"))
			Expect(updatedOp.Status.Steps[1].State).To(Equal("Pending"))
			Expect(updatedOp.Status.AutoCancelTriggered).To(BeTrue())
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusSucceeded))
			Expect(updatedOp.Status.AutoCancelMode).To(Equal(disasterv1.OperationAutoCancelModeDirectRollback))

			updatedInst := &disasterv1.DisasterInstance{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, updatedInst)).To(Succeed())
			Expect(updatedInst.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
		})

	})
})
