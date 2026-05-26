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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/softcdata/testudo-operator/internal/controller/datasync"
	"github.com/softcdata/testudo-operator/internal/controller/disasteroperation"
	"github.com/softcdata/testudo-operator/internal/controller/resourcesync"
	"github.com/softcdata/testudo-operator/internal/controller/scheduler"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
)

var _ = Describe("DisasterInstance 完整生命周期集成模拟", func() {
	var (
		ctx            context.Context
		fakeClient     client.Client
		s              *runtime.Scheme
		recorder       *record.FakeRecorder
		instReconciler *DisasterInstanceReconciler
		dsReconciler   *datasync.DataSyncReconciler
		rsReconciler   *resourcesync.ResourceSyncReconciler
		opReconciler   *disasteroperation.DisasterOperationReconciler
		syncSched      *scheduler.SyncScheduler
	)

	BeforeEach(func() {
		ctx = context.Background()
		s = runtime.NewScheme()
		Expect(scheme.AddToScheme(s)).To(Succeed())
		Expect(disasterv1.AddToScheme(s)).To(Succeed())
		recorder = record.NewFakeRecorder(1000)

		var err error
		syncSched, err = scheduler.NewSyncScheduler()
		Expect(err).NotTo(HaveOccurred())
		syncSched.Start()

		// 初始化 Client (初始为空)
		fakeClient = fake.NewClientBuilder().
			WithScheme(s).
			WithStatusSubresource(
				&disasterv1.DisasterInstance{},
				&disasterv1.DataSync{},
				&disasterv1.ResourceSync{},
				&disasterv1.DisasterOperation{},
			).
			Build()

		// 初始化所有 Reconcilers
		instReconciler = &DisasterInstanceReconciler{
			Client:   fakeClient,
			Scheme:   s,
			Log:      ctrl.Log.WithName("inst-ctrl"),
			Recorder: recorder,
		}
		dsReconciler = &datasync.DataSyncReconciler{
			Client:    fakeClient,
			Scheme:    s,
			Log:       ctrl.Log.WithName("ds-ctrl"),
			Recorder:  recorder,
			Scheduler: syncSched,
		}
		rsReconciler = &resourcesync.ResourceSyncReconciler{
			Client:    fakeClient,
			Scheme:    s,
			Log:       ctrl.Log.WithName("rs-ctrl"),
			Recorder:  recorder,
			Scheduler: syncSched,
		}
		opReconciler = &disasteroperation.DisasterOperationReconciler{
			Client:   fakeClient,
			Scheme:   s,
			Log:      ctrl.Log.WithName("op-ctrl"),
			Recorder: recorder,
		}
		opReconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
			return fakeClient, nil
		}
	})

	AfterEach(func() {
		_ = syncSched.Shutdown()
	})

	It("应成功执行从创建到保护，再到故障切换和删除的完整流程", func() {
		By("1. 创建基础配置和 DisasterInstance")
		config := &disasterv1.DisasterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "demo-config"},
			Spec: disasterv1.DisasterConfigSpec{
				SourceCluster:  "cluster-A",
				TargetCluster:  "cluster-B",
				DataSyncPolicy: "policy-ds",
			},
		}
		// 补充 DataSyncPolicy CR（新增的策略调度传播逻辑需要）
		policyDS := &disasterv1.DisasterPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "policy-ds", Namespace: "default"},
			Spec: disasterv1.DisasterPolicySpec{
				Type:     disasterv1.PolicyTypeDataSync,
				Schedule: "*/15 * * * *",
				State:    disasterv1.PolicyStateEnabled,
			},
		}
		Expect(fakeClient.Create(ctx, policyDS)).To(Succeed())
		Expect(fakeClient.Create(ctx, config)).To(Succeed())

		// 为 Failover 的 PreCheck 提供可用的 Cluster 对象（getClusterClient 依赖）
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
		Expect(fakeClient.Create(ctx, &disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-A"},
			Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
		})).To(Succeed())
		Expect(fakeClient.Create(ctx, &disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-B"},
			Spec:       disasterv1.ClusterSpec{KubeConfig: fakeKubeConfig},
		})).To(Succeed())

		instance := &disasterv1.DisasterInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "demo-app", Namespace: "default"},
			Spec:       disasterv1.DisasterInstanceSpec{Config: "demo-config"},
		}
		Expect(fakeClient.Create(ctx, instance)).To(Succeed())

		// 定义 Reconcile Helpers
		reconcileInstance := func() {
			_, err := instReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "demo-app", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
		}
		reconcileDataSync := func() {
			_, err := dsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "dr-ds-demo-app", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
		}
		reconcileResourceSync := func() {
			_, err := rsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "dr-rs-demo-app", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())
		}

		By("2. 驱动状态机: Pending -> Initializing")
		reconcileInstance() // 添加 Finalizer
		reconcileInstance() // 初始化 FsmState -> Pending
		reconcileInstance() // Pending -> Initializing, 创建子资源

		// 验证子资源已创建
		ds := &disasterv1.DataSync{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "dr-ds-demo-app", Namespace: "default"}, ds)).To(Succeed())

		rs := &disasterv1.ResourceSync{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "dr-rs-demo-app", Namespace: "default"}, rs)).To(Succeed())

		// 验证 Instance 状态
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "demo-app", Namespace: "default"}, instance)).To(Succeed())
		Expect(instance.Status.FsmState).To(Equal(disasterv1.FsmStateInitializing))

		By("3. 驱动子资源就绪")
		// 模拟 DataSync 就绪
		reconcileDataSync() // 初始化 State
		// 重新获取以更新 ResourceVersion
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "dr-ds-demo-app", Namespace: "default"}, ds)).To(Succeed())
		ds.Status.State = disasterv1.DataSyncStateReady
		ds.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
		Expect(fakeClient.Status().Update(ctx, ds)).To(Succeed())

		// 模拟 ResourceSync 就绪
		reconcileResourceSync() // 初始化 State
		// 重新获取以更新 ResourceVersion
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "dr-rs-demo-app", Namespace: "default"}, rs)).To(Succeed())
		rs.Status.State = disasterv1.ResourceSyncStateReady
		rs.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
		Expect(fakeClient.Status().Update(ctx, rs)).To(Succeed())

		By("4. 驱动状态机: Initializing -> Protected")
		reconcileInstance() // 检查子资源，更新状态

		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "demo-app", Namespace: "default"}, instance)).To(Succeed())
		Expect(instance.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
		Expect(instance.Status.AvailableOperations).To(ContainElement("failover"))

		By("5. 执行 Failover 操作")
		opFailover := &disasterv1.DisasterOperation{
			ObjectMeta: metav1.ObjectMeta{Name: "op-failover", Namespace: "default"},
			Spec: disasterv1.DisasterOperationSpec{
				InstanceName:  "demo-app",
				OperationType: disasterv1.OperationTypeFailover,
			},
		}
		Expect(fakeClient.Create(ctx, opFailover)).To(Succeed())

		// 执行所有步骤（循环驱动直到进入终态）
		for i := 0; i < 200; i++ {
			res, err := opReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "op-failover", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			// 模拟 FinalSync 完成 (当被触发时)
			var ds disasterv1.DataSync
			if err := fakeClient.Get(ctx, types.NamespacedName{Name: "dr-ds-demo-app", Namespace: "default"}, &ds); err == nil {
				if ds.Spec.Trigger.Manual != "" {
					// 只有当时间较旧时更新，防止无限刷新
					// 但这里简单总是设置为 Now，因为 Reconciler 只要看到 Ready 且 Recent 就会通过
					ds.Status.State = disasterv1.DataSyncStateReady
					ds.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
					_ = fakeClient.Status().Update(ctx, &ds)
				}
			}
			var rs disasterv1.ResourceSync
			if err := fakeClient.Get(ctx, types.NamespacedName{Name: "dr-rs-demo-app", Namespace: "default"}, &rs); err == nil {
				if rs.Spec.Trigger.Manual != "" {
					rs.Status.State = disasterv1.ResourceSyncStateReady
					rs.Status.LastSyncTime = &metav1.Time{Time: time.Now()}
					_ = fakeClient.Status().Update(ctx, &rs)
				}
			}

			// 进入终态后退出循环
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "op-failover", Namespace: "default"}, opFailover)).To(Succeed())
			if opFailover.Status.State == disasterv1.OperationStateCompleted || opFailover.Status.State == disasterv1.OperationStateFailed {
				break
			}
			_ = res
			time.Sleep(10 * time.Millisecond)
		}

		// 验证操作完成
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "op-failover", Namespace: "default"}, opFailover)).To(Succeed())
		Expect(opFailover.Status.State).To(Equal(disasterv1.OperationStateCompleted))

		// 验证实例状态变为 Active 且主备切换
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "demo-app", Namespace: "default"}, instance)).To(Succeed())
		Expect(instance.Status.FsmState).To(Equal(disasterv1.FsmStateActive))
		Expect(instance.Status.PrimaryCluster).To(Equal("cluster-B")) // 初始是 A->B，现在应该是 B

		By("6. 执行 Reprotect 操作")
		opReprotect := &disasterv1.DisasterOperation{
			ObjectMeta: metav1.ObjectMeta{Name: "op-reprotect", Namespace: "default"},
			Spec: disasterv1.DisasterOperationSpec{
				InstanceName:  "demo-app",
				OperationType: disasterv1.OperationTypeReprotect,
			},
		}
		Expect(fakeClient.Create(ctx, opReprotect)).To(Succeed())

		// 执行步骤（循环驱动直到进入终态）
		for i := 0; i < 200; i++ {
			res, err := opReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "op-reprotect", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "op-reprotect", Namespace: "default"}, opReprotect)).To(Succeed())
			if opReprotect.Status.State == disasterv1.OperationStateCompleted || opReprotect.Status.State == disasterv1.OperationStateFailed {
				break
			}
			_ = res
		}

		// 验证 Reprotect 完成
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "op-reprotect", Namespace: "default"}, opReprotect)).To(Succeed())
		Expect(opReprotect.Status.State).To(Equal(disasterv1.OperationStateCompleted))

		// 验证 Instance 状态恢复
		Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "demo-app", Namespace: "default"}, instance)).To(Succeed())
		Expect(instance.Status.FsmState).To(Equal(disasterv1.FsmStateProtected))
		Expect(instance.Status.PrimaryCluster).To(Equal("cluster-B")) // Reprotect 后，B 仍为主，但状态变回 Protected

		By("7. 删除实例 (应允许)")
		// now := metav1.Now()
		// instance.DeletionTimestamp = &now
		// Expect(fakeClient.Update(ctx, instance)).To(Succeed())
		Expect(fakeClient.Delete(ctx, instance)).To(Succeed())

		reconcileInstance()

		// 验证 Instance 已被(模拟)删除 (Get 返回 NotFound 或者 Finalizer 空)
		err := fakeClient.Get(ctx, types.NamespacedName{Name: "demo-app", Namespace: "default"}, instance)
		if err == nil {
			Expect(instance.Finalizers).NotTo(ContainElement("testudo.softcdata.com/disasterinstance-finalizer"))
		}
		// 如果返回 NotFound 也算成功
		Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
	})
})
