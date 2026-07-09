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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	runtimecfg "github.com/softcdata/testudo-operator/internal/controller/runtimeconfig"
	disasterv1 "github.com/softcdata/testudo-operator/pkg/apis/disaster/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("DisasterOperation Controller Timeout", func() {
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

	Describe("Timeout Inheritance", func() {
		It("Should use runtime defaults for operation timeout, retry, and requeue helpers", func() {
			runtimecfg.ResetForTest()
			DeferCleanup(runtimecfg.ResetForTest)

			snapshot := runtimecfg.DefaultSnapshot()
			snapshot.OperationRuntime.DefaultTimeoutMinutes = 7
			snapshot.OperationRuntime.StepStartRequeue = 2 * time.Second
			snapshot.OperationRuntime.StepRunningRequeue = 4 * time.Second
			snapshot.OperationRuntime.DefaultRetryInterval = 9 * time.Second
			runtimecfg.Activate(snapshot)

			Expect(operationDefaultTimeoutMinutes()).To(Equal(int32(7)))
			Expect(operationStepStartRequeue()).To(Equal(2 * time.Second))
			Expect(operationStepRunningRequeue()).To(Equal(4 * time.Second))

			reconciler := &DisasterOperationReconciler{}
			Expect(reconciler.retryWaitDuration(nil)).To(Equal(9 * time.Second))
			Expect(reconciler.retryWaitDuration(&disasterv1.DisasterOperation{
				Spec: disasterv1.DisasterOperationSpec{
					RetryPolicy: &disasterv1.RetryPolicy{RetryIntervalSeconds: 11},
				},
			})).To(Equal(11 * time.Second))
		})

		It("Should inherit timeout from DisasterInstance", func() {
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-instance-timeout",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterInstanceSpec{
					OperationTimeoutMinutes: 30,
				},
			}

			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-op-inherit",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:  "test-instance-timeout",
					OperationType: disasterv1.OperationTypeSyncData,
					// TimeoutMinutes is 0
				},
				Status: disasterv1.DisasterOperationStatus{
					State: disasterv1.OperationStatePending,
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

			// Reconcile
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op-inherit", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			// Check updated Op
			updatedOp := &disasterv1.DisasterOperation{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-op-inherit", Namespace: "default"}, updatedOp)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedOp.Spec.TimeoutMinutes).To(Equal(int32(30)))
		})

	})

	Describe("Step Timeout", func() {
		It("Should fail operation if a step exceeds timeout", func() {
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-instance-step-timeout",
					Namespace: "default",
				},
				Status: disasterv1.DisasterInstanceStatus{
					FsmState: disasterv1.FsmStateProtected,
				},
			}

			// Operation with 1 minute timeout
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-op-step-timeout",
					Namespace: "default",
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:   "test-instance-step-timeout",
					OperationType:  disasterv1.OperationTypeFailover,
					TimeoutMinutes: 1,
				},
				Status: disasterv1.DisasterOperationStatus{
					State: disasterv1.OperationStateRunning,
					Steps: []disasterv1.StepStatus{
						{
							Name:      string(disasterv1.FailoverStepPreCheck),
							State:     "Running",
							StartTime: &metav1.Time{Time: time.Now().Add(-2 * time.Minute)}, // Started 2 mins ago
						},
					},
					CurrentStep: string(disasterv1.FailoverStepPreCheck),
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
				ClientFactory: func(config *rest.Config, options client.Options) (client.Client, error) {
					return fakeClient, nil
				},
			}

			// Reconcile
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op-step-timeout", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			// Check updated Op
			updatedOp := &disasterv1.DisasterOperation{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-op-step-timeout", Namespace: "default"}, updatedOp)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Message).To(ContainSubstring("超时"))
			Expect(updatedOp.Status.Steps[0].State).To(Equal("Failed"))
			Expect(updatedOp.Status.AutoCancelTriggered).To(BeTrue())
			Expect(updatedOp.Status.AutoCancelStatus).To(Equal(disasterv1.OperationAutoCancelStatusSucceeded))
			Expect(updatedOp.Status.AutoCancelMode).To(Equal(disasterv1.OperationAutoCancelModeDirectRollback))
		})
	})

	Describe("Sync Timeout", func() {
		It("Should fail sync operation if total time exceeds timeout", func() {
			instance := &disasterv1.DisasterInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-instance-sync-timeout",
					Namespace: "default",
				},
			}

			// Operation with 1 minute timeout, started 2 mins ago
			op := &disasterv1.DisasterOperation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-op-sync-timeout",
					Namespace: "default",
					Annotations: map[string]string{
						"testudo.softcdata.com/sync-triggered-at": time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
					},
				},
				Spec: disasterv1.DisasterOperationSpec{
					InstanceName:   "test-instance-sync-timeout",
					OperationType:  disasterv1.OperationTypeSyncData,
					TimeoutMinutes: 1,
				},
				Status: disasterv1.DisasterOperationStatus{
					State:     disasterv1.OperationStateRunning,
					StartTime: &metav1.Time{Time: time.Now().Add(-2 * time.Minute)},
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

			// Reconcile
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-op-sync-timeout", Namespace: "default"}})
			Expect(err).NotTo(HaveOccurred())

			// Check updated Op
			updatedOp := &disasterv1.DisasterOperation{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-op-sync-timeout", Namespace: "default"}, updatedOp)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedOp.Status.State).To(Equal(disasterv1.OperationStateFailed))
			Expect(updatedOp.Status.Message).To(ContainSubstring("超时"))
		})
	})
})
