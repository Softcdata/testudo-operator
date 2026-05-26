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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	. "github.com/softcdata/testudo-operator/pkg/metadata"
)

var _ = Describe("DisasterPolicy Controller", func() {
	var (
		ctx        context.Context
		reconciler *DisasterPolicyReconciler
		ns         = "default"
		recorder   *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()
		recorder = record.NewFakeRecorder(10)
		reconciler = &DisasterPolicyReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: recorder,
		}
	})

	Context("When reconciling a DisasterPolicy", func() {
		It("Should reject non-positive TTL for AutoBackup policies", func() {
			ttl := metav1.Duration{Duration: 0}
			policy := &disasterv1.DisasterPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-invalid-ttl",
					Namespace: ns,
					Annotations: map[string]string{
						AnnotationTraceID: "test-trace",
					},
				},
				Spec: disasterv1.DisasterPolicySpec{
					Type:     disasterv1.PolicyTypeAutoBackup,
					Schedule: "*/5 * * * *",
					TTL:      &ttl,
					State:    disasterv1.PolicyStateEnabled,
				},
			}

			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			key := types.NamespacedName{Name: policy.Name, Namespace: ns}

			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			updatedPolicy := &disasterv1.DisasterPolicy{}
			Expect(k8sClient.Get(ctx, key, updatedPolicy)).To(Succeed())
			Expect(updatedPolicy.Status.Reason).To(Equal("InvalidTTL"))
			Expect(updatedPolicy.Status.Message).To(ContainSubstring("ttl"))
			Eventually(recorder.Events).Should(Receive(ContainSubstring("InvalidTTL")))
		})

		It("Should ignore TTL on non-AutoBackup policy types", func() {
			ttl := metav1.Duration{Duration: time.Hour}
			policy := &disasterv1.DisasterPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-sync-ttl-ignored",
					Namespace: ns,
					Annotations: map[string]string{
						AnnotationTraceID: "test-trace",
					},
				},
				Spec: disasterv1.DisasterPolicySpec{
					Type:     disasterv1.PolicyTypeDataSync,
					Schedule: "*/5 * * * *",
					TTL:      &ttl,
					State:    disasterv1.PolicyStateEnabled,
				},
			}

			Expect(k8sClient.Create(ctx, policy)).To(Succeed())
			key := types.NamespacedName{Name: policy.Name, Namespace: ns}

			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			updatedPolicy := &disasterv1.DisasterPolicy{}
			Expect(k8sClient.Get(ctx, key, updatedPolicy)).To(Succeed())
			Expect(updatedPolicy.Status.Reason).To(BeEmpty())
			Expect(updatedPolicy.Status.Message).To(BeEmpty())
			Expect(updatedPolicy.Labels[LabelDisasterPolicyType]).To(Equal(string(disasterv1.PolicyTypeDataSync)))
		})

		It("Should handle lifecycle, validation, and label syncing", func() {
			policy := &disasterv1.DisasterPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-lifecycle",
					Namespace: ns,
					Annotations: map[string]string{
						AnnotationTraceID: "test-trace",
					},
				},
				Spec: disasterv1.DisasterPolicySpec{
					Type:     disasterv1.PolicyTypeAutoBackup,
					Schedule: "invalid-cron", // Invalid
					State:    disasterv1.PolicyStateEnabled,
				},
			}

			By("Creating Policy with Invalid Schedule")
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())

			key := types.NamespacedName{Name: policy.Name, Namespace: ns}

			// 1. Reconcile - Invalid Cron
			// The controller records event and returns nil
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			// Verify Finalizer Added (happens before validation)
			updatedPolicy := &disasterv1.DisasterPolicy{}
			Expect(k8sClient.Get(ctx, key, updatedPolicy)).To(Succeed())
			Expect(updatedPolicy.Finalizers).To(ContainElement(LabelPolicyFinalizer))
			Expect(updatedPolicy.Status.Reason).To(Equal("InvalidSchedule"))
			Expect(updatedPolicy.Status.Message).To(ContainSubstring("invalid"))

			// Check for Event
			Eventually(recorder.Events).Should(Receive(ContainSubstring("InvalidSchedule")))

			// 2. Update to Valid Schedule
			By("Updating to Valid Schedule")
			updatedPolicy.Spec.Schedule = "*/5 * * * *"
			Expect(k8sClient.Update(ctx, updatedPolicy)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			// 3. Label Syncing
			By("Verifying Labels are synced")
			Expect(k8sClient.Get(ctx, key, updatedPolicy)).To(Succeed())
			Expect(updatedPolicy.Labels[LabelDisasterPolicyType]).To(Equal(string(disasterv1.PolicyTypeAutoBackup)))
			Expect(updatedPolicy.Labels[LabelDisasterPolicyState]).To(Equal(string(disasterv1.PolicyStateEnabled)))
			Expect(updatedPolicy.Status.Reason).To(BeEmpty())
			Expect(updatedPolicy.Status.Message).To(BeEmpty())

			// 4. Deletion
			By("Deletion: Should proceed when no upstream references exist")
			// Mark Policy for deletion
			Expect(k8sClient.Delete(ctx, updatedPolicy)).To(Succeed())

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deleted
			err = k8sClient.Get(ctx, key, updatedPolicy)
			if err == nil {
				Fail("DisasterPolicy should be deleted")
			} else {
				Expect(client.IgnoreNotFound(err)).To(BeNil())
			}
		})
	})

	Context("When deleting a DisasterPolicy referenced by AppBackup", func() {
		It("Should block deletion when AppBackup references the policy", func() {
			Skip("legacy finalizer deletion protection temporarily disabled")

			policy := &disasterv1.DisasterPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy-appbackup-protection",
					Namespace: ns,
					Annotations: map[string]string{
						AnnotationTraceID: "test-trace",
					},
				},
				Spec: disasterv1.DisasterPolicySpec{
					Type:     disasterv1.PolicyTypeAutoBackup,
					Schedule: "*/5 * * * *",
					State:    disasterv1.PolicyStateEnabled,
				},
			}

			By("Creating the DisasterPolicy")
			Expect(k8sClient.Create(ctx, policy)).To(Succeed())

			key := types.NamespacedName{Name: policy.Name, Namespace: ns}

			// Reconcile to add finalizer
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			By("Creating an AppBackup that references the policy via label")
			appBackup := &disasterv1.AppBackup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-appbackup-with-policy",
					Namespace: ns,
					Labels: map[string]string{
						LabelDisasterPolicyName: policy.Name,
					},
				},
				Spec: disasterv1.AppBackupSpec{
					Cluster:        "test-cluster",
					Schedule:       "*/10 * * * *",
					DisasterPolicy: policy.Name,
				},
			}
			Expect(k8sClient.Create(ctx, appBackup)).To(Succeed())

			By("Attempting to delete the policy")
			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying deletion is blocked")
			updatedPolicy := &disasterv1.DisasterPolicy{}
			Expect(k8sClient.Get(ctx, key, updatedPolicy)).To(Succeed())
			Expect(updatedPolicy.Status.Reason).To(Equal("DeletionBlocked"))
			Expect(updatedPolicy.Status.Message).To(ContainSubstring(appBackup.Name))

			By("Removing the AppBackup and retrying deletion")
			Expect(k8sClient.Delete(ctx, appBackup)).To(Succeed())

			// Reconcile
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying policy is deleted")
			err = k8sClient.Get(ctx, key, updatedPolicy)
			Expect(client.IgnoreNotFound(err)).To(BeNil())
		})
	})
})
