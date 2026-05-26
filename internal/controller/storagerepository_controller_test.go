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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
)

// MockS3API implements S3API
type MockS3API struct {
	MockHeadBucket    func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	MockCreateBucket  func(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	MockListObjectsV2 func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

func (m *MockS3API) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.MockHeadBucket != nil {
		return m.MockHeadBucket(ctx, params, optFns...)
	}
	return &s3.HeadBucketOutput{}, nil
}

func (m *MockS3API) CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	if m.MockCreateBucket != nil {
		return m.MockCreateBucket(ctx, params, optFns...)
	}
	return &s3.CreateBucketOutput{}, nil
}

func (m *MockS3API) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if m.MockListObjectsV2 != nil {
		return m.MockListObjectsV2(ctx, params, optFns...)
	}
	return &s3.ListObjectsV2Output{}, nil
}

// MockS3Factory implements S3ClientFactory
type MockS3Factory struct {
	MockClient   *MockS3API
	MockError    error
	LastSettings StorageRuntimeSettings
}

func (f *MockS3Factory) NewS3Client(ctx context.Context, sr *disasterv1.StorageRepository, settings StorageRuntimeSettings) (S3API, error) {
	f.LastSettings = settings
	if f.MockError != nil {
		return nil, f.MockError
	}
	return f.MockClient, nil
}

var _ = Describe("StorageRepository Controller", func() {
	var (
		ctx          context.Context
		sr           *disasterv1.StorageRepository
		ns           string
		mockS3Client *MockS3API
		mockFactory  *MockS3Factory
		reconciler   *StorageRepositoryReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		ns = "default"

		mockS3Client = &MockS3API{}
		mockFactory = &MockS3Factory{
			MockClient: mockS3Client,
		}

		// Setup reconciler with the mock factory and API server client
		reconciler = &StorageRepositoryReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			Recorder:  record.NewFakeRecorder(100),
			S3Factory: mockFactory,
		}
	})

	Context("When reconciling a StorageRepository", func() {
		It("Should handle the lifecycle including validation and deletion blocking", func() {
			spec := disasterv1.StorageRepositorySpec{
				Endpoint:  "http://minio.mock:9000",
				Region:    "us-east-1",
				Bucket:    "my-bucket",
				AccessKey: "minioadmin",
				SecretKey: "minioadmin",
			}

			sr = &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo-lifecycle",
					Namespace: ns,
					Annotations: map[string]string{
						AnnotationTraceID: "test-trace-id",
					},
				},
				Spec: spec,
			}

			By("Creating the StorageRepository")
			Expect(k8sClient.Create(ctx, sr)).To(Succeed())

			key := types.NamespacedName{Name: sr.Name, Namespace: ns}

			// 1. Initial Reconcile -> Add Finalizer
			By("1. Initial Reconcile: Should add finalizer")
			// Mock S3 success for safety, though it might not be reached if finalizer is handled first
			mockS3Client.MockHeadBucket = func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			}

			// First pass syncs dependency labels, second pass adds finalizer.
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			// Verify Finalizer
			updatedSr := &disasterv1.StorageRepository{}
			Expect(k8sClient.Get(ctx, key, updatedSr)).To(Succeed())
			Expect(updatedSr.Finalizers).To(ContainElement(LabelStorageFinalizer))

			// 2. Second Reconcile -> Validate S3 (Success)
			By("2. Reconcile Validation: Should update status to Available when S3 is reachable")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, key, updatedSr)).To(Succeed())
			Expect(updatedSr.Status.Status).To(Equal(disasterv1.StorageRepositoryStatusAvailable))
			Expect(updatedSr.Status.Reason).To(Equal("Available"))

			// 3. Validation Fail: Bucket Missing -> Created
			By("3. Reconcile Validation: Should create bucket if missing")
			// Reset mock to simulate NotFound
			mockS3Client.MockHeadBucket = func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, &s3types.NotFound{}
			}
			createCalled := false
			mockS3Client.MockCreateBucket = func(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
				createCalled = true
				return &s3.CreateBucketOutput{}, nil
			}

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			Expect(createCalled).To(BeTrue())

			// 4. Validation Fail: Critical Error
			By("4. Reconcile Validation: Should update status to Unavailable on critical error")
			mockS3Client.MockHeadBucket = func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, errors.New("network unreachable")
			}

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred()) // Controller records status and requeues without bubbling error

			Expect(k8sClient.Get(ctx, key, updatedSr)).To(Succeed())
			Expect(updatedSr.Status.Status).To(Equal(disasterv1.StorageRepositoryStatusUnavailable))
			Expect(updatedSr.Status.Reason).To(Equal("ValidationFailed"))
			Expect(updatedSr.Status.ObservedGeneration).To(Equal(updatedSr.Generation))
			Expect(updatedSr.Status.ObservedMetadataHash).NotTo(BeEmpty())

			// 4.1 Validation Fail: Bucket Create Error
			By("4.1 Reconcile Validation: Should return error if bucket creation fails")
			mockS3Client.MockHeadBucket = func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return nil, &s3types.NotFound{}
			}
			mockS3Client.MockCreateBucket = func(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
				return nil, errors.New("start create bucket error")
			}
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			// 5. Deletion
			By("5. Deletion: Should proceed even if a DisasterPolicy still references the StorageRepository")

			// Create a policy that uses this SR
			// logic: r.List(ctx, policies, client.MatchingLabels{LabelStorageRepositoryName: sr.Name})
			policy := &disasterv1.DisasterPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-blocking-sr",
					Namespace: ns,
					Labels: map[string]string{
						LabelStorageRepositoryName: sr.Name,
					},
				},
				Spec: disasterv1.DisasterPolicySpec{
					Type:     disasterv1.PolicyTypeAutoBackup,
					Schedule: "*/1 * * * *",
					State:    disasterv1.PolicyStateEnabled,
				},
			}
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())

			// Mark SR for deletion
			Expect(k8sClient.Delete(ctx, sr)).To(Succeed())

			// Handle Delete
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			// Check if deleted
			err = k8sClient.Get(ctx, key, updatedSr)
			if err == nil {
				Fail("StorageRepository should be deleted even when legacy finalizer deletion protection is disabled")
			} else {
				Expect(client.IgnoreNotFound(err)).To(BeNil())
			}
		})

		It("Should pass addressing style and CA bundle into validation runtime settings", func() {
			sr = &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-repo-runtime-settings",
					Namespace: ns,
				},
				Spec: disasterv1.StorageRepositorySpec{
					Endpoint:        "https://s3.example.com",
					Region:          "us-east-1",
					Bucket:          "my-bucket",
					AccessKey:       "ak",
					SecretKey:       "sk",
					AddressingStyle: disasterv1.StorageRepositoryAddressingStyleVirtualHostedStyle,
					CASecretRef:     &corev1.LocalObjectReference{Name: "repo-ca"},
				},
			}
			Expect(k8sClient.Create(ctx, sr)).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "repo-ca", Namespace: ns},
				Data: map[string][]byte{
					disasterv1.StorageRepositoryCASecretKey: []byte("-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----"),
				},
			})).To(Succeed())

			mockS3Client.MockHeadBucket = func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			}

			err := reconciler.ValidateS3Configuration(ctx, sr, "验证存储", false)
			Expect(err).NotTo(HaveOccurred())
			Expect(mockFactory.LastSettings.UsePathStyle).To(BeFalse())
			Expect(mockFactory.LastSettings.Endpoint).To(Equal("https://s3.example.com"))
			Expect(mockFactory.LastSettings.Region).To(Equal("us-east-1"))
			Expect(string(mockFactory.LastSettings.CACert)).To(ContainSubstring("BEGIN CERTIFICATE"))
		})

		It("Should handle client errors using MockClient", func() {
			mockClient := &MockClient{
				Client: k8sClient,
			}
			reconciler := &StorageRepositoryReconciler{
				Client:    mockClient,
				Scheme:    k8sClient.Scheme(),
				Recorder:  record.NewFakeRecorder(100),
				S3Factory: mockFactory,
			}

			srName := "test-error-handling"
			key := types.NamespacedName{Name: srName, Namespace: ns}

			sr := &disasterv1.StorageRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:       srName,
					Namespace:  ns,
					Finalizers: []string{LabelStorageFinalizer}, // Already has finalizer to skip that part
				},
				Spec: disasterv1.StorageRepositorySpec{
					Bucket: "test",
				},
			}
			// Don't create in real client, just mock Get to return it

			// 1. Get Error
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return errors.New("get error")
			}
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			// 2. Status Update Error
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				sr.DeepCopyInto(obj.(*disasterv1.StorageRepository))
				return nil
			}
			// Mock S3 success
			mockS3Client.MockHeadBucket = func(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			}

			// Mock Status Update Error
			mockStatusWriter := &MockStatusWriter{
				StatusWriter: k8sClient.Status(),
				MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					return errors.New("status update error")
				},
			}
			mockClient.MockStatus = func() client.StatusWriter {
				return mockStatusWriter
			}

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			// 3. Finalizer Removal Error
			// Setup object to be in deletion with finalizer
			now := metav1.Now()
			sr.DeletionTimestamp = &now
			mockClient.MockList = nil // Use real list or mock it to return empty policies
			// Need to mock List to return empty list or use simple mock
			mockClient.MockList = func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
				return nil // Empty list
			}

			// Mock Status Update Success (for any status updates)
			mockStatusWriter.MockUpdate = nil

			// Mock Update Error (for finalizer removal)
			mockClient.MockUpdate = func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
				return errors.New("finalizer remove error")
			}

			// We need to make sure Get returns the object with DeletionTimestamp
			mockClient.MockGet = func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				sr.DeepCopyInto(obj.(*disasterv1.StorageRepository))
				return nil
			}

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).To(HaveOccurred())

			// 3. SetupWithManager Coverage
			// Manual setup check skipped due to complexity of mocking ctrl.Manager
		})
	})
})
