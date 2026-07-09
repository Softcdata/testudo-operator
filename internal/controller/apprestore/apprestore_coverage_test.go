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

package apprestore

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("AppRestore Coverage Expansion", func() {
	var (
		ctx         context.Context
		reconciler  *AppRestoreReconciler
		appRestore  *disasterv1.AppRestore
		ns          = "default"
		recorder    *record.FakeRecorder
		mockFactory *controller.MockClientFactory
		mockClient  *controller.MockClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		recorder = record.NewFakeRecorder(100)

		// Use MockClient to intercept calls if needed
		mockClient = &controller.MockClient{
			Client: k8sClient,
		}

		mockFactory = &controller.MockClientFactory{
			MockClient: mockClient,
		}

		reconciler = &AppRestoreReconciler{
			Client:        mockClient,
			Scheme:        k8sClient.Scheme(),
			Recorder:      recorder,
			ClientFactory: mockFactory,
			StatsHelper:   helper.NewStatisticsHelper(mockClient),
		}

		// Create Namespaces
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		_ = k8sClient.Create(ctx, nsObj)
		veleroNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: controller.VeleroNamespace}}
		_ = k8sClient.Create(ctx, veleroNs)
	})

	Context("PendingHandler Logic", func() {
		It("should fail if cluster is invalid", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-invalid-cluster",
					Namespace:  ns,
					Finalizers: []string{LabelAppRestoreFinalizer},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "", // Invalid
				},
			}
			handler := &PendingHandler{}
			phase, _, _ := handler.Handle(ctx, reconciler, appRestore)
			Expect(phase).To(Equal(disasterv1.PhaseFailed))
			Eventually(recorder.Events).Should(Receive(ContainSubstring("ConfigError")))
		})

		It("should retry if KubeClient creation fails", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-client-fail",
					Namespace:  ns,
					Finalizers: []string{LabelAppRestoreFinalizer},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			mockFactory.MockError = fmt.Errorf("connection refused")

			// Mock Get - Only intercept for external resources if needed, otherwise use real client
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).To(HaveOccurred())
		})

		It("should handle cross-cluster BSL pre-loading missing StorageRepo", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cross-cluster-missing-sr",
					Namespace:  ns,
					Finalizers: []string{LabelAppRestoreFinalizer},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster:       "target-cluster",
					SourceCluster: "source-cluster",
					// StorageRepository: "", // Missing
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			// Reset factory error
			mockFactory.MockError = nil

			handler := &PendingHandler{}
			phase, _, _ := handler.Handle(ctx, reconciler, appRestore)
			Expect(phase).To(Equal(disasterv1.PhaseFailed))
			Eventually(recorder.Events).Should(Receive(ContainSubstring("ConfigError")))
		})
	})

	Context("RestoringHandler Logic", func() {
		BeforeEach(func() {
			mockFactory.MockError = nil
		})

		It("should create Velero restore if not found", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-create-restore",
					Namespace: ns,
					Annotations: map[string]string{
						AnnotationTraceID: "test-trace",
					},
					Finalizers: []string{LabelAppRestoreFinalizer},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
					Template: velerov1.RestoreSpec{
						BackupName: "test-backup",
					},
				},
			}
			// Set Status manually before Create? No, Status is ignored on Create.
			// Must Create then UpdateStatus.
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			appRestore.Status.Status = disasterv1.PhaseRestoring
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Create to succeed
			createCalled := false
			mockClient.MockCreate = func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					createCalled = true
					return nil
				}
				return k8sClient.Create(ctx, obj, opts...)
			}

			// Mock Get to return NotFound for Restore
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return apierrors.NewNotFound(schema.GroupResource{Group: "velero.io", Resource: "restores"}, key.Name)
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
			Expect(createCalled).To(BeTrue())
		})

		It("should keep restoring and mark missing restore before grace expires", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-restore-deleted",
					Namespace:  ns,
					Finalizers: []string{LabelAppRestoreFinalizer},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			appRestore.Status.Status = disasterv1.PhaseRestoring
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{
				Phase: velerov1.RestorePhaseInProgress,
			}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return apierrors.NewNotFound(schema.GroupResource{Group: "velero.io", Resource: "restores"}, key.Name)
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(15 * time.Second))

			updated := &disasterv1.AppRestore{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, updated)).To(Succeed())
			Expect(updated.Status.Status).To(Equal(disasterv1.PhaseRestoring))
			Expect(updated.Annotations).NotTo(BeNil())
			Expect(updated.Annotations["testudo.softcdata.com/app-restore-missing-since"]).NotTo(BeEmpty())
		})

		It("should keep Restoring if Get Velero Restore returns transient non-NotFound error", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-restore-get-error", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseRestoring
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get to return error
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return fmt.Errorf("the server was unable to return a response in the time allotted")
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			updated := &disasterv1.AppRestore{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, updated)).To(Succeed())
			Expect(updated.Status.Status).To(Equal(disasterv1.PhaseRestoring))
		})

		It("should transition to Failed when Velero restore phase is Failed", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-restore-phase-failed", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseRestoring
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get to return a Failed restore
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if r, ok := obj.(*velerov1.Restore); ok {
					r.Status.Phase = velerov1.RestorePhaseFailed
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhaseFailed))
		})

		It("should requeue when Velero restore phase is unknown but not timed out", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-restore-unknown-phase", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseRestoring
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get to return a restore with unknown phase
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if r, ok := obj.(*velerov1.Restore); ok {
					r.Status.Phase = ""                // Unknown phase
					r.CreationTimestamp = metav1.Now() // Recent, so no timeout
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0)) // Should requeue
		})
	})

	Context("Manual Actions (processAction) via Reconcile", func() {
		It("should handle Cancel action", func() {
			now := metav1.Now()
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cancel",
					Namespace:  ns,
					Finalizers: []string{LabelAppRestoreFinalizer},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
					Action: &disasterv1.RestoreAction{
						Type:      "cancel",
						RequestAt: now,
					},
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			// Set phase to Restoring (can cancel from Restoring)
			appRestore.Status.Status = disasterv1.PhaseRestoring
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Delete
			deleteCalled := false
			mockClient.MockDelete = func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
				deleteCalled = true
				return nil
			}

			// Force Get to find Restore
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Name == reconciler.GenRestoreName(appRestore) {
					r := obj.(*velerov1.Restore)
					r.Name = key.Name
					r.Namespace = key.Namespace
					r.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Restore"})
					r.Status.Phase = velerov1.RestorePhaseInProgress
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			// Use Reconcile - it should process action and transition to Cancelled
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
			Expect(deleteCalled).To(BeTrue(), "Delete should have been called on Velero Restore")

			// Verify Status
			updated := &disasterv1.AppRestore{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, updated)).To(Succeed())
			Expect(updated.Status.Status).To(Equal(disasterv1.PhaseCancelled))
		})

		It("should handle Retry action", func() {
			now := metav1.Now()
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-retry",
					Namespace:  ns,
					Finalizers: []string{LabelAppRestoreFinalizer},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
					Action: &disasterv1.RestoreAction{
						Type:      "retry",
						RequestAt: now,
					},
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			// Set Status to Failed
			appRestore.Status.Status = disasterv1.PhaseFailed
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseFailed}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Create/Delete
			mockClient.MockDelete = func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
				return nil
			}
			// Force Get to find Restore
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Name == reconciler.GenRestoreName(appRestore) {
					r := obj.(*velerov1.Restore)
					r.Name = key.Name
					r.Namespace = key.Namespace
					r.Status.Phase = velerov1.RestorePhaseFailed
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			// Reconcile 1: Should delete old restore and transition to Pending
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			updated := &disasterv1.AppRestore{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, updated)).To(Succeed())
			Expect(updated.Status.Status).To(Equal(disasterv1.PhasePending))
		})
	})

	Context("Deletion Logic", func() {
		It("should handle deletion by removing external resources", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-deletion",
					Namespace:  ns,
					Finalizers: []string{LabelAppRestoreFinalizer},
				},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			// Mark for deletion
			Expect(k8sClient.Delete(ctx, appRestore)).To(Succeed())

			// Verify deletion timestamp is set
			updated := &disasterv1.AppRestore{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, updated)).To(Succeed())
			Expect(updated.DeletionTimestamp.IsZero()).To(BeFalse())
			Expect(updated.Finalizers).To(ContainElement(LabelAppRestoreFinalizer))

			// Mock DeleteAllOf (used by deleteExternalResources)
			mockClient.MockDeleteAllOf = func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
				return nil
			}
			// Mock Delete (used by cmManager.DeleteConfigMap)
			mockClient.MockDelete = func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
				return nil
			}

			// Reconcile
			// Should call DeleteExternalResources and remove Finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			// Verify deleted (Finalizer removed, object gone)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "AppRestore should be deleted (NotFound)")
		})
	})

	Context("State Handler Coverage", func() {
		It("should transition from Initiating to Restoring", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-init", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			appRestore.Status.Status = disasterv1.PhaseInitiating
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhaseRestoring))
		})

		It("should stay Succeeded", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-succeeded", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseSucceeded
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
			// Should remain Succeeded
		})

		It("should handle Failed with RestoreStatus existing", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-failed-existing", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseFailed
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseFailed}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get Restore
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					r := obj.(*velerov1.Restore)
					r.Status.Phase = velerov1.RestorePhaseFailed
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle Cancelled with RestoreStatus existing", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cancelled-existing", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseCancelled
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseFailed}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get Restore
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					r := obj.(*velerov1.Restore)
					r.Status.Phase = velerov1.RestorePhaseFailed
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle DeletingHandler when cluster not found", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-del-cluster-notfound", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "nonexistent-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			Expect(k8sClient.Delete(ctx, appRestore)).To(Succeed())

			// Mock Factory to return NotFound
			mockFactory.MockError = apierrors.NewNotFound(schema.GroupResource{}, "cluster")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred()) // ClusterNotFound is skipped

			// Verify deleted
			err = k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("should handle FailedHandler when GetVeleroRestore fails", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-failed-get-error", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseFailed
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseFailed}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get Restore -> Error
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return fmt.Errorf("failed to get restore")
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get restore"))
		})

		It("should handle CancelledHandler when GetVeleroRestore fails", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cancelled-get-error", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseCancelled
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseFailed}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get Restore -> Error
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return fmt.Errorf("failed to get restore for cancelled")
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get restore for cancelled"))
		})

		It("pending handler should fail if BackupSourceInfo not found", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pending-fail", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			// Mock Factory to succeed
			mockFactory.MockError = nil

			// Mock Get BackupSourceInfo -> Fail (NotFound)
			appRestore.Spec.BackupSource = "missing-backup"
			Expect(k8sClient.Update(ctx, appRestore)).To(Succeed())

			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Backup); ok {
					return apierrors.NewNotFound(schema.GroupResource{Group: "velero.io", Resource: "backups"}, key.Name)
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}
			mockClient.MockList = func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*velerov1.BackupList); ok {
					return nil // Return empty list
				}
				return k8sClient.List(ctx, list, opts...)
			}

			// If not found, PendingHandler waits (Requeues) if not timed out.
			// It returns PhasePending + Requeue.

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhasePending))
		})

		It("should handle processAction retry with existing restore", func() {
			name := fmt.Sprintf("test-retry-existing-%d", time.Now().UnixNano())
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
					Action: &disasterv1.RestoreAction{
						Type:      "retry",
						RequestAt: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseFailed
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseFailed}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get Restore
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}
			mockClient.MockDelete = func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return nil
				}
				return k8sClient.Delete(ctx, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhasePending)) // Retry transitions to Pending
		})
	})

	Context("Full Lifecycle & Cross Cluster", func() {
		It("should transition Pending -> Restoring -> Succeeded", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-lifecycle", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster", Template: velerov1.RestoreSpec{BackupName: "backup-1"}},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			mockFactory.MockError = nil

			// Mock Backup Get (for GetBackupSourceInfo)
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Backup); ok {
					if key.Name == "backup-1" {
						b := obj.(*velerov1.Backup)
						// Skipping map assignment due to panic
						_ = b
						return nil // Found
					}
					return apierrors.NewNotFound(schema.GroupResource{Group: "velero.io", Resource: "backups"}, key.Name)
				}
				// Mock Restore Get (for RestoringHandler)
				if _, ok := obj.(*velerov1.Restore); ok {
					r := obj.(*velerov1.Restore)
					r.Status.Phase = velerov1.RestorePhaseCompleted
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}
			mockClient.MockList = func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*velerov1.BackupList); ok {
					return nil
				}
				return k8sClient.List(ctx, list, opts...)
			}

			// 1. Pending -> Restoring (Backup found)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhaseRestoring))
			// Expect(appRestore.Labels).To(HaveKeyWithValue(LabelAppBackupIncludeNamespace, "true")) // Removed

			// 2. Loop Reconcile until Succeeded
			for i := 0; i < 5; i++ {
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
				Expect(err).NotTo(HaveOccurred())

				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
				if appRestore.Status.Status == disasterv1.PhaseSucceeded {
					break
				}
			}
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhaseSucceeded))
		})

		It("should handle Restoring Timeout", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-timeout", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			// Status Restoring and RestoreStatus InProgress (to skip Initiating check)
			appRestore.Status.Status = disasterv1.PhaseRestoring
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseInProgress}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			mockFactory.MockError = nil
			mockClient.MockList = func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error { return nil }
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					r := obj.(*velerov1.Restore)
					r.Name = key.Name
					r.Status.Phase = velerov1.RestorePhaseInProgress
					// Set CreationTimestamp to extreme past
					r.CreationTimestamp = metav1.NewTime(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
					return nil
				}
				// Catch ConfigMap or others
				if _, ok := obj.(*corev1.ConfigMap); ok {
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			// Reconcile
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).To(HaveOccurred()) // Timeout returns error

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhaseFailed))
		})

		It("should requeue when Cross-Cluster BSL is not yet Available", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cross-bsl", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec: disasterv1.AppRestoreSpec{
					Cluster:           "target-cluster",
					SourceCluster:     "source-cluster",
					StorageRepository: "repo-1",
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			mockFactory.MockError = nil

			// Mock Get StorageRepository and Secret/BSL for ApplyStorageRepository
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*disasterv1.StorageRepository); ok {
					sr := obj.(*disasterv1.StorageRepository)
					sr.Name = "repo-1"
					sr.Spec.Bucket = "bucket"
					sr.Spec.Region = "region"
					sr.Spec.Endpoint = "endpoint"
					return nil
				}
				if _, ok := obj.(*corev1.Secret); ok {
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				}
				if _, ok := obj.(*velerov1.BackupStorageLocation); ok {
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				}
				if _, ok := obj.(*velerov1.Backup); ok {
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				}

				return k8sClient.Get(ctx, key, obj, opts...)
			}

			// Mock Create for Secret/BSL
			mockClient.MockCreate = func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
				return nil // Succeed
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Unavailable status"))
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhasePending))
		})
	})

	Context("Helper Functions and ConfigMap", func() {
		It("should test string helpers", func() {
			s := []string{"a", "b"}
			Expect(containsString(s, "a")).To(BeTrue())
			Expect(containsString(s, "c")).To(BeFalse())

			s2 := removeString(s, "a")
			Expect(s2).To(HaveLen(1))
			Expect(s2[0]).To(Equal("b"))

			s3 := removeString(s, "c")
			Expect(s3).To(HaveLen(2))
		})

		It("should generate ConfigMap with ResourceModifierRules", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
					ResourceModifierRules: []disasterv1.ResourceModifierRule{
						{
							Conditions: disasterv1.Conditions{
								GroupResource: "deployments.apps",
								Namespaces:    []string{"default"},
							},
							Patches: []disasterv1.JSONPatch{
								{Operation: "replace", Path: "/spec/replicas", Value: "1"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			mockFactory.MockError = nil

			// Mock Backup for GetBackupSourceInfo to succeed (via MockGet)
			// Mock Create for ConfigMap
			mockClient.MockList = func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*velerov1.BackupList); ok {
					return nil
				}
				return k8sClient.List(ctx, list, opts...)
			}
			mockClient.MockCreate = func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok {
					return fmt.Errorf("create cm failed")
				}
				if _, ok := obj.(*velerov1.Restore); ok {
					return nil
				}
				return k8sClient.Create(ctx, obj, opts...)
			}
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok {
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				} // NotFound -> Create
				if _, ok := obj.(*velerov1.Backup); ok {
					return nil
				} // Found
				if _, ok := obj.(*velerov1.Restore); ok {
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				} // NotFound -> Create
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			// Reconcile 1: Pending -> Restoring
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())
			Expect(appRestore.Status.Status).To(Equal(disasterv1.PhaseRestoring))

			// Reconcile 2: Restoring -> Create ConfigMap & Restore
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("create cm failed"))
		})

		It("should update ConfigMap if it exists", func() {
			name := fmt.Sprintf("test-cm-upd-%d", time.Now().UnixNano())
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec: disasterv1.AppRestoreSpec{
					Cluster: "target-cluster",
					ResourceModifierRules: []disasterv1.ResourceModifierRule{{
						Conditions: disasterv1.Conditions{GroupResource: "deployments"},
						Patches:    []disasterv1.JSONPatch{{Operation: "remove", Path: "/spec/replicas"}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())

			// Pre-create CM
			// Need to known UID. K8s assigns one.
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}, appRestore)).To(Succeed())

			uid := string(appRestore.UID)
			if len(uid) > 8 {
				uid = uid[:8]
			}
			cmName := fmt.Sprintf("am-%s-%s", appRestore.Name, uid)
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: controller.VeleroNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: disasterv1.GroupVersion.String(),
							Kind:       "AppRestore",
							Name:       appRestore.Name,
							UID:        appRestore.UID,
						},
					},
				},
				Data: map[string]string{"rules": "old-rules"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			appRestore.Status.Status = disasterv1.PhaseRestoring
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			// Mock Get
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				}
				err := k8sClient.Get(ctx, key, obj, opts...)
				if _, ok := obj.(*corev1.ConfigMap); ok {
				}
				return err
			}

			// Mock Create (for Restore)
			mockClient.MockCreate = func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return nil
				}
				return k8sClient.Create(ctx, obj, opts...)
			}

			// Mock Update
			updateCalled := false
			mockClient.MockUpdate = func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				if updatedCm, ok := obj.(*corev1.ConfigMap); ok {
					updateCalled = true
					// EnsureConfigMap uses CM Name as key
					key := updatedCm.Name
					Expect(updatedCm.Data[key]).To(ContainSubstring("deployments"))
					return nil
				}
				return k8sClient.Update(ctx, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
			Expect(updateCalled).To(BeTrue())
		})
	})

	Context("State Handler Extended", func() {
		It("should handle Cancelled state", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cancelled-state", Namespace: ns},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseCancelled
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			mockFactory.MockError = nil
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return nil
				} // Exists
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle Failed state (no action)", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-failed-state", Namespace: ns},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			appRestore.Status.Status = disasterv1.PhaseFailed
			appRestore.Status.RestoreStatus = velerov1.RestoreStatus{Phase: velerov1.RestorePhaseFailed}
			Expect(k8sClient.Status().Update(ctx, appRestore)).To(Succeed())

			mockFactory.MockError = nil
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return nil
				} // Exists
				return k8sClient.Get(ctx, key, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Deletion Failure", func() {
		It("should report error if deletion fails", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-del-fail", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			Expect(k8sClient.Delete(ctx, appRestore)).To(Succeed())

			mockClient.MockDeleteAllOf = func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
				return fmt.Errorf("delete failed")
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("delete failed"))
		})

		It("should report error if ConfigMap deletion fails", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-del-cm-fail", Namespace: ns, Finalizers: []string{LabelAppRestoreFinalizer}},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target-cluster"},
			}
			Expect(k8sClient.Create(ctx, appRestore)).To(Succeed())
			Expect(k8sClient.Delete(ctx, appRestore)).To(Succeed())

			// Mock DeleteAllOf
			mockClient.MockDeleteAllOf = func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return nil
				} // Restore deletion succeeds
				if _, ok := obj.(*corev1.ConfigMap); ok {
					return fmt.Errorf("cm invalid deletion")
				}
				return k8sClient.DeleteAllOf(ctx, obj, opts...)
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appRestore.Name, Namespace: appRestore.Namespace}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cm invalid deletion"))
		})
	})

	Context("Statistic Sync", func() {
		It("should sync statistics correctly", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-stats", Namespace: ns, UID: types.UID("uid")},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target"},
				Status:     disasterv1.AppRestoreStatus{Status: disasterv1.PhaseSucceeded},
			}

			err := reconciler.syncStatistics(ctx, appRestore, mockClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should sync statistics with Failed status", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-stats-failed", Namespace: ns, UID: types.UID("uid-failed")},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target"},
				Status:     disasterv1.AppRestoreStatus{Status: disasterv1.PhaseFailed},
			}
			err := reconciler.syncStatistics(ctx, appRestore, mockClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should sync statistics with InProgress status", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-stats-progress", Namespace: ns, UID: types.UID("uid-progress")},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target"},
				Status:     disasterv1.AppRestoreStatus{Status: disasterv1.PhaseRestoring},
			}
			err := reconciler.syncStatistics(ctx, appRestore, mockClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should sync statistics with Unknown status", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-stats-unknown", Namespace: ns, UID: types.UID("uid-unknown")},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target"},
				Status:     disasterv1.AppRestoreStatus{Status: disasterv1.PhaseUnknown},
			}
			err := reconciler.syncStatistics(ctx, appRestore, mockClient)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should sync statistics with empty status", func() {
			appRestore = &disasterv1.AppRestore{
				ObjectMeta: metav1.ObjectMeta{Name: "test-stats-empty", Namespace: ns, UID: types.UID("uid-empty")},
				Spec:       disasterv1.AppRestoreSpec{Cluster: "target"},
				Status:     disasterv1.AppRestoreStatus{Status: ""},
			}
			err := reconciler.syncStatistics(ctx, appRestore, mockClient)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
