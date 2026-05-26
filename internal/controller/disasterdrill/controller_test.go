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

package disasterdrill

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDisasterDrillController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DisasterDrill Controller Suite")
}

var _ = Describe("DisasterDrill Controller", func() {
	var (
		ctx        context.Context
		r          *DisasterDrillReconciler
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

	createTestInstance := func(name, namespace, primary, secondary string) *disasterv1.DisasterInstance {
		return &disasterv1.DisasterInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Status: disasterv1.DisasterInstanceStatus{
				FsmState:         disasterv1.FsmStateProtected,
				PrimaryCluster:   primary,
				SecondaryCluster: secondary,
			},
		}
	}

	createTestGroup := func(name, namespace string, levels [][]string) *disasterv1.DisasterGroup {
		return &disasterv1.DisasterGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: disasterv1.DisasterGroupSpec{
				Levels: levels,
			},
		}
	}

	createTestCluster := func(name string) *disasterv1.Cluster {
		return &disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Status: disasterv1.ClusterStatus{
				Status: "Ready",
			},
		}
	}

	Describe("目标集群回退与安全检测", func() {
		It("实例演练：不传目标集群时应自动回退到备集群，且未配置 NamespaceMapping 时仅警告不拦截", func() {
			instance := createTestInstance("inst-1", "default", "cluster-A", "cluster-B")
			clusterB := createTestCluster("cluster-B")

			// 演练：不指定 TargetCluster，不指定 NamespaceMapping
			drill := &disasterv1.DisasterDrill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-drill",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterDrillSpec{
					InstanceName: "inst-1",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, clusterB, drill).
				WithStatusSubresource(drill).
				Build()

			r = &DisasterDrillReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			// 调谐
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-drill", Namespace: "default"}}) // Add Finalizer
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-drill", Namespace: "default"}}) // Init Status (Pending)
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-drill", Namespace: "default"}}) // Handle Pending -> Ready

			updated := &disasterv1.DisasterDrill{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "test-drill", Namespace: "default"}, updated)

			// 验证放行：目标集群回退到 cluster-B，仅发出警告事件但不拦截
			Expect(updated.Status.State).To(Equal(disasterv1.DrillStateReady))
			Expect(updated.Status.TargetCluster).To(Equal("cluster-B"))
		})

		It("容灾组演练：未配置 NamespaceMapping 时仅警告不拦截", func() {
			inst1 := createTestInstance("inst-1", "default", "cluster-A", "cluster-B")
			inst2 := createTestInstance("inst-2", "default", "cluster-X", "cluster-Y")
			group := createTestGroup("group-1", "default", [][]string{{"inst-1", "inst-2"}})
			clusterY := createTestCluster("cluster-Y")

			// 演练：指定目标为 cluster-Y（inst-2 的备集群），无映射
			drill := &disasterv1.DisasterDrill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "group-drill",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterDrillSpec{
					GroupName:     "group-1",
					TargetCluster: "cluster-Y",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(inst1, inst2, group, clusterY, drill).
				WithStatusSubresource(drill).
				Build()

			r = &DisasterDrillReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "group-drill", Namespace: "default"}}) // Add Finalizer
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "group-drill", Namespace: "default"}}) // Init Status
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "group-drill", Namespace: "default"}}) // Handle Pending -> Ready

			updated := &disasterv1.DisasterDrill{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "group-drill", Namespace: "default"}, updated)

			// 验证放行：仅发出警告事件但不拦截
			Expect(updated.Status.State).To(Equal(disasterv1.DrillStateReady))
			Expect(updated.Status.TargetCluster).To(Equal("cluster-Y"))
		})

		It("容灾组演练：不传目标集群时应标记为 (Auto) 且通过安全查核 (若配置了映射)", func() {
			inst1 := createTestInstance("inst-1", "default", "cluster-A", "cluster-B")
			group := createTestGroup("group-1", "default", [][]string{{"inst-1"}})
			clusterB := createTestCluster("cluster-B")

			drill := &disasterv1.DisasterDrill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auto-drill",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterDrillSpec{
					GroupName: "group-1",
					NamespaceMapping: map[string]string{
						"ns1": "drill-ns1",
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(inst1, group, clusterB, drill).
				WithStatusSubresource(drill).
				Build()

			r = &DisasterDrillReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "auto-drill", Namespace: "default"}}) // Add Finalizer
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "auto-drill", Namespace: "default"}}) // Init Status
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "auto-drill", Namespace: "default"}}) // Handle Pending -> Ready

			updated := &disasterv1.DisasterDrill{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "auto-drill", Namespace: "default"}, updated)

			Expect(updated.Status.State).To(Equal(disasterv1.DrillStateReady))
			Expect(updated.Status.TargetCluster).To(Equal("(Auto)"))
		})
	})

	Describe("Drill Cleanup 状态转化", func() {
		It("演练完成且 CleanUp 为 true 时，应进入 CleaningUp 状态并创建 Operation", func() {
			drill := &disasterv1.DisasterDrill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cleanup-drill",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterDrillSpec{
					InstanceName: "inst-1",
					CleanUp:      true,
				},
				Status: disasterv1.DisasterDrillStatus{
					State:         disasterv1.DrillStateCompleted,
					TargetCluster: "cluster-B",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(drill).
				WithStatusSubresource(drill).
				Build()

			r = &DisasterDrillReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			// Handle CleanUp triggered from Completed state
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "cleanup-drill", Namespace: "default"}}) // Add Finalizer
			r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "cleanup-drill", Namespace: "default"}}) // triggers triggerCleanup

			updated := &disasterv1.DisasterDrill{}
			fakeClient.Get(ctx, types.NamespacedName{Name: "cleanup-drill", Namespace: "default"}, updated)

			Expect(updated.Status.State).To(Equal(disasterv1.DrillStateCleaningUp))
			Expect(updated.Status.OperationName).To(ContainSubstring("drill-cln-cleanup-drill"))
		})

		It("清理 Operation 失败后再次调谐不应重复创建清理 Operation", func() {
			cleanupOp := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cleanup-op",
					Namespace: "default",
					Labels: map[string]string{
						"testudo.softcdata.com/drill": "cleanup-failed-drill",
					},
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "missing-inst",
					OperationType: disasterv1.OperationTypeDrillCleanup,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:   disasterv1.OperationStateFailed,
					Reason:  "ResourceNotFound",
					Message: "DisasterInstance 未找到",
				},
			}
			drill := &disasterv1.DisasterDrill{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cleanup-failed-drill",
					Namespace:  "default",
					Finalizers: []string{drillFinalizer},
				},
				Spec: disasterv1.DisasterDrillSpec{
					InstanceName: "missing-inst",
					CleanUp:      true,
				},
				Status: disasterv1.DisasterDrillStatus{
					State:         disasterv1.DrillStateCleaningUp,
					OperationName: cleanupOp.Name,
					TargetCluster: "cluster-B",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(drill, cleanupOp).
				WithStatusSubresource(drill, cleanupOp).
				Build()

			r = &DisasterDrillReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "cleanup-failed-drill", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.DisasterDrill{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "cleanup-failed-drill", Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.State).To(Equal(disasterv1.DrillStateFailed))
			Expect(updated.Status.OperationName).To(Equal(cleanupOp.Name))
			Expect(updated.Status.Reason).To(Equal("ResourceNotFound"))

			_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "cleanup-failed-drill", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			ops := &disasterv1.DisasterOperationList{}
			Expect(fakeClient.List(ctx, ops, client.InNamespace("default"), client.MatchingLabels{"testudo.softcdata.com/drill": "cleanup-failed-drill"})).To(Succeed())
			Expect(ops.Items).To(HaveLen(1))
			Expect(ops.Items[0].Name).To(Equal(cleanupOp.Name))
		})
	})

	Describe("Drill RestorePolicy 透传", func() {
		It("Ready+Confirmed 的演练应将 drill 级 restorePolicy 透传到 DisasterOperation", func() {
			instance := createTestInstance("inst-1", "default", "cluster-A", "cluster-B")
			drill := &disasterv1.DisasterDrill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "policy-drill",
					Namespace: "default",
					Finalizers: []string{
						drillFinalizer,
					},
				},
				Spec: disasterv1.DisasterDrillSpec{
					InstanceName: "inst-1",
					Confirmed:    true,
					RestorePolicy: &disasterv1.RestorePolicy{
						UseUnifiedDirectionResolver: func() *bool {
							v := true
							return &v
						}(),
						ModifierRules: []disasterv1.RestoreModifierRule{{
							ID:   "drill-only-rule",
							Mode: disasterv1.RestoreModifierModeReversible,
							Pair: &disasterv1.RestoreModifierPair{
								Path:        "/metadata/annotations/drill-only",
								SourceValue: "source",
								TargetValue: "target",
							},
						}},
					},
				},
				Status: disasterv1.DisasterDrillStatus{
					State:         disasterv1.DrillStateReady,
					TargetCluster: "cluster-B",
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance, drill).
				WithStatusSubresource(drill).
				Build()

			r = &DisasterDrillReconciler{
				Client:   fakeClient,
				Scheme:   s,
				Log:      ctrl.Log.WithName("test"),
				Recorder: recorder,
			}

			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "policy-drill", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			ops := &disasterv1.DisasterOperationList{}
			Expect(fakeClient.List(ctx, ops, client.InNamespace("default"))).To(Succeed())
			Expect(ops.Items).To(HaveLen(1))
			Expect(ops.Items[0].Spec.DrillConfig).NotTo(BeNil())
			Expect(ops.Items[0].Spec.DrillConfig.RestorePolicy).NotTo(BeNil())
			Expect(ops.Items[0].Spec.DrillConfig.RestorePolicy.ModifierRules).To(HaveLen(1))
			Expect(ops.Items[0].Spec.DrillConfig.RestorePolicy.ModifierRules[0].ID).To(Equal("drill-only-rule"))
		})
	})
})
