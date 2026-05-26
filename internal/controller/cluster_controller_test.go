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
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	"github.com/softcdata/testudo-operator/pkg/tools"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

func buildVeleroCleanupTargetClient(scheme *runtime.Scheme, ns string) client.Client {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	}
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "backups.velero.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "velero.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "backups",
				Singular: "backup",
				Kind:     "Backup",
			},
			Scope: "Namespaced",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
					},
				},
			},
		},
	}
	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "velero-upgrade-crds"}}
	roleBinding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "velero-upgrade-crds"}}
	return fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, crd, role, roleBinding).
		Build()
}

var _ = Describe("Cluster Controller", func() {
	Context("When reconciling a resource", func() {
		var (
			resourceName       string
			veleroNamespace    string
			testNamespace      string
			ctx                context.Context
			cluster            *disasterv1.Cluster
			typeNamespacedName types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()
			resourceName = fmt.Sprintf("test-cluster-%d", time.Now().UnixNano())
			veleroNamespace = fmt.Sprintf("velero-%d", time.Now().UnixNano())
			testNamespace = fmt.Sprintf("test-ns-%d", time.Now().UnixNano())

			// Update the global variable for the controller to use
			VeleroNamespace = veleroNamespace

			typeNamespacedName = types.NamespacedName{
				Name:      resourceName,
				Namespace: testNamespace,
			}

			// Create Velero Namespace
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: veleroNamespace}}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Create Test Namespace
			testNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			Expect(k8sClient.Create(ctx, testNs)).To(Succeed())
		})

		AfterEach(func() {
			// 避免跨测试污染全局变量，其他测试用例默认依赖 "velero"。
			VeleroNamespace = "velero"
		})

		It("should reconcile an existing velero install on first create when veleroInstall is configured", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			veleroDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "velero",
					Namespace: veleroNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "velero"}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "velero"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "velero", Image: "velero/velero:v1.17.0"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, veleroDeployment)).To(Succeed())

			nodeAgent := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-agent",
					Namespace: veleroNamespace,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "node-agent"}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"name": "node-agent"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "velero/velero:v1.17.0"}}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, nodeAgent)).To(Succeed())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
					VeleroInstall: &disasterv1.VeleroInstallSpec{
						ImageRegistry: "registry.example.com/disaster",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			mockExecutor := &MockCommandExecutor{}
			reconciler := &ClusterReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        record.NewFakeRecorder(100),
				CommandExecutor: mockExecutor,
			}

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(mockExecutor.CalledWith).NotTo(BeEmpty())
			Expect(mockExecutor.CalledWith[0][0]).To(Equal("helm"))
			Expect(mockExecutor.CalledWith[0][1]).To(Equal("upgrade"))
		})

		// 测试正常调和流程：验证 Velero 安装、ServerStatusRequest 创建及 Finalizer 添加
		It("should successfully reconcile the resource", func() {
			By("creating the custom resource for the Kind Cluster")

			// Generate kubeconfig pointing to the test env
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationTraceID: "test-trace-id",
					},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create the Reconciler
			mockExecutor := &MockCommandExecutor{}
			reconciler := &ClusterReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Recorder:                record.NewFakeRecorder(100),
				CommandExecutor:         mockExecutor,
				ForceVeleroNotInstalled: true,
			}

			// Trigger Reconcile
			// First run: dependency-label sync (metadata update) and Requeue
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeTrue())

			// Second run: initialization (set Pending) and Requeue (1s)
			res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(1 * time.Second))

			// Simulate Velero Installation (Create Deployment)
			veleroDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "velero",
					Namespace: veleroNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "velero"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "velero"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "velero", Image: "velero/velero:v1.17.0"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, veleroDeployment)).To(Succeed())

			// Manually create SSR to ensure it exists and avoid race conditions/visibility issues
			ssrName := fmt.Sprintf("disaster-cluster-operator-%s", resourceName)
			ssr := &velerov1.ServerStatusRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ssrName,
					Namespace: veleroNamespace,
				},
			}
			// Ignore error if it already exists (in case controller created it fast)
			if err := k8sClient.Create(ctx, ssr); err != nil {
				Expect(apierrors.IsAlreadyExists(err)).To(BeTrue())
			}

			// Third run: Should check version, find SSR (empty status), and Requeue (3s)
			res, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(3 * time.Second))

			// Verify that InstallVeleroInCluster was called
			Expect(len(mockExecutor.CalledWith)).To(BeNumerically(">=", 1))
			Expect(mockExecutor.CalledWith[0][0]).To(Equal("helm"))
			Expect(mockExecutor.CalledWith[0][1]).To(Equal("upgrade"))

			// Verify Finalizer
			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Finalizers).To(ContainElement(LabelClusterFinalizer))
			Expect(updatedCluster.Status.Reason).NotTo(BeEmpty())
			Expect(updatedCluster.Status.Message).NotTo(BeEmpty())

			// TODO: Simulate Velero server updating the ServerStatusRequest and verify stats collection.
			// Currently encountering issues with envtest visibility of ServerStatusRequest created by the controller's client.
		})

		// 测试删除阻塞：当存在 AppBackup 依赖时，应阻止 Cluster 删除
		It("should block deletion if dependencies exist", func() {
			Skip("legacy finalizer deletion protection temporarily disabled")

			// Create Cluster
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create Reconciler
			reconciler := &ClusterReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        record.NewFakeRecorder(100),
				CommandExecutor: &MockCommandExecutor{},
			}

			// Reconcile to add finalizer
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Create Dependency (AppBackup)
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-appbackup",
					Namespace: testNamespace,
					Labels: map[string]string{
						LabelAppBackupCluster: resourceName,
					},
				},
				Spec: disasterv1.AppBackupSpec{
					Cluster: resourceName,
					// Add other required fields if any
				},
			}
			Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

			// Trigger Deletion
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Reconcile
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(10 * time.Second)) // Should requeue

			// Verify Status
			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Status.Reason).To(Equal("DeletionBlocked"))

			// Cleanup Dependency
			Expect(k8sClient.Delete(ctx, appBackup)).To(Succeed())
		})

		// 测试正常删除：当无依赖且带有卸载注解时，应成功删除 Cluster
		It("should successfully delete when no dependencies exist", func() {
			// Create Cluster
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationUninstallVelero: "true",
					},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create Reconciler
			mockExecutor := &MockCommandExecutor{}
			reconciler := &ClusterReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        record.NewFakeRecorder(100),
				CommandExecutor: mockExecutor,
			}
			cleanupClient := buildVeleroCleanupTargetClient(k8sClient.Scheme(), VeleroNamespace)
			reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
				return cleanupClient, nil
			}

			// Reconcile: first pass syncs dependency labels, second pass adds finalizer.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Trigger Deletion
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Reconcile deletion. First pass marks status as Deleting; second pass performs cleanup.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify helm uninstall was called
			foundUninstall := false
			for _, args := range mockExecutor.CalledWith {
				if args[0] == "helm" && args[1] == "uninstall" && args[2] == "velero" {
					foundUninstall = true
					break
				}
			}
			Expect(foundUninstall).To(BeTrue())

			// Verify Deletion
			err = k8sClient.Get(ctx, typeNamespacedName, &disasterv1.Cluster{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		// 测试状态更新：当 ServerStatusRequest 就绪时，应更新 Velero 版本并收集统计信息
		It("should successfully update Velero version and collect stats when ServerStatusRequest is ready", func() {
			// Generate kubeconfig pointing to the test env
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			// Note: BeforeEach already ensures the velero namespace exists and is clean.

			veleroDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "velero",
					Namespace: VeleroNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "velero"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "velero"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "velero",
									Image: "velero/velero:v1.17.0",
								},
							},
						},
					},
				},
			}
			if err := k8sClient.Create(ctx, veleroDeployment); err != nil && !apierrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
			nodeAgent := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-agent",
					Namespace: VeleroNamespace,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "node-agent"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "node-agent"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "node-agent",
								Image: "velero/velero:v1.17.0",
							}},
						},
					},
				},
			}
			if err := k8sClient.Create(ctx, nodeAgent); err != nil && !apierrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			// Wait for deployment to be created
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "velero", Namespace: VeleroNamespace}, &appsv1.Deployment{})
			}, time.Second*10, time.Millisecond*100).Should(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "node-agent", Namespace: VeleroNamespace}, &appsv1.DaemonSet{})
			}, time.Second*10, time.Millisecond*100).Should(Succeed())

			veleroDeployment.Status.Replicas = 1
			veleroDeployment.Status.UpdatedReplicas = 1
			veleroDeployment.Status.ReadyReplicas = 1
			veleroDeployment.Status.AvailableReplicas = 1
			veleroDeployment.Status.UnavailableReplicas = 0
			Expect(k8sClient.Status().Update(ctx, veleroDeployment)).To(Succeed())

			nodeAgent.Status.DesiredNumberScheduled = 1
			nodeAgent.Status.NumberReady = 1
			Expect(k8sClient.Status().Update(ctx, nodeAgent)).To(Succeed())

			// 2. Create some resources to be counted
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: testNamespace,
				},
				Data: map[string]string{"key": "value"},
			}
			if err := k8sClient.Create(ctx, cm); err != nil && !apierrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			// 3. Create Cluster
			cluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationTraceID: "test-trace-id-version",
					},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			// Ensure clean state
			_ = k8sClient.Delete(ctx, cluster)
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, &disasterv1.Cluster{}))
			}, time.Second*10, time.Millisecond*100).Should(BeTrue())

			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create Reconciler
			mockExecutor := &MockCommandExecutor{}
			reconciler := &ClusterReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Recorder:                record.NewFakeRecorder(100),
				CommandExecutor:         mockExecutor,
				ForceVeleroNotInstalled: false, // We want it to find the deployment
			}

			// 4. Reconcile (First pass - sync dependency labels)
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// 5. Reconcile (Second pass - sets Pending)
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// 6. Reconcile (Third pass - checks installation, creates ServerStatusRequest)
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// 7. Verify ServerStatusRequest exists
			ssrName := fmt.Sprintf("disaster-cluster-operator-%s", resourceName)
			ssr := &velerov1.ServerStatusRequest{}

			// Create a client using the same config as the controller
			clientConfig, err := tools.GetRestConfig(kubeConfigBytes)
			Expect(err).NotTo(HaveOccurred())
			debugClient, err := client.New(clientConfig, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				return debugClient.Get(ctx, types.NamespacedName{
					Name:      ssrName,
					Namespace: VeleroNamespace,
				}, ssr)
			}, time.Second*10, time.Millisecond*500).Should(Succeed())

			// 8. Update ServerStatusRequest status
			ssr.Status.ServerVersion = "v1.17.0"
			// Use debugClient to update, and use Update() instead of Status().Update()
			// because the CRD might not have status subresource enabled in this test env.
			Expect(debugClient.Update(ctx, ssr)).To(Succeed())

			// 9. Reconcile again (Fourth pass - reads version, collects stats)
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// 10. Verify Cluster status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.VeleroVersion).To(Equal("v1.17.0"))
			Expect(cluster.Status.Status).To(Equal(disasterv1.ClusterStatusReady))

			// Verify Stats
			Expect(cluster.Status.ResourceTotalCount).To(BeNumerically(">", 0))
			Expect(cluster.Status.NamespaceCount).To(BeNumerically(">", 0))
		})
		It("should recover a current-generation cluster from stale velero status sync waiting", func() {
			VeleroNamespace = "velero"

			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       resourceName,
					Namespace:  testNamespace,
					Finalizers: []string{LabelClusterFinalizer},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			cluster.Status.Status = disasterv1.ClusterStatusNotReady
			cluster.Status.Reason = clusterReasonVeleroStatusSyncPending
			cluster.Status.Message = fmt.Sprintf(
				"waiting for Velero server status request to be processed for %s",
				veleroWaitGenerationMarker(cluster.Generation),
			)
			cluster.Status.VeleroVersion = "v1.17.0"
			cluster.Status.ObservedGeneration = cluster.Generation
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			reconciler := &ClusterReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        record.NewFakeRecorder(100),
				CommandExecutor: &MockCommandExecutor{},
			}
			reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
				return &MockClient{
					Client: k8sClient,
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if (key.Name == "velero" && key.Namespace == "velero") || (key.Name == "node-agent" && key.Namespace == "velero") {
							return nil
						}
						return k8sClient.Get(ctx, key, obj, opts...)
					},
					MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
						if _, ok := list.(*velerov1.BackupList); ok {
							return nil
						}
						return k8sClient.List(ctx, list, opts...)
					},
				}, nil
			}
			defer func() { reconciler.ClientFactory = nil }()

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(1 * time.Minute))

			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Status.Status).To(Equal(disasterv1.ClusterStatusReady))
			Expect(updatedCluster.Status.Reason).To(BeEmpty())
			Expect(updatedCluster.Status.Message).To(BeEmpty())
			Expect(updatedCluster.Status.VeleroVersion).To(Equal("v1.17.0"))
			Expect(updatedCluster.Status.ObservedGeneration).To(Equal(updatedCluster.Generation))

			ssrName := fmt.Sprintf("disaster-cluster-operator-%s", resourceName)
			ssr := &velerov1.ServerStatusRequest{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ssrName, Namespace: VeleroNamespace}, ssr)).To(Succeed())
		})

		It("should mark a previously ready cluster NotReady when velero runtime becomes unhealthy", func() {
			VeleroNamespace = "velero"

			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       resourceName,
					Namespace:  testNamespace,
					Finalizers: []string{LabelClusterFinalizer},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			cluster.Status.Status = disasterv1.ClusterStatusReady
			cluster.Status.VeleroVersion = "v1.17.0"
			cluster.Status.ObservedGeneration = cluster.Generation
			cluster.Status.LastEventPhase = string(disasterv1.ClusterStatusReady)
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			ssrName := fmt.Sprintf("disaster-cluster-operator-%s", resourceName)
			remoteObjects := append(
				buildCompatibleVeleroCRDObjects(),
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "velero", Namespace: VeleroNamespace},
					Status: appsv1.DeploymentStatus{
						ReadyReplicas:       0,
						AvailableReplicas:   0,
						UnavailableReplicas: 1,
					},
				},
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: VeleroNamespace},
					Status: appsv1.DaemonSetStatus{
						DesiredNumberScheduled: 1,
						NumberReady:            0,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "velero-0", Namespace: VeleroNamespace},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						InitContainerStatuses: []corev1.ContainerStatus{{
							State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
						}},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "node-agent-0", Namespace: VeleroNamespace},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						ContainerStatuses: []corev1.ContainerStatus{{
							State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
						}},
					},
				},
				&velerov1.ServerStatusRequest{
					ObjectMeta: metav1.ObjectMeta{Name: ssrName, Namespace: VeleroNamespace},
					Status: velerov1.ServerStatusRequestStatus{
						ServerVersion: "v1.17.0",
					},
				},
			)
			remoteClient := fakeclient.NewClientBuilder().
				WithScheme(k8sClient.Scheme()).
				WithObjects(remoteObjects...).
				Build()

			reconciler := &ClusterReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        record.NewFakeRecorder(100),
				CommandExecutor: &MockCommandExecutor{},
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return remoteClient, nil
				},
			}

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(3 * time.Second))

			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Status.Status).To(Equal(disasterv1.ClusterStatusNotReady))
			Expect(updatedCluster.Status.Reason).To(Equal(clusterReasonVeleroRuntimeNotReady))
			Expect(updatedCluster.Status.Message).To(ContainSubstring("deployment velero ready=0 available=0 unavailable=1"))
			Expect(updatedCluster.Status.Message).To(ContainSubstring("daemonset node-agent ready=0 desired=1"))
			Expect(updatedCluster.Status.Message).To(ContainSubstring("pod velero-0 ImagePullBackOff"))
			Expect(updatedCluster.Status.Message).To(ContainSubstring("pod node-agent-0 ImagePullBackOff"))
			Expect(updatedCluster.Status.VeleroVersion).To(Equal("v1.17.0"))
		})
	})

	Context("When handling error scenarios", func() {
		var (
			resourceName       string
			testNamespace      string
			ctx                context.Context
			cluster            *disasterv1.Cluster
			typeNamespacedName types.NamespacedName
			reconciler         *ClusterReconciler
		)

		BeforeEach(func() {
			ctx = context.Background()
			resourceName = fmt.Sprintf("error-cluster-%d", time.Now().UnixNano())
			testNamespace = fmt.Sprintf("error-ns-%d", time.Now().UnixNano())

			typeNamespacedName = types.NamespacedName{
				Name:      resourceName,
				Namespace: testNamespace,
			}

			// Create Test Namespace
			testNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			Expect(k8sClient.Create(ctx, testNs)).To(Succeed())

			reconciler = &ClusterReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        record.NewFakeRecorder(100),
				CommandExecutor: &MockCommandExecutor{},
			}
		})

		// 测试无效配置：当 KubeConfig 无效时，应将状态置为 NotReady
		It("should handle invalid kubeconfig", func() {
			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: []byte("invalid-kubeconfig"),
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Manually set status to Pending to bypass the initialization check
			cluster.Status.Status = disasterv1.ClustreStatusPending
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Reconcile
			// First pass syncs dependency labels and requeues.
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			// Second pass reaches kubeconfig parsing and should fail.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())

			// Verify Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Status).To(Equal(disasterv1.ClusterStatusNotReady))
		})

		// 测试缺失配置：当 KubeConfig 和 Token 均缺失时，应将状态置为 NotReady
		It("should handle missing kubeconfig and token", func() {
			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					// Empty Spec
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Manually set status to Pending to bypass the initialization check
			cluster.Status.Status = disasterv1.ClustreStatusPending
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Reconcile
			// First pass syncs dependency labels and requeues.
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			// Second pass reaches spec validation and should set NotReady (but return nil).
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred()) // Should not return error (nil)

			// Verify Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Status).To(Equal(disasterv1.ClusterStatusNotReady))
		})

		// 测试安装失败：当 Helm 安装命令失败时，应报告错误并更新状态
		It("should handle velero installation failure", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Manually set status to Pending to bypass the initialization check
			cluster.Status.Status = disasterv1.ClustreStatusPending
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Mock Executor to fail
			mockExecutor := &MockCommandExecutor{
				ReturnError: fmt.Errorf("installation failed"),
			}
			reconciler.CommandExecutor = mockExecutor
			reconciler.ForceVeleroNotInstalled = false // Ensure it tries to install

			// Ensure Velero is NOT installed in the test env
			veleroDep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "velero",
					Namespace: "velero",
				},
			}
			_ = k8sClient.Delete(ctx, veleroDep)

			// Reconcile
			// First pass syncs dependency labels and requeues.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			// Second pass reaches installation flow and should fail.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("installation failed"))

			// Verify Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Status).To(Equal(disasterv1.ClusterStatusNotReady))
		})

		// 测试删除依赖检查：当存在 AppBackup 依赖时，应阻塞删除
		It("should handle deletion with dependencies", func() {
			Skip("legacy finalizer deletion protection temporarily disabled")

			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create AppBackup dependency
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dep-backup",
					Namespace: testNamespace,
					Labels: map[string]string{
						LabelAppBackupCluster: resourceName,
					},
				},
				Spec: disasterv1.AppBackupSpec{
					Cluster: resourceName,
				},
			}
			Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

			// Mark Cluster for deletion
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			controllerutil.AddFinalizer(cluster, LabelClusterFinalizer)
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(10 * time.Second))

			// Verify Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Reason).To(Equal("DeletionBlocked"))
		})

		// 测试删除依赖检查：当存在 AppRestore 依赖时，应阻塞删除
		It("should handle deletion with AppRestore dependency", func() {
			Skip("legacy finalizer deletion protection temporarily disabled")

			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create AppRestore dependency
			appRestore := &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dep-restore",
					Namespace: testNamespace,
					Labels: map[string]string{
						LabelAppRestoreCluster: resourceName,
					},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster:      resourceName,
					BackupSource: "some-backup",
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			// Mark Cluster for deletion
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			controllerutil.AddFinalizer(cluster, LabelClusterFinalizer)
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(10 * time.Second))

			// Verify Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Reason).To(Equal("DeletionBlocked"))
			Expect(cluster.Status.Message).To(ContainSubstring("AppRestores"))
		})

		// 测试删除依赖检查：当存在 DisasterConfig 依赖时，应阻塞删除
		It("should handle deletion with DisasterConfig dependency", func() {
			Skip("legacy finalizer deletion protection temporarily disabled")

			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Create DisasterConfig dependency
			disasterConfig := &disasterv1.DisasterConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dep-config",
					Namespace: testNamespace,
				},
				Spec: disasterv1.DisasterConfigSpec{
					SourceCluster: resourceName,
					TargetCluster: "other-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, disasterConfig)).To(Succeed())

			// Mark Cluster for deletion
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			controllerutil.AddFinalizer(cluster, LabelClusterFinalizer)
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(10 * time.Second))

			// Verify Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Reason).To(Equal("DeletionBlocked"))
			Expect(cluster.Status.Message).To(ContainSubstring("DisasterConfig"))
		})

		// 测试卸载流程：当存在卸载注解时，应触发 Velero 卸载逻辑
		It("should uninstall velero when annotation is present", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationUninstallVelero: "true",
					},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mark Cluster for deletion
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			controllerutil.AddFinalizer(cluster, LabelClusterFinalizer)
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Mock CommandExecutor to verify uninstall
			mockExecutor := &MockCommandExecutor{}
			reconciler.CommandExecutor = mockExecutor
			defer func() { reconciler.CommandExecutor = nil }()
			cleanupClient := buildVeleroCleanupTargetClient(k8sClient.Scheme(), VeleroNamespace)
			reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
				return cleanupClient, nil
			}
			defer func() { reconciler.ClientFactory = nil }()

			// Reconcile
			// First pass persists Deleting status, second pass performs uninstall.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify helm uninstall was called
			Expect(len(mockExecutor.CalledWith)).To(BeNumerically(">", 0))
			foundUninstall := false
			for _, args := range mockExecutor.CalledWith {
				if args[0] == "helm" && args[1] == "uninstall" && args[2] == "velero" {
					foundUninstall = true
					break
				}
			}
			Expect(foundUninstall).To(BeTrue())

			// Verify Cluster is deleted (since finalizer should be removed)
			err = k8sClient.Get(ctx, typeNamespacedName, cluster)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		// 测试版本检查失败：当连接错误导致无法检查 Velero 版本时，应报告错误
		It("should handle checkVeleroVersion failure due to connection error", func() {
			// Create a kubeconfig pointing to an invalid host
			invalidConfig := &rest.Config{
				Host: "https://127.0.0.1:12345", // Closed port
				TLSClientConfig: rest.TLSClientConfig{
					Insecure: true,
				},
			}
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(invalidConfig)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Manually set status to bypass installation check
			cluster.Status.Status = disasterv1.ClustreStatusPending
			cluster.Status.VeleroVersion = "v1.0.0" // Skip install check
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Reconcile
			// First pass: sync dependency labels (metadata update) and Requeue
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Second pass: reach ServerVersion check or checkVeleroVersion and should fail
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())

			// Verify Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Status).To(Equal(disasterv1.ClusterStatusNotReady))
			// VeleroVersion might be "-" or whatever we set, depending on where it failed.
			// If it failed at ServerVersion, VeleroVersion check is skipped.
		})

		// 测试标签更新失败：当更新 Cluster 标签失败时，应报告错误
		It("should handle label update failure", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Manually set status to Ready to reach label update
			cluster.Status.Status = disasterv1.ClusterStatusReady
			cluster.Status.VeleroVersion = "v1.0.0"
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Mock Client to fail Update
			mockClient := &MockClient{
				Client: k8sClient,
				MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*disasterv1.Cluster); ok {
						return fmt.Errorf("update failed")
					}
					return k8sClient.Update(ctx, obj, opts...)
				},
			}
			reconciler.Client = mockClient

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("update failed"))
		})

		// 测试依赖检查失败：当列出依赖资源失败时，应阻塞删除并报告错误
		It("should handle dependency check failure", func() {
			Skip("legacy finalizer deletion protection temporarily disabled")

			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mark for deletion
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			controllerutil.AddFinalizer(cluster, LabelClusterFinalizer)
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Mock Client to fail List
			mockClient := &MockClient{
				Client: k8sClient,
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					return fmt.Errorf("list failed")
				},
			}
			reconciler.Client = mockClient

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(10 * time.Second))

			// Verify Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Reason).To(Equal("DeletionBlocked"))
			Expect(cluster.Status.Message).To(Equal("list failed"))
		})

		// 测试管理器设置：应成功将控制器注册到管理器
		It("should setup with manager", func() {
			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
				Metrics: server.Options{
					BindAddress: "0",
				},
				HealthProbeBindAddress: "0",
			})
			Expect(err).NotTo(HaveOccurred())

			err = reconciler.SetupWithManager(mgr)
			Expect(err).NotTo(HaveOccurred())
		})

		// 测试 Token 安装：当提供 Token 和 Endpoint 时，应使用 Helm 安装 Velero
		It("should install velero using token", func() {
			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					Endpoint: "https://127.0.0.1:6443",
					Token:    "some-token",
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mock Executor
			mockExecutor := &MockCommandExecutor{}
			reconciler.CommandExecutor = mockExecutor
			reconciler.ForceVeleroNotInstalled = false

			// Mock ClientFactory
			reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
				return &MockClient{
					Client: k8sClient,
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if key.Name == "velero" && key.Namespace == "velero" {
							return apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "velero")
						}
						// Delegate to the real envtest client for other reads, so objects (e.g. CRDs) are populated.
						return k8sClient.Get(ctx, key, obj, opts...)
					},
					MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
						// Delegate to the real envtest client so list results are realistic.
						return k8sClient.List(ctx, list, opts...)
					},
				}, nil
			}

			// Mock KubeClientFactory
			reconciler.KubeClientFactory = func(c *rest.Config) (kubernetes.Interface, error) {
				return fake.NewSimpleClientset(), nil
			}

			defer func() {
				reconciler.ClientFactory = nil
				reconciler.KubeClientFactory = nil
			}()

			// Reconcile
			// First pass: sync dependency labels (metadata update) and Requeue
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Second pass: set Pending (init)
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Third pass: installs Velero
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify Install Command was called
			Expect(len(mockExecutor.CalledWith)).To(BeNumerically(">", 0))
			Expect(mockExecutor.CalledWith[0][0]).To(Equal("helm"))
		})

		// 测试节点列表失败：当获取节点列表失败时，应报告错误
		It("should handle node list failure", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mock CommandExecutor to avoid actual helm install
			reconciler.CommandExecutor = &MockCommandExecutor{}

			// Mock KubeClientFactory to return a client that fails on Node List
			reconciler.KubeClientFactory = func(c *rest.Config) (kubernetes.Interface, error) {
				fakeClient := fake.NewSimpleClientset()
				fakeClient.PrependReactor("list", "nodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("failed to list nodes")
				})
				return fakeClient, nil
			}
			defer func() { reconciler.KubeClientFactory = nil }()

			// Reconcile
			// First pass syncs dependency labels and requeues.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Second pass sets Pending (init)
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Third pass tries to list nodes
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to list nodes"))
		})

		// 测试统计收集失败：当收集集群统计信息失败时，不应中断 Reconcile 流程，但不会更新统计标签
		It("should handle collect stats failure", func() {
			// Avoid flakiness from other tests mutating this global.
			VeleroNamespace = "velero"

			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mock ClientFactory: make collectClusterStats fail, while keeping other reads realistic.
			reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
				return &MockClient{
					Client: k8sClient,
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						// Let IsVeleroInstalled see Velero as installed (no install path).
						if (key.Name == "velero" && key.Namespace == "velero") || (key.Name == "node-agent" && key.Namespace == "velero") {
							return nil
						}
						// Delegate other reads (e.g. ServerStatusRequest) to the real envtest client.
						return k8sClient.Get(ctx, key, obj, opts...)
					},
					MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
						if _, ok := list.(*velerov1.BackupList); ok {
							return nil
						}
						// Force stats collection to fail.
						return fmt.Errorf("failed to list resources")
					},
				}, nil
			}
			defer func() { reconciler.ClientFactory = nil }()

			// Reconcile
			// 1) sync dependency labels
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// 2) init: set Pending
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// 3) create ServerStatusRequest (version not ready yet)
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// 4) set ServerStatusRequest status so the controller can proceed to stats collection
			ssrName := fmt.Sprintf("disaster-cluster-operator-%s", resourceName)
			ssr := &velerov1.ServerStatusRequest{}
			clientConfig, err := tools.GetRestConfig(kubeConfigBytes)
			Expect(err).NotTo(HaveOccurred())
			debugClient, err := client.New(clientConfig, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())
			Expect(debugClient.Get(ctx, types.NamespacedName{Name: ssrName, Namespace: VeleroNamespace}, ssr)).To(Succeed())
			ssr.Status.ServerVersion = "v1.17.0"
			Expect(debugClient.Update(ctx, ssr)).To(Succeed())

			// 5) proceed to stats collection, which should fail but must not fail reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify Labels were NOT updated (because collectClusterStats failed)
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Labels).NotTo(HaveKey(LabelClusterNamespaceCount))
			Expect(cluster.Labels).NotTo(HaveKey(LabelClusterResourceTotalCount))
			Expect(cluster.Labels).NotTo(HaveKey(LabelClusterName))
		})

		// 测试 Finalizer 移除失败：当移除 Finalizer 失败时，应报告错误
		It("should handle update failure when removing finalizer", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationUninstallVelero: "true",
					},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mark Cluster for deletion
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			controllerutil.AddFinalizer(cluster, LabelClusterFinalizer)
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Mock Client Update failure
			originalClient := reconciler.Client
			reconciler.Client = &MockClient{
				Client: k8sClient,
				MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					// Only fail if it's the cluster update (removing finalizer)
					if cl, ok := obj.(*disasterv1.Cluster); ok && cl.Name == resourceName {
						// Check if finalizer is removed
						if !controllerutil.ContainsFinalizer(cl, LabelClusterFinalizer) {
							return fmt.Errorf("failed to update cluster")
						}
					}
					return k8sClient.Update(ctx, obj, opts...)
				},
			}
			defer func() { reconciler.Client = originalClient }()
			cleanupClient := buildVeleroCleanupTargetClient(k8sClient.Scheme(), VeleroNamespace)
			reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
				return cleanupClient, nil
			}
			defer func() { reconciler.ClientFactory = nil }()

			// Reconcile deletion. First pass marks status as Deleting; second pass removes the finalizer.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to update cluster"))
		})

		// 测试 Finalizer 添加失败：当添加 Finalizer 失败时，应报告错误
		It("should handle update failure when adding finalizer", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mock Client Update failure
			originalClient := reconciler.Client
			reconciler.Client = &MockClient{
				Client: k8sClient,
				MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					// Only fail if it's the cluster update (adding finalizer)
					if cl, ok := obj.(*disasterv1.Cluster); ok && cl.Name == resourceName {
						if controllerutil.ContainsFinalizer(cl, LabelClusterFinalizer) {
							return fmt.Errorf("failed to add finalizer")
						}
					}
					return k8sClient.Update(ctx, obj, opts...)
				},
			}
			defer func() { reconciler.Client = originalClient }()

			// Reconcile
			// First pass: sync dependency labels and requeue
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Second pass: adding finalizer should fail
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to add finalizer"))
		})

		// 测试状态更新失败：当更新状态失败时，应记录错误但不中断流程
		It("should handle status update failure", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mock Client Status Update failure
			originalClient := reconciler.Client
			reconciler.Client = &MockClient{
				Client: k8sClient,
				MockStatus: func() client.StatusWriter {
					return &MockStatusWriter{
						StatusWriter: k8sClient.Status(),
						MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							return fmt.Errorf("failed to update status")
						},
					}
				},
			}
			defer func() { reconciler.Client = originalClient }()

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			// Reconcile should not return error if status update fails (it just logs it)
			Expect(err).NotTo(HaveOccurred())
		})

		// 测试卸载失败：当卸载 Velero 失败时，应更新状态为 VeleroUninstallFailed
		It("should handle uninstall velero failure", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationUninstallVelero: "true",
					},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Mark Cluster for deletion
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			controllerutil.AddFinalizer(cluster, LabelClusterFinalizer)
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Mock CommandExecutor to fail on uninstall
			reconciler.CommandExecutor = &MockCommandExecutor{
				ReturnError: fmt.Errorf("helm uninstall failed"),
			}
			defer func() { reconciler.CommandExecutor = nil }()

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(10 * time.Second))

			// Verify status updated
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Reason).To(Equal("VeleroUninstallFailed"))
			Expect(cluster.Status.Message).To(ContainSubstring("helm uninstall failed"))
		})

		// 测试安装失败：当 Helm 安装失败时，应报告错误
		It("should handle velero installation failure", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Update Status to trigger install
			cluster.Status.VeleroVersion = "-"
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Mock ClientFactory to say Velero is NOT installed
			reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
				return &MockClient{
					Client: k8sClient,
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if key.Name == "velero" && key.Namespace == "velero" {
							return apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "velero")
						}
						// Delegate other reads (e.g. CRDs) to the real envtest client.
						return k8sClient.Get(ctx, key, obj, opts...)
					},
				}, nil
			}
			defer func() { reconciler.ClientFactory = nil }()

			// Mock CommandExecutor to fail
			reconciler.CommandExecutor = &MockCommandExecutor{
				ReturnError: fmt.Errorf("helm install failed"),
			}
			reconciler.ForceVeleroNotInstalled = false

			// Reconcile
			// First pass syncs dependency labels
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Ensure VeleroVersion is "-" to trigger install
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			if cluster.Status.VeleroVersion != "-" {
				cluster.Status.VeleroVersion = "-"
				Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())
			}

			// Second pass sets Pending (initialization)
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Third pass reaches install flow and should fail
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("helm install failed"))
		})

		// 测试安装检查失败：当检查 Velero 是否安装失败时，应报告错误
		It("should handle IsVeleroInstalled failure", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Update Status
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			cluster.Status.VeleroVersion = "-"
			cluster.Status.Status = disasterv1.ClusterStatusReady // Set status to avoid early return
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Wait for cache to update
			Eventually(func() string {
				var c disasterv1.Cluster
				k8sClient.Get(ctx, typeNamespacedName, &c)
				return c.Status.VeleroVersion
			}, time.Second*5, time.Millisecond*100).Should(Equal("-"))

			// Mock ClientFactory to fail on Get
			reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
				return &MockClient{
					Client: k8sClient,
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if key.Name == "velero" && key.Namespace == "velero" {
							return fmt.Errorf("failed to get velero")
						}
						return k8sClient.Get(ctx, key, obj, opts...)
					},
				}, nil
			}
			defer func() { reconciler.ClientFactory = nil }()

			// Reconcile
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())

			// First pass: sync dependency labels and requeue
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Second pass: should reach IsVeleroInstalled and fail
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get velero"))
		})

		// 测试卸载流程失败：当卸载过程中发生错误时，应正确处理并更新状态
		It("should handle uninstallVelero failure", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationUninstallVelero: "true",
					},
					Finalizers: []string{LabelClusterFinalizer},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Update Status to Ready and VeleroVersion to v1.0.0 so it proceeds to uninstall check
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			cluster.Status.Status = disasterv1.ClusterStatusReady
			cluster.Status.VeleroVersion = "v1.0.0"
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Delete the cluster to trigger handleDelete
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Refresh cluster to get DeletionTimestamp
			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.DeletionTimestamp.IsZero()).To(BeFalse())

			// Mock CommandExecutor to fail on uninstall
			reconciler.CommandExecutor = &MockCommandExecutor{
				ReturnError: fmt.Errorf("failed to uninstall velero"),
			}
			defer func() { reconciler.CommandExecutor = nil }()

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			// Reconcile returns nil but requeues on uninstall failure
			Expect(err).NotTo(HaveOccurred())

			// Verify Status
			Eventually(func() string {
				var c disasterv1.Cluster
				k8sClient.Get(ctx, typeNamespacedName, &c)
				return c.Status.Reason
			}, time.Second*5, time.Millisecond*100).Should(Equal("VeleroUninstallFailed"))

			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Message).To(ContainSubstring("failed to uninstall velero"))
		})

		// 测试无效配置卸载失败：当配置无效导致无法卸载时，应更新状态为 VeleroUninstallFailed
		It("should handle uninstallVelero failure due to invalid config", func() {
			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationUninstallVelero: "true",
					},
					Finalizers: []string{LabelClusterFinalizer},
				},
				Spec: disasterv1.ClusterSpec{
					// Empty spec
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())

			// Reconcile
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify Status
			Eventually(func() string {
				var c disasterv1.Cluster
				k8sClient.Get(ctx, typeNamespacedName, &c)
				return c.Status.Reason
			}, time.Second*5, time.Millisecond*100).Should(Equal("VeleroUninstallFailed"))

			Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
			Expect(cluster.Status.Message).To(ContainSubstring("neither kubeconfig nor token/endpoint provided"))
		})

		Context("When installing Velero", func() {
			// 测试安装失败：当 Helm 安装失败时，应更新状态为 NotReady
			It("should handle installation failure", func() {
				// Setup cluster with no Velero installed
				kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
				Expect(err).NotTo(HaveOccurred())

				cluster = &disasterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
					Spec: disasterv1.ClusterSpec{
						KubeConfig: kubeConfigBytes,
					},
				}
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Mock ClientFactory to return client that says Velero is NOT installed
				reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
					return &MockClient{
						Client: k8sClient,
						MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if key.Name == "velero" && key.Namespace == "velero" {
								return apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "velero")
							}
							return k8sClient.Get(ctx, key, obj, opts...)
						},
					}, nil
				}
				defer func() { reconciler.ClientFactory = nil }()

				// Mock CommandExecutor to fail
				reconciler.CommandExecutor = &MockCommandExecutor{
					ReturnError: fmt.Errorf("helm install failed"),
				}
				defer func() { reconciler.CommandExecutor = nil }()

				// Reconcile
				// First pass: sync dependency labels
				_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				// Second pass: set Pending (init)
				_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				// Third pass installs Velero
				_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("helm install failed"))

				Expect(k8sClient.Get(ctx, typeNamespacedName, cluster)).To(Succeed())
				Expect(cluster.Status.Status).To(Equal(disasterv1.ClusterStatusNotReady))
			})

			// 测试安装成功：当 Helm 安装成功时，应继续流程
			It("should handle installation success", func() {
				// Setup cluster with no Velero installed
				kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
				Expect(err).NotTo(HaveOccurred())

				cluster = &disasterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
					Spec: disasterv1.ClusterSpec{
						KubeConfig: kubeConfigBytes,
					},
				}
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Mock ClientFactory
				reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
					return &MockClient{
						Client: k8sClient,
						MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if key.Name == "velero" && key.Namespace == "velero" {
								return apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "velero")
							}
							return k8sClient.Get(ctx, key, obj, opts...)
						},
					}, nil
				}
				defer func() { reconciler.ClientFactory = nil }()

				// Mock CommandExecutor to succeed
				mockExecutor := &MockCommandExecutor{ReturnError: nil}
				reconciler.CommandExecutor = mockExecutor
				defer func() { reconciler.CommandExecutor = nil }()

				// Reconcile
				// First pass: sync dependency labels
				_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				// Second pass: set Pending (init)
				_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				// Third pass: installs Velero (helm is executed even if version check is not ready yet)
				_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				Expect(len(mockExecutor.CalledWith)).To(BeNumerically(">=", 1))
				Expect(mockExecutor.CalledWith[0][0]).To(Equal("helm"))
				Expect(mockExecutor.CalledWith[0][1]).To(Equal("upgrade"))
			})
		})

		Context("When checking Velero version", func() {
			// 测试版本检查：当 ServerStatusRequest 不存在时，应创建该资源以获取版本信息
			It("should create ServerStatusRequest if not found", func() {
				// Setup cluster
				kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
				Expect(err).NotTo(HaveOccurred())

				cluster = &disasterv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
					Spec: disasterv1.ClusterSpec{
						KubeConfig: kubeConfigBytes,
					},
				}
				Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

				// Mock ClientFactory
				reconciler.ClientFactory = func(config *rest.Config, options client.Options) (client.Client, error) {
					return &MockClient{
						Client: k8sClient,
						MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							// Mock IsVeleroInstalled to return true
							if key.Name == "velero" && key.Namespace == "velero" {
								return nil
							}
							// Mock ServerStatusRequest to return NotFound
							if _, ok := obj.(*velerov1.ServerStatusRequest); ok {
								return apierrors.NewNotFound(schema.GroupResource{Group: "velero.io", Resource: "serverstatusrequests"}, key.Name)
							}
							return k8sClient.Get(ctx, key, obj, opts...)
						},
						MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
							if _, ok := obj.(*velerov1.ServerStatusRequest); ok {
								return nil // Simulate successful creation
							}
							return k8sClient.Create(ctx, obj, opts...)
						},
					}, nil
				}
				defer func() { reconciler.ClientFactory = nil }()

				// Reconcile
				// First pass syncs dependency labels
				_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				// Second pass sets Pending (init)
				_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				// Third pass checks version (creates ServerStatusRequest and requeues)
				result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(3 * time.Second))

				updatedCluster := &disasterv1.Cluster{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
				Expect(updatedCluster.Status.Reason).NotTo(BeEmpty())
				Expect(updatedCluster.Status.Message).NotTo(BeEmpty())
			})

			It("should describe velero runtime wait details when pods are not ready", func() {
				scheme := runtime.NewScheme()
				Expect(corev1.AddToScheme(scheme)).To(Succeed())
				Expect(appsv1.AddToScheme(scheme)).To(Succeed())

				cli := fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(
					&appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{Name: "velero", Namespace: "velero"},
						Status: appsv1.DeploymentStatus{
							ReadyReplicas:       0,
							AvailableReplicas:   0,
							UnavailableReplicas: 1,
						},
					},
					&appsv1.DaemonSet{
						ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "velero"},
						Status: appsv1.DaemonSetStatus{
							DesiredNumberScheduled: 1,
							NumberReady:            0,
						},
					},
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "velero-0", Namespace: "velero"},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							InitContainerStatuses: []corev1.ContainerStatus{{
								State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
							}},
						},
					},
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "node-agent-0", Namespace: "velero"},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							ContainerStatuses: []corev1.ContainerStatus{{
								State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
							}},
						},
					},
				).Build()

				reason, message := diagnoseVeleroStatusPending(ctx, cli, 3)
				Expect(reason).To(Equal(clusterReasonVeleroRuntimeNotReady))
				Expect(message).To(ContainSubstring("spec generation 3"))
				Expect(message).To(ContainSubstring("deployment velero ready=0 available=0 unavailable=1"))
				Expect(message).To(ContainSubstring("daemonset node-agent ready=0 desired=1"))
				Expect(message).To(ContainSubstring("pod velero-0 ImagePullBackOff"))
				Expect(message).To(ContainSubstring("pod node-agent-0 ImagePullBackOff"))
			})
		})
	})
	Context("Token Expiration Check", func() {
		var (
			resourceName       string
			testNamespace      string
			ctx                context.Context
			cluster            *disasterv1.Cluster
			typeNamespacedName types.NamespacedName
			reconciler         *ClusterReconciler
		)

		BeforeEach(func() {
			ctx = context.Background()
			resourceName = fmt.Sprintf("token-cluster-%d", time.Now().UnixNano())
			testNamespace = fmt.Sprintf("token-ns-%d", time.Now().UnixNano())

			typeNamespacedName = types.NamespacedName{
				Name:      resourceName,
				Namespace: testNamespace,
			}

			testNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			Expect(k8sClient.Create(ctx, testNs)).To(Succeed())

			reconciler = &ClusterReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        record.NewFakeRecorder(100),
				CommandExecutor: &MockCommandExecutor{},
			}
		})

		It("should fail if token is expired", func() {
			// Generate expired token
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"exp": time.Now().Add(-1 * time.Hour).Unix(),
			})
			tokenString, _ := token.SignedString([]byte("secret"))

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					Token:    tokenString,
					Endpoint: "https://127.0.0.1:6443", // Mock endpoint
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Bypass initialization to run token check
			cluster.Status.Status = disasterv1.ClustreStatusPending
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Reconcile
			// First pass syncs dependency labels.
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			// Second pass runs token expiration check.
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})

			// Verify Status
			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Status.Status).To(Equal(disasterv1.ClusterStatusNotReady))
			Expect(updatedCluster.Status.Reason).To(Equal("TokenExpired"))
			Expect(updatedCluster.Status.TokenExpiration).NotTo(BeNil())
		})

		It("should have TokenExpiration set if token is valid", func() {
			// Generate valid token
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"exp": time.Now().Add(1 * time.Hour).Unix(),
			})
			tokenString, _ := token.SignedString([]byte("secret"))

			cluster = &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: disasterv1.ClusterSpec{
					Token:    tokenString,
					Endpoint: "https://127.0.0.1:6443",
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Bypass initialization
			cluster.Status.Status = disasterv1.ClustreStatusPending
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			// Reconcile
			// First pass syncs dependency labels.
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			// Second pass runs token expiration check.
			_, _ = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})

			// Verify Status
			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
			// Status might be NotReady due to connection failure, but TokenExpiration should be set
			Expect(updatedCluster.Status.TokenExpiration).NotTo(BeNil())
			Expect(updatedCluster.Status.TokenExpiration.Time.After(time.Now())).To(BeTrue())
		})

		It("should clear stale TokenExpired reason when token becomes valid", func() {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"exp": time.Now().Add(1 * time.Hour).Unix(),
			})
			tokenString, _ := token.SignedString([]byte("secret"))

			cluster := &disasterv1.Cluster{
				Spec: disasterv1.ClusterSpec{Token: tokenString},
				Status: disasterv1.ClusterStatus{
					Status:          disasterv1.ClusterStatusNotReady,
					Reason:          "TokenExpired",
					Message:         "Token expired at stale-time",
					TokenExpiration: &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
				},
			}

			expired := refreshClusterTokenExpiration(cluster, time.Now())

			Expect(expired).To(BeFalse())
			Expect(cluster.Status.Reason).To(BeEmpty())
			Expect(cluster.Status.Message).To(BeEmpty())
			Expect(cluster.Status.TokenExpiration).NotTo(BeNil())
			Expect(cluster.Status.TokenExpiration.Time.After(time.Now())).To(BeTrue())
		})
	})

	Context("When verifying event emission", func() {
		var (
			resourceName       string
			veleroNamespace    string
			testNamespace      string
			ctx                context.Context
			typeNamespacedName types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()
			resourceName = fmt.Sprintf("event-cluster-%d", time.Now().UnixNano())
			veleroNamespace = fmt.Sprintf("velero-event-%d", time.Now().UnixNano())
			testNamespace = fmt.Sprintf("event-ns-%d", time.Now().UnixNano())

			VeleroNamespace = veleroNamespace

			typeNamespacedName = types.NamespacedName{
				Name:      resourceName,
				Namespace: testNamespace,
			}

			// Create namespaces
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: veleroNamespace}}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			testNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			Expect(k8sClient.Create(ctx, testNs)).To(Succeed())
		})

		// 测试事件发射：验证 ClusterCreated 事件在 Finalizer 添加后发射
		It("should emit ClusterCreated event when finalizer is added", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			cluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationTraceID: "test-event-trace",
					},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			reconciler := &ClusterReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Recorder:                record.NewFakeRecorder(100),
				CommandExecutor:         &MockCommandExecutor{},
				ForceVeleroNotInstalled: true,
			}

			// Reconcile - should add finalizer and emit ClusterCreated event
			// First pass syncs dependency labels.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			// Second pass adds finalizer and emits the Started event.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer was added
			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Finalizers).To(ContainElement(LabelClusterFinalizer))

			// Verify event was created (check for Event resources)
			eventList := &corev1.EventList{}
			Eventually(func() bool {
				// Cluster 相关事件统一落在 disaster-system 命名空间（见 suite_test.go 的说明）。
				if err := k8sClient.List(ctx, eventList, client.InNamespace("disaster-system")); err != nil {
					return false
				}
				for _, event := range eventList.Items {
					if event.Reason == "ExecutionStarted" && event.InvolvedObject.Name == resourceName {
						return true
					}
				}
				return false
			}, time.Second*5, time.Millisecond*100).Should(BeTrue())
		})

		// 测试事件发射：验证 ClusterReady 事件只发射一次
		It("should emit ClusterReady event only once when transitioning to Ready", func() {
			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			// Create Velero deployment to skip installation
			veleroDeployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "velero",
					Namespace: VeleroNamespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "velero"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "velero"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "velero", Image: "velero/velero:v1.17.0"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, veleroDeployment)).To(Succeed())
			nodeAgent := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node-agent",
					Namespace: VeleroNamespace,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "node-agent"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "node-agent"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "node-agent",
								Image: "velero/velero:v1.17.0",
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, nodeAgent)).To(Succeed())
			veleroDeployment.Status.Replicas = 1
			veleroDeployment.Status.UpdatedReplicas = 1
			veleroDeployment.Status.ReadyReplicas = 1
			veleroDeployment.Status.AvailableReplicas = 1
			Expect(k8sClient.Status().Update(ctx, veleroDeployment)).To(Succeed())
			nodeAgent.Status.DesiredNumberScheduled = 1
			nodeAgent.Status.NumberReady = 1
			Expect(k8sClient.Status().Update(ctx, nodeAgent)).To(Succeed())

			cluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
					Annotations: map[string]string{
						AnnotationTraceID: "test-ready-trace",
					},
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

			// Pre-set VeleroVersion to skip installation check
			cluster.Status.VeleroVersion = "v1.17.0"
			cluster.Status.Status = disasterv1.ClustreStatusPending
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			reconciler := &ClusterReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        record.NewFakeRecorder(100),
				CommandExecutor: &MockCommandExecutor{},
			}

			// Create SSR with version to allow reconcile to succeed
			ssrName := fmt.Sprintf("disaster-cluster-operator-%s", resourceName)
			ssr := &velerov1.ServerStatusRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ssrName,
					Namespace: VeleroNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, ssr)).To(Succeed())

			// Update SSR status (Create 通常不会持久化 Status 字段；使用 Update() 写入)
			clientConfig, err := tools.GetRestConfig(kubeConfigBytes)
			Expect(err).NotTo(HaveOccurred())
			debugClient, err := client.New(clientConfig, client.Options{Scheme: k8sClient.Scheme()})
			Expect(err).NotTo(HaveOccurred())
			Expect(debugClient.Get(ctx, types.NamespacedName{Name: ssrName, Namespace: VeleroNamespace}, ssr)).To(Succeed())
			ssr.Status.ServerVersion = "v1.17.0"
			Expect(debugClient.Update(ctx, ssr)).To(Succeed())

			// Reconcile to reach Ready
			// First pass syncs dependency labels.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			// Second pass should observe SSR version and mark Cluster Ready.
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify cluster is Ready
			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Status.Status).To(Equal(disasterv1.ClusterStatusReady))

			// Verify LastEventPhase is set to prevent duplicate events
			Expect(updatedCluster.Status.LastEventPhase).To(Equal(string(disasterv1.ClusterStatusReady)))

			// Verify ReadyTimestamp is set
			Expect(updatedCluster.Status.ReadyTimestamp).NotTo(BeNil())
		})
	})

	Context("When handling ensure-storage signal", func() {
		ensureStorageRepository := func(ctx context.Context, storageName string) {
			_ = k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "disaster-system"}})
			sr := &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      storageName,
					Namespace: "disaster-system",
				},
				Spec: disasterv1.StorageRepositorySpec{
					Endpoint:  "http://s3.example.com",
					Bucket:    "my-bucket",
					Region:    "us-east-1",
					AccessKey: "key",
					SecretKey: "secret",
				},
			}
			if err := k8sClient.Create(ctx, sr); err != nil && !apierrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		}

		createSignalCluster := func(ctx context.Context, namespace, resourceName, storageName, sourceCluster string) types.NamespacedName {
			_ = k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			kubeConfigBytes, err := GetKubeConfigFromRestConfig(cfg)
			Expect(err).NotTo(HaveOccurred())

			annotations := map[string]string{
				AnnotationEnsureStorage: storageName,
				AnnotationTraceID:       "signal-trace",
			}
			if sourceCluster != "" {
				annotations[AnnotationEnsureStorageSourceCluster] = sourceCluster
			}

			cluster := &disasterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:        resourceName,
					Namespace:   namespace,
					Annotations: annotations,
				},
				Spec: disasterv1.ClusterSpec{
					KubeConfig: kubeConfigBytes,
				},
			}
			Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
			return types.NamespacedName{Name: resourceName, Namespace: namespace}
		}

		newReconciler := func(mockBSL *MockBSL) *ClusterReconciler {
			return &ClusterReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Recorder:                record.NewFakeRecorder(100),
				CommandExecutor:         &MockCommandExecutor{},
				BSL:                     mockBSL,
				ForceVeleroNotInstalled: true,
			}
		}

		It("should use source cluster suffix when source-cluster annotation exists", func() {
			ctx := context.Background()
			storageName := "test-storage-dual"
			sourceCluster := "source-a"
			resourceName := fmt.Sprintf("signal-%d", time.Now().UnixNano())
			testNamespace := fmt.Sprintf("signal-ns-%d", time.Now().UnixNano())

			ensureStorageRepository(ctx, storageName)
			namespacedName := createSignalCluster(ctx, testNamespace, resourceName, storageName, sourceCluster)

			mockBSL := &MockBSL{}
			reconciler := newReconciler(mockBSL)

			Eventually(func() bool {
				_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())
				return mockBSL.Called
			}, time.Second*5, time.Millisecond*100).Should(BeTrue())

			Expect(mockBSL.LastBSLName).To(Equal(storageName + "-" + sourceCluster))
			Expect(mockBSL.LastPrefix).To(Equal(sourceCluster))

			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, namespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Annotations).NotTo(HaveKey(AnnotationEnsureStorage))
			Expect(updatedCluster.Annotations).NotTo(HaveKey(AnnotationEnsureStorageSourceCluster))
		})

		It("should fallback to cluster name when source-cluster annotation is missing", func() {
			ctx := context.Background()
			storageName := "test-storage-fallback"
			resourceName := fmt.Sprintf("signal-%d", time.Now().UnixNano())
			testNamespace := fmt.Sprintf("signal-ns-%d", time.Now().UnixNano())

			ensureStorageRepository(ctx, storageName)
			namespacedName := createSignalCluster(ctx, testNamespace, resourceName, storageName, "")

			mockBSL := &MockBSL{}
			reconciler := newReconciler(mockBSL)

			Eventually(func() bool {
				_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())
				return mockBSL.Called
			}, time.Second*5, time.Millisecond*100).Should(BeTrue())

			Expect(mockBSL.LastBSLName).To(Equal(storageName + "-" + resourceName))
			Expect(mockBSL.LastPrefix).To(Equal(resourceName))
		})

		It("should remove both signal annotations when storage repository is missing", func() {
			ctx := context.Background()
			storageName := "missing-storage"
			sourceCluster := "source-missing"
			resourceName := fmt.Sprintf("signal-%d", time.Now().UnixNano())
			testNamespace := fmt.Sprintf("signal-ns-%d", time.Now().UnixNano())

			namespacedName := createSignalCluster(ctx, testNamespace, resourceName, storageName, sourceCluster)
			reconciler := newReconciler(&MockBSL{})

			Eventually(func() bool {
				_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())
				updatedCluster := &disasterv1.Cluster{}
				if getErr := k8sClient.Get(ctx, namespacedName, updatedCluster); getErr != nil {
					return false
				}
				if updatedCluster.Annotations == nil {
					return true
				}
				_, hasStorage := updatedCluster.Annotations[AnnotationEnsureStorage]
				_, hasSource := updatedCluster.Annotations[AnnotationEnsureStorageSourceCluster]
				return !hasStorage && !hasSource
			}, time.Second*5, time.Millisecond*100).Should(BeTrue())
		})

		It("should keep signal annotations when apply storage repository returns error", func() {
			ctx := context.Background()
			storageName := "test-storage-error"
			sourceCluster := "source-error"
			resourceName := fmt.Sprintf("signal-%d", time.Now().UnixNano())
			testNamespace := fmt.Sprintf("signal-ns-%d", time.Now().UnixNano())

			ensureStorageRepository(ctx, storageName)
			namespacedName := createSignalCluster(ctx, testNamespace, resourceName, storageName, sourceCluster)

			mockBSL := &MockBSL{Err: fmt.Errorf("apply bsl failed")}
			reconciler := newReconciler(mockBSL)

			Eventually(func() bool {
				_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: namespacedName})
				return err != nil
			}, time.Second*5, time.Millisecond*100).Should(BeTrue())

			updatedCluster := &disasterv1.Cluster{}
			Expect(k8sClient.Get(ctx, namespacedName, updatedCluster)).To(Succeed())
			Expect(updatedCluster.Annotations).To(HaveKey(AnnotationEnsureStorage))
			Expect(updatedCluster.Annotations).To(HaveKey(AnnotationEnsureStorageSourceCluster))
		})
	})
})

// MockBSL implements BSL interface for testing
type MockBSL struct {
	Called      bool
	LastBSLName string
	LastPrefix  string
	Err         error
}

func (m *MockBSL) ApplyStorageRepository(ctx context.Context, _ client.Reader, cli client.Client, sr *disasterv1.StorageRepository, bslName, prefix string) error {
	m.Called = true
	m.LastBSLName = bslName
	m.LastPrefix = prefix
	return m.Err
}
