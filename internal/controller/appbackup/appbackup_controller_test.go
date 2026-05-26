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

package appbackup

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	"github.com/softcdata/testudo-operator/pkg/helper"
)

var _ = Describe("AppBackup Controller", func() {
	var (
		ctx                context.Context
		resourceName       string
		clusterName        string
		storageName        string
		namespace          string
		typeNamespacedName types.NamespacedName
	)

	BeforeEach(func() {
		ctx = context.Background()
		resourceName = fmt.Sprintf("test-appbackup-%d", time.Now().UnixNano())
		clusterName = fmt.Sprintf("test-cluster-%d", time.Now().UnixNano())
		storageName = fmt.Sprintf("test-storage-%d", time.Now().UnixNano())
		namespace = "default"

		typeNamespacedName = types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}

		// Create Cluster resource
		cluster := &disasterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
			Spec: disasterv1.ClusterSpec{
				KubeConfig: []byte("fake-kubeconfig"),
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		// Create StorageRepository resource
		storage := &disasterv1.StorageRepository{
			ObjectMeta: metav1.ObjectMeta{
				Name:      storageName,
				Namespace: "disaster-system", // Match PendingHandler's expectation
			},
			Spec: disasterv1.StorageRepositorySpec{
				Bucket:    "test-bucket",
				Endpoint:  "http://minio:9000",
				Region:    "us-east-1",
				AccessKey: "minio",
				SecretKey: "minio123",
			},
		}
		// Ensure namespace exists
		utilsNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "disaster-system"}}
		_ = k8sClient.Create(ctx, utilsNs)
		veleroNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "velero"}}
		_ = k8sClient.Create(ctx, veleroNs)
		Expect(k8sClient.Create(ctx, storage)).To(Succeed())
	})

	AfterEach(func() {
		// Cleanup
	})

	It("should transition from Pending to Ready on successful BSL application", func() {
		By("Creating a new AppBackup")
		appBackup := &disasterv1.AppBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: disasterv1.AppBackupSpec{
				Cluster: clusterName,
				Template: velerov1.BackupSpec{
					StorageLocation: storageName,
				},
			},
		}
		Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

		// Ensure we don't hit the "Initial Backup" logic
		// Fetch the created appBackup to update its status
		Expect(k8sClient.Get(ctx, typeNamespacedName, appBackup)).To(Succeed())
		appBackup.Status.TotalBackups = 1
		appBackup.Status.History = []disasterv1.BackupRecord{{Name: "existing"}}
		Expect(k8sClient.Status().Update(ctx, appBackup)).To(Succeed())

		// Setup Reconciler with Mock
		mockTargetClient := &controller.MockClient{
			Client: k8sClient, // Use same client for simplicity if not conflicting
			MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if bsl, ok := obj.(*velerov1.BackupStorageLocation); ok {
					bsl.Name = key.Name
					bsl.Namespace = key.Namespace
					bsl.Status.Phase = velerov1.BackupStorageLocationPhaseAvailable
					// Initialize Spec to avoid panic
					bsl.Spec = velerov1.BackupStorageLocationSpec{
						StorageType: velerov1.StorageType{
							ObjectStorage: &velerov1.ObjectStorageLocation{
								Bucket: "test-bucket",
								Prefix: clusterName,
							},
						},
						Config: map[string]string{
							"region": "us-east-1",
							"s3Url":  "http://minio:9000",
						},
					}
					return nil
				}
				return k8sClient.Get(ctx, key, obj, opts...)
			},
			MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				switch obj.(type) {
				case *velerov1.BackupStorageLocation, *corev1.Secret:
					return nil
				default:
					return k8sClient.Update(ctx, obj, opts...)
				}
			},
			MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
				switch obj.(type) {
				case *velerov1.BackupStorageLocation, *corev1.Secret:
					return nil
				default:
					return k8sClient.Create(ctx, obj, opts...)
				}
			},
		}
		mockFactory := &controller.MockClientFactory{
			MockClient: mockTargetClient,
		}

		reconciler := &AppBackupReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Recorder:      record.NewFakeRecorder(100),
			ClientFactory: mockFactory,
			StatsHelper:   helper.NewStatisticsHelper(k8sClient),
		}

		By("Reconciling the resource - First pass (Add Finalizer)")
		result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue || result.RequeueAfter > 0).To(BeTrue())

		By("Reconciling the resource - Second pass (Apply BSL and Move to Ready)")
		result, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		By("Checking status transitioned to Ready")
		err = k8sClient.Get(ctx, typeNamespacedName, appBackup)
		Expect(err).NotTo(HaveOccurred())
		Expect(appBackup.Status.Status).To(Equal(string(PhaseReady)))
	})

	It("should transition to Failed if StorageRepository is missing", func() {
		By("Creating an AppBackup with non-existent storage")
		appBackup := &disasterv1.AppBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName + "-failed",
				Namespace: namespace,
			},
			Spec: disasterv1.AppBackupSpec{
				Cluster: clusterName,
				Template: velerov1.BackupSpec{
					StorageLocation: "non-existent",
				},
			},
		}
		Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

		reconciler := &AppBackupReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(100),
			ClientFactory: &controller.MockClientFactory{
				MockClient: &controller.MockClient{
					Client: k8sClient,
				},
			},
			StatsHelper: helper.NewStatisticsHelper(k8sClient),
		}

		By("Reconciling - First Pass")
		reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appBackup.Name, Namespace: namespace}})

		By("Reconciling - Second Pass (Should fail)")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appBackup.Name, Namespace: namespace}})
		Expect(err).NotTo(HaveOccurred())

		By("Checking status transitioned to Failed")
		err = k8sClient.Get(ctx, types.NamespacedName{Name: appBackup.Name, Namespace: namespace}, appBackup)
		Expect(err).NotTo(HaveOccurred())
		Expect(appBackup.Status.Status).To(Equal(string(PhaseFailed)))
	})

	It("should handle Deleting status with zero DeletionTimestamp gracefully", func() {
		By("Creating an AppBackup with Deleting status but no deletion timestamp")
		appBackup := &disasterv1.AppBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:       resourceName + "-deleting-status",
				Namespace:  namespace,
				Finalizers: []string{"testudo.softcdata.com/appbackup-finalizer"},
			},
			Spec: disasterv1.AppBackupSpec{
				Cluster: clusterName,
				Template: velerov1.BackupSpec{
					StorageLocation: storageName,
				},
			},
		}
		Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

		// Set status to Deleting manually (edge case)
		appBackup.Status.Status = string(PhaseDeleting)
		appBackup.Status.History = []disasterv1.BackupRecord{{Name: "existing"}}
		Expect(k8sClient.Status().Update(ctx, appBackup)).To(Succeed())

		mockFactory := &controller.MockClientFactory{
			MockClient: &controller.MockClient{Client: k8sClient},
		}

		reconciler := &AppBackupReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Recorder:      record.NewFakeRecorder(100),
			ClientFactory: mockFactory,
			StatsHelper:   helper.NewStatisticsHelper(k8sClient),
		}

		// First reconcile resets internal phase
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appBackup.Name, Namespace: namespace}})
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile processes with the reset phase
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appBackup.Name, Namespace: namespace}})
		Expect(err).NotTo(HaveOccurred())

		// After two reconciles, status should not be Deleting anymore
		err = k8sClient.Get(ctx, types.NamespacedName{Name: appBackup.Name, Namespace: namespace}, appBackup)
		Expect(err).NotTo(HaveOccurred())
		// The key assertion: Deleting status without DeletionTimestamp should be reset to Failed (SR not found) or Pending
		Expect(appBackup.Status.Status).To(Or(Equal(string(PhaseFailed)), Equal(string(PhasePending))))
	})

	It("should handle unknown phase by defaulting to PendingHandler", func() {
		By("Creating an AppBackup with unknown status")
		appBackup := &disasterv1.AppBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:       resourceName + "-unknown",
				Namespace:  namespace,
				Finalizers: []string{"testudo.softcdata.com/appbackup-finalizer"},
			},
			Spec: disasterv1.AppBackupSpec{
				Cluster: clusterName,
				Template: velerov1.BackupSpec{
					StorageLocation: storageName,
				},
			},
		}
		Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

		// Set status to an unknown value
		appBackup.Status.Status = "UnknownPhase"
		appBackup.Status.History = []disasterv1.BackupRecord{{Name: "existing"}}
		Expect(k8sClient.Status().Update(ctx, appBackup)).To(Succeed())

		mockFactory := &controller.MockClientFactory{
			MockClient: &controller.MockClient{Client: k8sClient},
		}

		reconciler := &AppBackupReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			Recorder:      record.NewFakeRecorder(100),
			ClientFactory: mockFactory,
			StatsHelper:   helper.NewStatisticsHelper(k8sClient),
		}

		By("Reconciling - Should use default handler")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appBackup.Name, Namespace: namespace}})
		Expect(err).NotTo(HaveOccurred())
	})
	Context("When handling Delete action", func() {
		It("should delete the specified backup and update history", func() {
			By("Initializing AppBackup with history")
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName + "-delete",
					Namespace: namespace,
				},
				Spec: disasterv1.AppBackupSpec{
					Cluster: clusterName,
					Template: velerov1.BackupSpec{
						StorageLocation: storageName,
					},
					Action: &disasterv1.BackupAction{
						Type:         "Delete",
						TargetBackup: "backup-to-delete",
						RequestAt:    metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

			// Pre-populate status
			appBackup.Status.Status = string(PhaseReady)
			appBackup.Status.TotalBackups = 2
			appBackup.Status.History = []disasterv1.BackupRecord{
				{Name: "backup-to-delete", ManagedStatus: "Completed"},
				{Name: "keep-me", ManagedStatus: "Completed"},
			}
			Expect(k8sClient.Status().Update(ctx, appBackup)).To(Succeed())

			// Setup Mock Handler
			// We need to mock the remote client's Delete call
			mockTargetClient := &controller.MockClient{
				Client: k8sClient,
				MockDelete: func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					if _, ok := obj.(*velerov1.Backup); ok {
						return fmt.Errorf("unexpected direct delete of Backup object")
					}
					return k8sClient.Delete(ctx, obj, opts...)
				},
				MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					if req, ok := obj.(*velerov1.DeleteBackupRequest); ok {
						if req.Spec.BackupName == "backup-to-delete" {
							return nil
						}
					}
					return k8sClient.Create(ctx, obj, opts...)
				},
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					if backupList, ok := list.(*velerov1.BackupList); ok {
						backupList.Items = []velerov1.Backup{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "backup-to-delete",
									Namespace: "velero",
									Labels: map[string]string{
										"testudo.softcdata.com/appbackup": appBackup.Name,
									},
								},
								Status: velerov1.BackupStatus{
									Phase: velerov1.BackupPhaseCompleted,
								},
							},
							{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "keep-me",
									Namespace: "velero",
									Labels: map[string]string{
										"testudo.softcdata.com/appbackup": appBackup.Name,
									},
								},
								Status: velerov1.BackupStatus{
									Phase: velerov1.BackupPhaseCompleted,
								},
							},
						}
						return nil
					}
					return k8sClient.List(ctx, list, opts...)
				},
			}
			mockFactory := &controller.MockClientFactory{
				MockClient: mockTargetClient,
			}

			reconciler := &AppBackupReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				Recorder:      record.NewFakeRecorder(100),
				ClientFactory: mockFactory,
				StatsHelper:   helper.NewStatisticsHelper(k8sClient),
			}

			By("Reconciling the delete action")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appBackup.Name, Namespace: namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the backup was removed from history")
			err = k8sClient.Get(ctx, types.NamespacedName{Name: appBackup.Name, Namespace: namespace}, appBackup)
			Expect(err).NotTo(HaveOccurred())

			// This assertion is expected to FAIL until implementation is done
			// This assertion expects history to be removed
			Expect(appBackup.Status.TotalBackups).To(Equal(1))
			Expect(appBackup.Status.History).To(HaveLen(1))
			Expect(appBackup.Status.History[0].Name).To(Equal("keep-me"))
			Expect(appBackup.Status.LastAction.Type).To(Equal("Delete"))
		})

		It("should clean up history even if backup is not found", func() {
			By("Initializing AppBackup with history containing phantom backup")
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName + "-delete-phantom",
					Namespace: namespace,
				},
				Spec: disasterv1.AppBackupSpec{
					Cluster: clusterName,
					Template: velerov1.BackupSpec{
						StorageLocation: storageName,
					},
					Action: &disasterv1.BackupAction{
						Type:         "Delete",
						TargetBackup: "phantom-backup",
						RequestAt:    metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

			// Pre-populate status
			appBackup.Status.Status = string(PhaseReady)
			appBackup.Status.TotalBackups = 1
			appBackup.Status.History = []disasterv1.BackupRecord{
				{Name: "phantom-backup", ManagedStatus: "Completed"},
			}
			Expect(k8sClient.Status().Update(ctx, appBackup)).To(Succeed())

			// Setup Mock Handler - List returns EMPTY
			mockTargetClient := &controller.MockClient{
				Client: k8sClient,
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					if backupList, ok := list.(*velerov1.BackupList); ok {
						backupList.Items = []velerov1.Backup{} // Empty
						return nil
					}
					return k8sClient.List(ctx, list, opts...)
				},
			}
			mockFactory := &controller.MockClientFactory{
				MockClient: mockTargetClient,
			}

			reconciler := &AppBackupReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				Recorder:      record.NewFakeRecorder(100),
				ClientFactory: mockFactory,
				StatsHelper:   helper.NewStatisticsHelper(k8sClient),
			}

			By("Reconciling")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appBackup.Name, Namespace: namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying history is empty")
			err = k8sClient.Get(ctx, types.NamespacedName{Name: appBackup.Name, Namespace: namespace}, appBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(appBackup.Status.History).To(BeEmpty())
			Expect(appBackup.Status.TotalBackups).To(Equal(0))
		})
		It("should NOT recreate initial backup after deleting the last backup", func() {
			By("Creating an AppBackup with history of one backup")
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName + "-bugfix",
					Namespace: namespace,
				},
				Spec: disasterv1.AppBackupSpec{
					Cluster: clusterName,
					Template: velerov1.BackupSpec{
						StorageLocation: storageName,
					},
					// No Schedule -> Once-off
					Action: &disasterv1.BackupAction{
						Type:         "Delete",
						TargetBackup: "last-backup",
						RequestAt:    metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

			// Simulate that it had a backup
			appBackup.Status.Status = string(PhaseReady)
			appBackup.Status.TotalBackups = 1
			appBackup.Status.History = []disasterv1.BackupRecord{
				{Name: "last-backup", ManagedStatus: "Completed", Phase: "Completed"},
			}
			Expect(k8sClient.Status().Update(ctx, appBackup)).To(Succeed())

			// Mock Client: Return empty list (simulating deletion success), and handle Delete call
			createCalled := false
			mockTargetClient := &controller.MockClient{
				Client: k8sClient,
				MockList: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					if backupList, ok := list.(*velerov1.BackupList); ok {
						backupList.Items = []velerov1.Backup{} // Empty!
						return nil
					}
					return k8sClient.List(ctx, list, opts...)
				},
				MockDelete: func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					if _, ok := obj.(*velerov1.Backup); ok {
						return fmt.Errorf("unexpected direct deletion of Backup")
					}
					return nil
				},
				MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*velerov1.Backup); ok {
						createCalled = true
					}
					if _, ok := obj.(*velerov1.DeleteBackupRequest); ok {
						return nil
					}
					return k8sClient.Create(ctx, obj, opts...)
				},
			}
			mockFactory := &controller.MockClientFactory{
				MockClient: mockTargetClient,
			}

			reconciler := &AppBackupReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				Recorder:      record.NewFakeRecorder(100),
				ClientFactory: mockFactory,
				StatsHelper:   helper.NewStatisticsHelper(k8sClient),
			}

			By("Reconciling the Delete action")
			// This should process the delete, clear history (currently bugged behavior), AND THEN potentially trigger recreation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: appBackup.Name, Namespace: namespace}})
			Expect(err).NotTo(HaveOccurred())

			By("Ensuring CreateVeleroBackup was NOT called")
			Expect(createCalled).To(BeFalse(), "Controller mistakenly attempted to recreate the backup after deletion")

			By("Checking Status History is empty")
			// Reload
			err = k8sClient.Get(ctx, types.NamespacedName{Name: appBackup.Name, Namespace: namespace}, appBackup)
			Expect(err).NotTo(HaveOccurred())
			// History should be empty because we deleted the only backup
			Expect(appBackup.Status.History).To(BeEmpty())
			// But recreation should be prevented because HasRunInitialBackup is implicitly handled (by migration or initial setup)
			// Actually, in this test setup, we need to ensure HasRunInitialBackup is true or becomes true.
			// The migration logic runs in syncStatus.
			Expect(appBackup.Status.HasRunInitialBackup).To(BeTrue())
		})
	})
})
