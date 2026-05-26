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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	"github.com/softcdata/testudo-operator/pkg/helper"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
)

var _ = Describe("AppRestore State Machine", func() {
	var (
		reconciler       *AppRestoreReconciler
		fakeClient       client.Client
		mockFactory      *controller.MockClientFactory
		mockTargetClient *controller.MockClient
		appRestore       *disasterv1.AppRestore
		ctx              context.Context
		recorder         *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()
		recorder = record.NewFakeRecorder(100)

		fakeClient = k8sClient // EnvTest client for AppRestore (CRD exists)

		mockTargetClient = &controller.MockClient{
			Client: fakeClient,
		}
		mockFactory = &controller.MockClientFactory{
			MockClient: mockTargetClient,
		}

		reconciler = &AppRestoreReconciler{
			Client:        fakeClient,
			Scheme:        fakeClient.Scheme(),
			Recorder:      recorder,
			ClientFactory: mockFactory,
			StatsHelper:   helper.NewStatisticsHelper(fakeClient),
		}

		appRestore = &disasterv1.AppRestore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-apprestore",
				Namespace: "default",
				UID:       "test-uid",
			},
			Spec: disasterv1.AppRestoreSpec{
				Cluster:       "test-cluster",
				SourceCluster: "test-cluster",
				Template: velerov1.RestoreSpec{
					BackupName: "backup-source",
				},
			},
		}

		// Ensure namespaces
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
		_ = fakeClient.Create(ctx, ns)
	})

	Context("PendingHandler", func() {
		var handler *PendingHandler

		BeforeEach(func() {
			handler = &PendingHandler{}

			// Configure MockGet to return Backup
			mockTargetClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if b, ok := obj.(*velerov1.Backup); ok {
					if key.Name == "backup-source" {
						b.Name = "backup-source"
						b.Namespace = controller.VeleroNamespace
						return nil
					}
					return errors.NewNotFound(velerov1.Resource("backup"), key.Name)
				}
				return fakeClient.Get(ctx, key, obj, opts...)
			}
		})

		It("should add finalizer if missing", func() {
			phase, res, err := handler.Handle(ctx, reconciler, appRestore)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(disasterv1.PhasePending))
			Expect(res.Requeue).To(BeFalse())
			Expect(controllerutil.ContainsFinalizer(appRestore, LabelAppRestoreFinalizer)).To(BeTrue())
		})

		It("should transition to Restoring if checks pass", func() {
			controllerutil.AddFinalizer(appRestore, LabelAppRestoreFinalizer)
			phase, res, err := handler.Handle(ctx, reconciler, appRestore)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(disasterv1.PhaseRestoring))
			Expect(res).To(Equal(ctrl.Result{}))
		})
	})

	Context("RestoringHandler", func() {
		var handler *RestoringHandler

		BeforeEach(func() {
			handler = &RestoringHandler{}
			appRestore.Status.Status = disasterv1.PhaseRestoring
		})

		It("should create Velero Restore and ConfigMap", func() {
			// Mock Create to capture restore creation
			configMapCreated := false
			restoreCreated := false

			appRestore.Spec.ResourceModifierRules = []disasterv1.ResourceModifierRule{
				{
					Conditions: disasterv1.Conditions{
						GroupResource: "persistentvolumeclaims",
					},
					Patches: []disasterv1.JSONPatch{
						{
							Operation: "replace",
							Path:      "/spec/storageClassName",
							Value:     "premium",
						},
					},
				},
			}

			mockTargetClient.MockCreate = func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
				if r, ok := obj.(*velerov1.Restore); ok {
					Expect(r.Name).To(Equal(reconciler.GenRestoreName(appRestore)))
					restoreCreated = true
					return nil // Simulate success
				}
				if _, ok := obj.(*corev1.ConfigMap); ok {
					configMapCreated = true
					return nil // Simulate success
				}
				return fakeClient.Create(ctx, obj, opts...)
			}

			// Mock Get to return NotFound for Restore AND ConfigMap initially
			mockTargetClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return errors.NewNotFound(velerov1.Resource("restore"), key.Name)
				}
				if _, ok := obj.(*corev1.ConfigMap); ok {
					// ConfigMap check
					return errors.NewNotFound(corev1.Resource("configmap"), key.Name)
				}
				return fakeClient.Get(ctx, key, obj, opts...)
			}

			// Mock List for ConfigMapManager.EnsureConfigMap
			mockTargetClient.MockList = func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.ConfigMapList); ok {
					// Return empty list so EnsureConfigMap tries to create one
					return nil
				}
				return fakeClient.List(ctx, list, opts...)
			}

			// Mock Get to return NotFound for Restore initially
			mockTargetClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					return errors.NewNotFound(velerov1.Resource("restore"), key.Name)
				}
				return fakeClient.Get(ctx, key, obj, opts...)
			}

			phase, res, err := handler.Handle(ctx, reconciler, appRestore)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(disasterv1.PhaseRestoring))
			Expect(res.RequeueAfter).To(Equal(controller.RestorePhaseCreateWaitSeconds))
			if !restoreCreated {
				println("DEBUG: restoreCreated is false")
			}
			if !configMapCreated {
				println("DEBUG: configMapCreated is false")
			}
			Expect(restoreCreated).To(BeTrue())
			Expect(configMapCreated).To(BeTrue())
		})
	})

	Context("DeletingHandler", func() {
		var handler *DeletingHandler

		BeforeEach(func() {
			handler = &DeletingHandler{}
			controllerutil.AddFinalizer(appRestore, LabelAppRestoreFinalizer)
		})

		It("should delete external resources", func() {
			restoreDeleted := false
			cmDeleted := false

			mockTargetClient.MockDeleteAllOf = func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					restoreDeleted = true
					return nil
				}
				if _, ok := obj.(*corev1.ConfigMap); ok {
					cmDeleted = true
					return nil
				}
				return nil
			}

			mockTargetClient.MockDelete = func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok {
					cmDeleted = true
					return nil
				}
				return nil
			}

			// We also need MockList for ConfigMapManager.DeleteConfigMap as it might look for it?
			// apprestore_controller.go uses cmManager.DeleteConfigMap
			// configmap_manager.go: DeleteConfigMap -> GetConfigMap -> List -> Delete

			mockTargetClient.MockList = func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				if cmList, ok := list.(*corev1.ConfigMapList); ok {
					// Return one item to trigger delete
					cmList.Items = []corev1.ConfigMap{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "cm-to-delete",
								Namespace: controller.VeleroNamespace,
							},
						},
					}
					return nil
				}
				return nil
			}

			phase, _, err := handler.Handle(ctx, reconciler, appRestore)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(disasterv1.PhaseDeleting))

			Expect(restoreDeleted).To(BeTrue())
			Expect(cmDeleted).To(BeTrue())
		})
	})
})
