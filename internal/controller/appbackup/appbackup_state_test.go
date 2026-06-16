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
	. "github.com/softcdata/testudo-operator/internal/controller"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/softcdata/testudo-operator/pkg/helper"
)

var _ = Describe("AppBackup State Machine", func() {
	var (
		ctx           context.Context
		r             *AppBackupReconciler
		fakeClient    client.Client
		remoteClient  client.Client
		scheme        *runtime.Scheme
		appBackup     *disasterv1.AppBackup
		storageRepo   *disasterv1.StorageRepository
		clientFactory *MockClientFactory
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		_ = disasterv1.AddToScheme(scheme)
		_ = velerov1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		appBackup = &disasterv1.AppBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-appbackup",
				Namespace: "default",
				UID:       types.UID("test-uid"),
			},
			Spec: disasterv1.AppBackupSpec{
				Cluster: "test-cluster",
				Template: velerov1.BackupSpec{
					StorageLocation: "test-repo",
				},
			},
		}

		storageRepo = &disasterv1.StorageRepository{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-repo",
				Namespace: "disaster-system",
			},
			Spec: disasterv1.StorageRepositorySpec{
				Bucket:   "test-bucket",
				Region:   "us-east-1",
				Endpoint: "http://minio",
			},
		}

		// Setup Fake Clients
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(appBackup, storageRepo).Build()
		remoteClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		clientFactory = &MockClientFactory{MockClient: remoteClient}

		r = &AppBackupReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			Recorder:      record.NewFakeRecorder(100),
			ClientFactory: clientFactory,
			StatsHelper:   helper.NewStatisticsHelper(fakeClient),
		}
	})

	Context("PendingHandler", func() {
		BeforeEach(func() {
			// Ensure Finalizer is present so we don't just return Pending for finalizer addition
			controllerutil.AddFinalizer(appBackup, LabelAppBackupFinalizer)
		})

		It("should transition to Ready when all checks pass", func() {
			// Pre-create BSL in remote client with Available status
			bsl := &velerov1.BackupStorageLocation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo-test-cluster",
					Namespace: "velero",
				},
				Spec: velerov1.BackupStorageLocationSpec{
					StorageType: velerov1.StorageType{
						ObjectStorage: &velerov1.ObjectStorageLocation{
							Bucket: "test-bucket",
						},
					},
					Config: map[string]string{
						"region": "us-east-1",
						"s3Url":  "http://minio",
					},
				},
				Status: velerov1.BackupStorageLocationStatus{
					Phase: velerov1.BackupStorageLocationPhaseAvailable,
				},
			}
			Expect(remoteClient.Create(ctx, bsl)).To(Succeed())

			handler := &PendingHandler{}
			phase, _, err := handler.Handle(ctx, r, appBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(PhaseReady))
			Expect(controllerutil.ContainsFinalizer(appBackup, LabelAppBackupFinalizer)).To(BeTrue())
		})

		It("should add finalizer and transition to Ready in the same pass", func() {
			appBackup.Finalizers = nil
			bsl := &velerov1.BackupStorageLocation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo-test-cluster",
					Namespace: "velero",
				},
				Spec: velerov1.BackupStorageLocationSpec{
					StorageType: velerov1.StorageType{
						ObjectStorage: &velerov1.ObjectStorageLocation{
							Bucket: "test-bucket",
						},
					},
					Config: map[string]string{
						"region": "us-east-1",
						"s3Url":  "http://minio",
					},
				},
				Status: velerov1.BackupStorageLocationStatus{
					Phase: velerov1.BackupStorageLocationPhaseAvailable,
				},
			}
			Expect(remoteClient.Create(ctx, bsl)).To(Succeed())

			handler := &PendingHandler{}
			phase, res, err := handler.Handle(ctx, r, appBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(PhaseReady))
			Expect(res.Requeue).To(BeTrue())
			Expect(controllerutil.ContainsFinalizer(appBackup, LabelAppBackupFinalizer)).To(BeTrue())
		})

		It("should transition to Failed when StorageRepository is missing", func() {
			// Delete StorageRepo
			Expect(fakeClient.Delete(ctx, storageRepo)).To(Succeed())

			handler := &PendingHandler{}
			phase, _, err := handler.Handle(ctx, r, appBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(PhaseFailed))
		})

		It("should stay in Pending (Requeue) when Client creation fails", func() {
			clientFactory.MockError = fmt.Errorf("connection error")

			handler := &PendingHandler{}
			phase, res, err := handler.Handle(ctx, r, appBackup)
			Expect(err).To(HaveOccurred())
			Expect(phase).To(Equal(PhasePending))
			Expect(res.RequeueAfter).To(Equal(time.Duration(0)))
		})
	})

	Context("ReadyHandler", func() {
		BeforeEach(func() {
			// Ensure Finalizer is present as PendingHandler would add it
			controllerutil.AddFinalizer(appBackup, LabelAppBackupFinalizer)

			// ReadyHandler assumes PendingHandler has already ensured BSL exists/was applied.
			// Seed an Available BSL to avoid DefaultBSL create-and-error (Unavailable) in unit tests.
			bsl := &velerov1.BackupStorageLocation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo-test-cluster",
					Namespace: VeleroNamespace,
				},
				Spec: velerov1.BackupStorageLocationSpec{
					StorageType: velerov1.StorageType{
						ObjectStorage: &velerov1.ObjectStorageLocation{
							Bucket: "test-bucket",
						},
					},
					Config: map[string]string{
						"region": "us-east-1",
						"s3Url":  "http://minio",
					},
				},
				Status: velerov1.BackupStorageLocationStatus{
					Phase: velerov1.BackupStorageLocationPhaseAvailable,
				},
			}
			Expect(remoteClient.Create(ctx, bsl)).To(Succeed())
		})

		Context("Provisioning", func() {
			It("should create Velero Schedule if Spec.Schedule is set", func() {
				appBackup.Spec.Schedule = "0 0 * * *"
				handler := &ReadyHandler{}
				phase, _, err := handler.Handle(ctx, r, appBackup)
				Expect(err).NotTo(HaveOccurred())
				Expect(phase).To(Equal(PhaseReady))

				// Check Schedule created in Remote Client
				schedule := &velerov1.Schedule{}
				err = remoteClient.Get(ctx, types.NamespacedName{Name: r.GenScheduleName(appBackup), Namespace: VeleroNamespace}, schedule)
				Expect(err).NotTo(HaveOccurred())
				Expect(schedule.Spec.Schedule).To(Equal("CRON_TZ=Asia/Shanghai 0 0 * * *"))
			})

			It("should create one-off Backup if Spec.Schedule is empty and no history", func() {
				appBackup.Spec.Schedule = ""
				handler := &ReadyHandler{}
				phase, _, err := handler.Handle(ctx, r, appBackup)
				Expect(err).NotTo(HaveOccurred())
				Expect(phase).To(Equal(PhaseReady))

				// Check Backup created in Remote Client
				backupList := &velerov1.BackupList{}
				err = remoteClient.List(ctx, backupList)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(backupList.Items)).To(Equal(1))
				Expect(backupList.Items[0].Labels[LabelAppBackupUID]).To(Equal(string(appBackup.UID)))
			})
		})

		Context("Observation", func() {
			It("should update status based on latest Velero Backup", func() {
				// Create a backup in remote client
				backup := &velerov1.Backup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup-1",
						Namespace: VeleroNamespace,
						Labels: map[string]string{
							LabelAppBackupUID: string(appBackup.UID),
						},
						CreationTimestamp: metav1.Now(),
					},
					Status: velerov1.BackupStatus{
						Phase: velerov1.BackupPhaseCompleted,
					},
				}
				Expect(remoteClient.Create(ctx, backup)).To(Succeed())

				handler := &ReadyHandler{}
				phase, _, err := handler.Handle(ctx, r, appBackup)
				Expect(err).NotTo(HaveOccurred())
				Expect(phase).To(Equal(PhaseReady))

				Expect(appBackup.Status.TotalBackups).To(Equal(1))
				Expect(appBackup.Status.BackupStatus.Phase).To(Equal(velerov1.BackupPhaseCompleted))
				Expect(len(appBackup.Status.History)).To(Equal(1))
				Expect(appBackup.Status.History[0].Name).To(Equal("backup-1"))
			})
		})

		Context("Action Handling", func() {
			BeforeEach(func() {
				// Ensure we don't hit the "Initial Backup" logic
				appBackup.Status.TotalBackups = 1
				appBackup.Status.History = []disasterv1.BackupRecord{{Name: "existing"}}
			})

			It("should handle Backup action", func() {
				appBackup.Spec.Action = &disasterv1.BackupAction{
					Type:      "Backup",
					RequestAt: metav1.Now(),
				}

				handler := &ReadyHandler{}
				phase, _, err := handler.Handle(ctx, r, appBackup)
				Expect(err).NotTo(HaveOccurred())
				Expect(phase).To(Equal(PhaseReady))

				// Verify backup created
				backupList := &velerov1.BackupList{}
				Expect(remoteClient.List(ctx, backupList)).To(Succeed())
				Expect(len(backupList.Items)).To(Equal(1))
				Expect(appBackup.Status.LastAction).NotTo(BeNil())
			})

			It("should handle Cancel action", func() {
				// Create a running backup
				backup := &velerov1.Backup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backup-running",
						Namespace: VeleroNamespace,
						Labels: map[string]string{
							LabelAppBackupUID: string(appBackup.UID),
						},
						CreationTimestamp: metav1.Now(),
					},
					Status: velerov1.BackupStatus{
						Phase: velerov1.BackupPhaseInProgress,
					},
				}
				Expect(remoteClient.Create(ctx, backup)).To(Succeed())

				appBackup.Spec.Action = &disasterv1.BackupAction{
					Type:      "Cancel",
					RequestAt: metav1.Now(),
				}

				handler := &ReadyHandler{}
				phase, _, err := handler.Handle(ctx, r, appBackup)
				Expect(err).NotTo(HaveOccurred())
				Expect(phase).To(Equal(PhaseReady))

				// Verify backup deleted
				err = remoteClient.Get(ctx, types.NamespacedName{Name: "backup-running", Namespace: VeleroNamespace}, &velerov1.Backup{})
				Expect(err).To(HaveOccurred()) // Should be NotFound
			})
		})
	})

	Context("FailedHandler", func() {
		It("should transition to Pending", func() {
			handler := &FailedHandler{}
			phase, _, err := handler.Handle(ctx, r, appBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(PhasePending))
		})
	})

	Context("DeletingHandler", func() {
		It("should delete external resources and remove finalizer", func() {
			// Setup: Add finalizer and create external resources
			controllerutil.AddFinalizer(appBackup, LabelAppBackupFinalizer)
			// Update the object in the fake client to have the finalizer
			Expect(fakeClient.Update(ctx, appBackup)).To(Succeed())

			backup := &velerov1.Backup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backup-to-delete",
					Namespace: VeleroNamespace,
					Labels: map[string]string{
						LabelAppBackupUID: string(appBackup.UID),
					},
				},
			}
			Expect(remoteClient.Create(ctx, backup)).To(Succeed())

			// Trigger Deletion via Client
			Expect(fakeClient.Delete(ctx, appBackup)).To(Succeed())
			// Fetch updated object to get DeletionTimestamp
			Expect(fakeClient.Get(ctx, types.NamespacedName{Name: appBackup.Name, Namespace: appBackup.Namespace}, appBackup)).To(Succeed())

			handler := &DeletingHandler{}
			phase, _, err := handler.Handle(ctx, r, appBackup)
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal(PhaseDeleting))

			// Verify Finalizer removed
			Expect(controllerutil.ContainsFinalizer(appBackup, LabelAppBackupFinalizer)).To(BeFalse())

			// Verify DeleteBackupRequest created. The request name is timestamped.
			deleteReqs := &velerov1.DeleteBackupRequestList{}
			Expect(remoteClient.List(ctx, deleteReqs, client.InNamespace(VeleroNamespace))).To(Succeed())
			found := false
			for _, deleteReq := range deleteReqs.Items {
				if deleteReq.Spec.BackupName == "backup-to-delete" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue())
		})
	})
})
